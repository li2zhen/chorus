package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"chores/internal/store"
)

// 本文件是契约 v2/v5 的 HTTP 增量：
//   0. 单条实例的编辑/删除（PATCH/DELETE /api/instances/{id}）
//   1. 分组 / 成员的"不需要管理员"路径（老 /api/admin/* 保留可用）
//   2. 三选二的时间字段解析（解析失败 → 400）
//   3. 头像上传 / 读取 / 清空
//   4. /api/bootstrap 的 time_defaults
//
// 授权模型（契约 v2）：
//   - 任意已登录成员：POST/PATCH 成员、POST/PATCH/DELETE 分组、发布任务、认领/完成
//   - 仍只有管理员：DELETE 成员、导出、写演示数据

// ---- 时间字段：解码 → 解析 → 写进 input ----

func decodeInstanceTime(in *store.InstanceInput, loc *time.Location) error {
	_, start, end, dur, err := store.ParseTimeFields(in.RawStartAt, in.RawEndAt, in.RawDuration, loc)
	if err != nil {
		return err
	}
	in.StartAt, in.EndAt, in.DurationMinutes = start, end, dur
	return nil
}

func decodeChoreTime(in *store.ChoreInput, loc *time.Location) error {
	_, start, end, dur, err := store.ParseTimeFields(in.RawStartAt, in.RawEndAt, in.RawDuration, loc)
	if err != nil {
		return err
	}
	in.StartAt, in.EndAt, in.DurationMinutes = start, end, dur
	return nil
}

// ---- 分组：任意已登录成员 ----

func (a *API) postGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireMember(w, r); !ok {
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

func (a *API) patchGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireMember(w, r); !ok {
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

func (a *API) deleteGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireMember(w, r); !ok {
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

// ---- 成员：新增/修改任意成员可做，删除仍要管理员 ----

func (a *API) postMember(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireMember(w, r); !ok {
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

func (a *API) patchMember(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireMember(w, r); !ok {
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

// ---- 读接口：分组与成员列表 ----
//
// 契约 v2 只写了 POST/PATCH/DELETE，但 /admin 页要列出分组与成员，
// 所以补两个 GET（登录成员即可读）。老前缀 /api/admin/groups|members 保留同名 GET 兼容。

func (a *API) groupsPayload() map[string]any {
	return map[string]any{"groups": a.groupViews(a.deps.Store.ListGroups())}
}

func (a *API) membersPayload() map[string]any {
	// memberView 只输出契约字段（id/name/color/avatar/avatar_url/is_admin/sort），
	// 头像字节与任何内部字段都不外露。
	return map[string]any{"members": a.memberViews(a.deps.Store.ListMembers())}
}

func (a *API) getGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireMember(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, a.groupsPayload())
}

func (a *API) getMembers(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireMember(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, a.membersPayload())
}

// 老前缀：只要管理员 Cookie 即可（不要求同时有成员 Cookie）。
func (a *API) getAdminGroupsCompat(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, a.groupsPayload())
}

func (a *API) getAdminMembersCompat(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, a.membersPayload())
}

// ---- 头像 ----

// putMemberAvatar 与 putAdminMemberAvatar 是同一实现的两个入口：
// 成员入口要求"任意已登录成员"，管理员入口要求管理员 Cookie。
// 前端管理页打的是 /api/admin/... 那条（曾经 404，见 v4 修复）。
func (a *API) putMemberAvatar(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireMember(w, r); !ok {
		return
	}
	a.putAvatar(w, r)
}

func (a *API) putAdminMemberAvatar(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.putAvatar(w, r)
}

// putAvatar 是两条路由共用的实现（不做鉴权，由入口负责）。
func (a *API) putAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	var body struct {
		ContentType string `json:"content_type"`
		DataBase64  string `json:"data_base64"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	if !store.AllowedAvatarType(body.ContentType) {
		writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "头像只支持 image/png 或 image/jpeg")
		return
	}
	// base64 膨胀约 4/3：先按上限粗筛，省掉一次无谓的解码。
	if len(body.DataBase64) > store.MaxAvatarBytes*4/3+16 {
		writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "头像不得超过 256 KB")
		return
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body.DataBase64))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "data_base64 不是合法 base64")
		return
	}
	m, err := a.deps.Store.SetAvatar(id, body.ContentType, data)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"member": a.memberView(m)})
}

func (a *API) getAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	data, contentType, ok := a.deps.Store.AvatarData(id)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "该成员没有头像")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (a *API) deleteMemberAvatar(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireMember(w, r); !ok {
		return
	}
	a.deleteAvatar(w, r)
}

func (a *API) deleteAdminMemberAvatar(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.deleteAvatar(w, r)
}

// deleteAvatar 是两条路由共用的实现（不做鉴权，由入口负责）。
func (a *API) deleteAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	m, err := a.deps.Store.ClearAvatar(id)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"member": a.memberView(m)})
}

// ---- 老路径转发：保留 /api/admin/members* 与 /api/admin/groups* 可用 ----

func (a *API) postAdminMemberCompat(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.postMember(w, r)
}

func (a *API) patchAdminMemberCompat(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.patchMember(w, r)
}

func (a *API) postAdminGroupCompat(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.postGroup(w, r)
}

func (a *API) patchAdminGroupCompat(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.patchGroup(w, r)
}

func (a *API) deleteAdminGroupCompat(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.deleteGroup(w, r)
}

// adminStatusFor 供 bootstrap 返回 is_admin（既要 Cookie 也要是管理员成员）。
func (a *API) adminStatusFor(r *http.Request, memberID int64) bool {
	if !a.isAdmin(r) {
		return false
	}
	if memberID == 0 {
		return false
	}
	m, ok := a.deps.Store.MemberByID(memberID)
	return ok && m.IsAdmin
}

// memberView 统一构造（含 v2 的 avatar_url）。
func (a *API) memberView(m store.Member) memberView {
	v := memberView{
		ID:      m.ID,
		Name:    m.Name,
		Color:   m.Color,
		Avatar:  m.Avatar,
		IsAdmin: m.IsAdmin,
		Sort:    m.Sort,
	}
	if url := a.deps.Store.AvatarURL(m.ID); url != "" {
		v.AvatarURL = &url
	}
	return v
}

var _ = errors.Is
var _ = strconv.Itoa

// ---- v5：单条实例的编辑与删除（前端"更多 → 编辑/删除"打这两条） ----

// patchInstanceRequest 是 PATCH /api/instances/{id} 的请求体。
// 时间三个字段用 json.RawMessage 以便区分"没给"与"显式 null"。
type patchInstanceRequest struct {
	Title    *string         `json:"title"`
	Note     *string         `json:"note"`
	GroupID  *int64          `json:"group_id"`
	StartAt  json.RawMessage `json:"start_at"`
	EndAt    json.RawMessage `json:"end_at"`
	Duration json.RawMessage `json:"duration_minutes"`
}

// patchInstance 只改这一条实例，不动它所属的定义。
func (a *API) patchInstance(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireMember(w, r); !ok {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	var body patchInstanceRequest
	if err := decodeJSON(r, &body); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	_, start, end, dur, err := store.ParseTimeFields(body.StartAt, body.EndAt, body.Duration, a.loc)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	inst, err := a.deps.Store.UpdateInstance(id, store.UpdateInstanceInput{
		Title:           body.Title,
		Note:            body.Note,
		GroupID:         body.GroupID,
		StartAt:         start,
		EndAt:           end,
		DurationMinutes: dur,
	})
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": a.instanceView(inst)})
}

// deleteInstance 只删这一条（幂等：删两次都 200）。
func (a *API) deleteInstance(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireMember(w, r); !ok {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	if err := a.deps.Store.DeleteInstance(id); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": map[string]any{"deleted": true}})
}
