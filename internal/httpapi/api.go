// Package httpapi 把 api/CONTRACT.md 的 /api 端点接到 store 上。
//
// 约定：
//   - 响应一律 application/json; charset=utf-8，snake_case 字段
//   - 错误一律 {"error":{"code","message"}}，code 用大写下划线
//   - 认证：Cookie chores_member=<id>（成员）/ chores_admin=1（管理员）
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"chores/internal/store"
)

// Deps 是这一层需要的外部事实。
type Deps struct {
	Store      *store.DB
	AdminToken string
	Version    string
}

// API 持有依赖并暴露 http.Handler。
type API struct {
	deps Deps
	loc  *time.Location
}

// New 构造 API；loc 决定"今天"的边界（默认 UTC，main 传 TZ）。
func New(deps Deps, loc *time.Location) *API {
	if loc == nil {
		loc = time.UTC
	}
	return &API{deps: deps, loc: loc}
}

// ---- 通用工具 ----

const (
	cookieMember = "chores_member"
	cookieAdmin  = "chores_admin"
)

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorBody struct {
	Error apiError `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("chores: encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: apiError{Code: code, Message: message}})
}

// writeDomainError 把 store 的领域错误翻成契约里的 code。
func writeDomainError(w http.ResponseWriter, err error, body any) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, store.ErrForbidden):
		writeError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
	case errors.Is(err, store.ErrConflict):
		// 冲突时把当前实例一起回给前端（契约第 12 行）。
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":    apiError{Code: "CONFLICT", Message: err.Error()},
			"instance": body,
		})
	case errors.Is(err, store.ErrUnsupportedMedia):
		writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", err.Error())
	case errors.Is(err, store.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", err.Error())
	case errors.Is(err, store.ErrBadInput):
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", badInputMessage(err))
	default:
		log.Printf("chores: internal error: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "服务器开小差了")
	}
}

// badInputMessage 去掉 store 用 %w 包 ErrBadInput 时带上的 "bad input: " 前缀。
//
// 为什么要剥：契约 v2 规定"只给一个时间字段"时 message 必须是
// 时间需要给两个：开始/时长/结束 —— 原文，不能带内部前缀。
// store 里的 *fieldError（badTime/badDuration/ResolveTime）自带干净文案，
// 它们也 Unwrap 成 ErrBadInput，走的正是这条分支，因此文案原样透出。
func badInputMessage(err error) string {
	return strings.TrimPrefix(err.Error(), store.ErrBadInput.Error()+": ")
}

func decodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return fmt.Errorf("%w: 请求体为空", store.ErrBadInput)
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: JSON 解析失败", store.ErrBadInput)
	}
	return nil
}

func pathID(r *http.Request) (int64, error) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: 非法 id %q", store.ErrBadInput, raw)
	}
	return id, nil
}

func (a *API) memberID(r *http.Request) int64 {
	c, err := r.Cookie(cookieMember)
	if err != nil {
		return 0
	}
	id, err := strconv.ParseInt(strings.TrimSpace(c.Value), 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

func (a *API) isAdmin(r *http.Request) bool {
	c, err := r.Cookie(cookieAdmin)
	return err == nil && c.Value == "1"
}

// requireMember 在需要"谁在做"的动作里强制登录，返回成员 id。
func (a *API) requireMember(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id := a.memberID(r)
	if id == 0 {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "请先选择成员")
		return 0, false
	}
	if _, ok := a.deps.Store.MemberByID(id); !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "成员不存在")
		return 0, false
	}
	return id, true
}

func (a *API) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !a.isAdmin(r) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "需要管理员")
		return false
	}
	return true
}

func setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// adminToken 取环境变量，默认 admin（README 的默认值）。
func (a *API) adminToken() string {
	if a.deps.AdminToken != "" {
		return a.deps.AdminToken
	}
	if v := strings.TrimSpace(os.Getenv("CHORES_ADMIN_TOKEN")); v != "" {
		return v
	}
	return "admin"
}

// ---- 序列化：契约里的 snake_case 视图 ----

type memberView struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Color   string `json:"color"`
	Avatar  string `json:"avatar"`
	IsAdmin bool   `json:"is_admin"`
	Sort    int    `json:"sort"`
	// v2：有头像时是 /api/avatars/{id}?v=<updatedAt>，否则 null。
	AvatarURL *string `json:"avatar_url"`
}

type groupView struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Sort      int     `json:"sort"`
	MemberIDs []int64 `json:"member_ids"`
}

type instanceView struct {
	ID             int64   `json:"id"`
	ChoreID        *int64  `json:"chore_id"`
	Title          string  `json:"title"`
	Note           string  `json:"note"`
	GroupID        *int64  `json:"group_id"`
	GroupName      *string `json:"group_name"`
	DueDate        string  `json:"due_date"`
	State          string  `json:"state"`
	RequiresClaim  bool    `json:"requires_claim"`
	RequiresPhoto  bool    `json:"requires_photo"`
	ClaimedBy      *int64  `json:"claimed_by"`
	ClaimedAt      *string `json:"claimed_at"`
	CompletedBy    *int64  `json:"completed_by"`
	CompletedAt    *string `json:"completed_at"`
	CompletionNote *string `json:"completion_note"`
	CompletedName  *string `json:"completed_by_name"`
	ClaimedName    *string `json:"claimed_by_name"`
	// v2：任务时间；老数据为空 → null。
	StartAt         *string `json:"start_at"`
	EndAt           *string `json:"end_at"`
	DurationMinutes *int    `json:"duration_minutes"`
}

type choreView struct {
	ID            int64   `json:"id"`
	Title         string  `json:"title"`
	Note          string  `json:"note"`
	GroupID       *int64  `json:"group_id"`
	GroupName     *string `json:"group_name"`
	Recurrence    string  `json:"recurrence"`
	Weekday       *int    `json:"weekday"`
	DayOfMonth    *int    `json:"day_of_month"`
	DueTime       string  `json:"due_time"`
	MemberID      *int64  `json:"member_id"`
	RequiresClaim bool    `json:"requires_claim"`
	RequiresPhoto bool    `json:"requires_photo"`
	StartDate     string  `json:"start_date"`
	// v2：时间模板。
	StartAt         *string `json:"start_at"`
	EndAt           *string `json:"end_at"`
	DurationMinutes *int    `json:"duration_minutes"`
}

type activityView struct {
	ID         int64   `json:"id"`
	At         string  `json:"at"`
	ActorID    *int64  `json:"actor_id"`
	ActorName  *string `json:"actor_name"`
	InstanceID *int64  `json:"instance_id"`
	Action     string  `json:"action"`
	Detail     string  `json:"detail"`
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

func (a *API) memberName(id *int64) *string {
	if id == nil {
		return nil
	}
	if m, ok := a.deps.Store.MemberByID(*id); ok {
		return &m.Name
	}
	return nil
}

func (a *API) groupName(id *int64) *string {
	if id == nil {
		return nil
	}
	for _, g := range a.deps.Store.ListGroups() {
		if g.ID == *id {
			name := g.Name
			return &name
		}
	}
	return nil
}

func (a *API) instanceView(inst store.Instance) instanceView {
	return instanceView{
		ID:              inst.ID,
		ChoreID:         inst.ChoreID,
		Title:           inst.Title,
		Note:            inst.Note,
		GroupID:         inst.GroupID,
		GroupName:       a.groupName(inst.GroupID),
		DueDate:         inst.DueDate,
		State:           inst.State,
		RequiresClaim:   inst.RequiresClaim,
		RequiresPhoto:   inst.RequiresPhoto,
		ClaimedBy:       inst.ClaimedBy,
		ClaimedAt:       strPtr(inst.ClaimedAt),
		CompletedBy:     inst.CompletedBy,
		CompletedAt:     strPtr(inst.CompletedAt),
		CompletionNote:  strPtr(inst.CompletionNote),
		CompletedName:   a.memberName(inst.CompletedBy),
		ClaimedName:     a.memberName(inst.ClaimedBy),
		StartAt:         strPtr(inst.StartAt),
		EndAt:           strPtr(inst.EndAt),
		DurationMinutes: inst.DurationMinutes,
	}
}

func (a *API) instanceViews(list []store.Instance) []instanceView {
	out := make([]instanceView, 0, len(list))
	for _, inst := range list {
		out = append(out, a.instanceView(inst))
	}
	return out
}

func (a *API) memberViews(list []store.Member) []memberView {
	out := make([]memberView, 0, len(list))
	for _, m := range list {
		out = append(out, a.memberView(m))
	}
	return out
}

// groupView 单条（list 版是 groupViews）。
func (a *API) groupView(g store.Group) groupView {
	ids := g.MemberIDs
	if ids == nil {
		ids = []int64{}
	}
	return groupView{ID: g.ID, Name: g.Name, Sort: g.Sort, MemberIDs: ids}
}

func (a *API) groupViews(list []store.Group) []groupView {
	out := make([]groupView, 0, len(list))
	for _, g := range list {
		ids := g.MemberIDs
		if ids == nil {
			ids = []int64{}
		}
		out = append(out, groupView{ID: g.ID, Name: g.Name, Sort: g.Sort, MemberIDs: ids})
	}
	return out
}

func (a *API) choreView(c store.Chore) choreView {
	return choreView{
		ID:              c.ID,
		Title:           c.Title,
		Note:            c.Note,
		GroupID:         c.GroupID,
		GroupName:       a.groupName(c.GroupID),
		Recurrence:      c.Recurrence,
		Weekday:         c.Weekday,
		DayOfMonth:      c.DayOfMonth,
		DueTime:         c.DueTime,
		MemberID:        c.MemberID,
		RequiresClaim:   c.RequiresClaim,
		RequiresPhoto:   c.RequiresPhoto,
		StartDate:       c.StartDate,
		StartAt:         strPtr(c.StartAt),
		EndAt:           strPtr(c.EndAt),
		DurationMinutes: c.DurationMinutes,
	}
}

// ---- 身份端点 ----

func (a *API) postLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MemberID int64 `json:"member_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	m, ok := a.deps.Store.MemberByID(body.MemberID)
	if !ok || m.Archived {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "成员不存在")
		return
	}
	setCookie(w, cookieMember, strconv.FormatInt(m.ID, 10), 365*24*3600)
	writeJSON(w, http.StatusOK, map[string]any{"me": memberView{ID: m.ID, Name: m.Name, Color: m.Color, Avatar: m.Avatar, IsAdmin: m.IsAdmin, Sort: m.Sort}})
}

func (a *API) postLogout(w http.ResponseWriter, r *http.Request) {
	setCookie(w, cookieMember, "", -1)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) getSession(w http.ResponseWriter, r *http.Request) {
	id := a.memberID(r)
	if id == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"me": nil, "is_admin": a.isAdmin(r)})
		return
	}
	m, ok := a.deps.Store.MemberByID(id)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"me": nil, "is_admin": a.isAdmin(r)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"me":       memberView{ID: m.ID, Name: m.Name, Color: m.Color, Avatar: m.Avatar, IsAdmin: m.IsAdmin, Sort: m.Sort},
		"is_admin": a.isAdmin(r),
	})
}

func (a *API) postAdminLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	if strings.TrimSpace(body.Token) != a.adminToken() {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "口令不对")
		return
	}
	setCookie(w, cookieAdmin, "1", 30*24*3600)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) postAdminLogout(w http.ResponseWriter, r *http.Request) {
	setCookie(w, cookieAdmin, "", -1)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) getAdminSession(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- 视图端点 ----

func (a *API) getBootstrap(w http.ResponseWriter, r *http.Request) {
	from, to := a.defaultRange()
	if v := strings.TrimSpace(r.URL.Query().Get("from")); v != "" {
		from = v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("to")); v != "" {
		to = v
	}
	views := a.instanceViews(a.deps.Store.ListInstances(store.Query{From: from, To: to}))

	memberID := a.memberID(r)
	var me any
	if memberID != 0 {
		if m, ok := a.deps.Store.MemberByID(memberID); ok {
			me = a.memberView(m)
		}
	}
	// v2：给前端表单预填的默认时间（服务器本地时区的当前时刻）。
	nowLocal := time.Now().In(a.loc)
	writeJSON(w, http.StatusOK, map[string]any{
		"now":      time.Now().UTC().Format(time.RFC3339),
		"tz":       a.loc.String(),
		"today":    nowLocal.Format("2006-01-02"),
		"me":       me,
		"is_admin": a.adminStatusFor(r, memberID),
		"time_defaults": map[string]any{
			"duration_minutes": store.DefaultDurationMinutes,
			"start_at_local":   nowLocal.Format("2006-01-02T15:04"),
		},
		"members":   a.memberViews(a.deps.Store.ListMembers()),
		"groups":    a.groupViews(a.deps.Store.ListGroups()),
		"instances": views,
		"range":     map[string]string{"from": from, "to": to},
	})
}

func (a *API) getInstances(w http.ResponseWriter, r *http.Request) {
	q := store.Query{
		From:  strings.TrimSpace(r.URL.Query().Get("from")),
		To:    strings.TrimSpace(r.URL.Query().Get("to")),
		State: strings.TrimSpace(r.URL.Query().Get("state")),
	}
	if v := strings.TrimSpace(r.URL.Query().Get("member")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "member 需为数字")
			return
		}
		q.MemberID = id
	}
	if v := strings.TrimSpace(r.URL.Query().Get("group")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "group 需为数字")
			return
		}
		q.GroupID = id
	}
	if q.From == "" && q.To == "" {
		q.From, q.To = a.defaultRange()
	}
	// v3：窗口外的增量补齐必须能安全调用——日期格式非法、或 from > to，一律 400。
	// 不静默返回空数组：否则前端会把"参数写错"误当成"这段时间真的没任务"。
	if q.From != "" {
		if _, err := time.ParseInLocation("2006-01-02", q.From, time.UTC); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "from 需为 YYYY-MM-DD")
			return
		}
	}
	if q.To != "" {
		if _, err := time.ParseInLocation("2006-01-02", q.To, time.UTC); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "to 需为 YYYY-MM-DD")
			return
		}
	}
	if q.From != "" && q.To != "" && q.From > q.To {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "from 不能晚于 to")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"instances": a.instanceViews(a.deps.Store.ListInstances(q)),
		"today":     time.Now().In(a.loc).Format("2006-01-02"),
	})
}

func (a *API) getChores(w http.ResponseWriter, r *http.Request) {
	list := a.deps.Store.ListChores()
	views := make([]choreView, 0, len(list))
	for _, c := range list {
		views = append(views, a.choreView(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"chores": views})
}

func (a *API) getActivity(w http.ResponseWriter, r *http.Request) {
	var instanceID int64
	if v := strings.TrimSpace(r.URL.Query().Get("instance")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "instance 需为数字")
			return
		}
		instanceID = id
	}
	limit := 100
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	list := a.deps.Store.ListActivity(instanceID, limit)
	views := make([]activityView, 0, len(list))
	for _, it := range list {
		views = append(views, activityView{
			ID:         it.ID,
			At:         it.At,
			ActorID:    it.ActorID,
			ActorName:  a.memberName(it.ActorID),
			InstanceID: it.InstanceID,
			Action:     it.Action,
			Detail:     it.Detail,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"activity": views})
}

// defaultRange 是契约里的默认窗口：今天往前 7 天 … 往后 30 天，
// 并与"当前自然月"取并集（前端的周/月日历靠首屏这一份数据直接渲染）。
// 即 min(今天-7, 本月 1 日) ~ max(今天+30, 本月最后一天)。
func (a *API) defaultRange() (string, string) {
	today := time.Now().In(a.loc)
	from := today.AddDate(0, 0, -7)
	to := today.AddDate(0, 0, 30)
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, a.loc)
	monthEnd := time.Date(today.Year(), today.Month()+1, 0, 0, 0, 0, 0, a.loc)
	if monthStart.Before(from) {
		from = monthStart
	}
	if monthEnd.After(to) {
		to = monthEnd
	}
	return from.Format("2006-01-02"), to.Format("2006-01-02")
}

// ---- 动作端点 ----

func (a *API) postInstance(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireMember(w, r)
	if !ok {
		return
	}
	var in store.InstanceInput
	if err := decodeJSON(r, &in); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	if err := decodeInstanceTime(&in, a.loc); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	in.ActorID = &actor
	inst, err := a.deps.Store.CreateInstance(in)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"instance": a.instanceView(inst)})
}

func (a *API) postChore(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireMember(w, r); !ok {
		return
	}
	var in store.ChoreInput
	if err := decodeJSON(r, &in); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	if err := decodeChoreTime(&in, a.loc); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	c, err := a.deps.Store.CreateChore(in)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"chore": a.choreView(c)})
}

func (a *API) patchChore(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireMember(w, r); !ok {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	var in store.ChoreInput
	if err := decodeJSON(r, &in); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	if err := decodeChoreTime(&in, a.loc); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	c, err := a.deps.Store.UpdateChore(id, in)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"chore": a.choreView(c)})
}

func (a *API) deleteChore(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireMember(w, r); !ok {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	if err := a.deps.Store.ArchiveChore(id); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) postClaim(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireMember(w, r)
	if !ok {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	inst, err := a.deps.Store.Claim(id, actor)
	if err != nil {
		writeDomainError(w, err, a.instanceView(inst))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": a.instanceView(inst)})
}

func (a *API) postRelease(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireMember(w, r)
	if !ok {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	inst, err := a.deps.Store.Release(id, actor, a.isAdmin(r))
	if err != nil {
		writeDomainError(w, err, a.instanceView(inst))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": a.instanceView(inst)})
}

func (a *API) postComplete(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireMember(w, r)
	if !ok {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	var body struct {
		Note    string `json:"note"`
		PhotoID string `json:"photo_id"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &body); err != nil {
			writeDomainError(w, err, nil)
			return
		}
	}
	inst, err := a.deps.Store.Complete(id, actor, body.Note)
	if err != nil {
		writeDomainError(w, err, a.instanceView(inst))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": a.instanceView(inst)})
}

func (a *API) postUncomplete(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireMember(w, r)
	if !ok {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	inst, err := a.deps.Store.Uncomplete(id, actor, a.isAdmin(r))
	if err != nil {
		writeDomainError(w, err, a.instanceView(inst))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": a.instanceView(inst)})
}

func (a *API) postReschedule(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireMember(w, r)
	if !ok {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	var body struct {
		DueDate string `json:"due_date"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	inst, err := a.deps.Store.Reschedule(id, actor, a.isAdmin(r), body.DueDate)
	if err != nil {
		writeDomainError(w, err, a.instanceView(inst))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": a.instanceView(inst)})
}

// ---- 管理面端点 ----

func (a *API) postAdminMember(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body struct {
		Name   string `json:"name"`
		Color  string `json:"color"`
		Avatar string `json:"avatar"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	m, err := a.deps.Store.CreateMember(body.Name, body.Color, body.Avatar)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"member": a.memberView(m)})
}

func (a *API) patchAdminMember(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	var body struct {
		Name    *string `json:"name"`
		Color   *string `json:"color"`
		Avatar  *string `json:"avatar"`
		Sort    *int    `json:"sort"`
		IsAdmin *bool   `json:"is_admin"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	m, err := a.deps.Store.UpdateMember(id, body.Name, body.Color, body.Avatar, body.Sort, body.IsAdmin)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"member": a.memberView(m)})
}

func (a *API) deleteAdminMember(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	if err := a.deps.Store.ArchiveMember(id); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) postAdminGroup(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body struct {
		Name      string  `json:"name"`
		MemberIDs []int64 `json:"member_ids"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	g, err := a.deps.Store.CreateGroup(body.Name, body.MemberIDs)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"group": a.groupView(g)})
}

func (a *API) patchAdminGroup(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	var body struct {
		Name      *string  `json:"name"`
		MemberIDs *[]int64 `json:"member_ids"`
		Sort      *int     `json:"sort"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	g, err := a.deps.Store.UpdateGroup(id, body.Name, body.MemberIDs, body.Sort)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": a.groupView(g)})
}

func (a *API) deleteAdminGroup(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	if err := a.deps.Store.DeleteGroup(id); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
