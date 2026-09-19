// Package store 是家务认领板的数据层。
//
// 实现选择（契约允许）：单文件 JSON + sync.RWMutex + 原子写（临时文件 + rename），
// 而不是 SQLite——硬性约束是零第三方 Go 依赖，标准库没有 SQLite 驱动。
// 契约 api/CONTRACT.md 里的 DDL 在这里一一对应为结构体字段：
// members/groups/chores/chore_instances/activity/settings。
//
// 并发正确性：所有"读-判断-写"（尤其是认领）都在写锁内完成，谁先拿到锁谁赢，
// 输的一方返回 ErrConflict 并带上当前实例，HTTP 层翻译成 409。
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 文件格式版本；结构变更时 +1，Load 时按版本做兼容。
const formatVersion = 2

// 领域错误：HTTP 层把它们翻译成契约里的 code。
var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrForbidden = errors.New("forbidden")
	ErrBadInput  = errors.New("bad input")
	// ErrUnsupportedMedia → 415，ErrTooLarge → 413（头像上传用）。
	ErrUnsupportedMedia = errors.New("unsupported media type")
	ErrTooLarge         = errors.New("payload too large")
)

// Member 家庭成员。
type Member struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Avatar    string `json:"avatar"`
	IsAdmin   bool   `json:"is_admin"`
	Sort      int    `json:"sort"`
	Archived  bool   `json:"archived"`
	CreatedAt string `json:"created_at"`
	// AvatarData 是上传的头像字节（base64 存进数据文件，不落磁盘文件）。
	AvatarData []byte `json:"avatar_data,omitempty"`
	// AvatarContentType 只可能是 image/png 或 image/jpeg。
	AvatarContentType string `json:"avatar_content_type,omitempty"`
	// AvatarUpdatedAt 用于 avatar_url 的破缓存参数。
	AvatarUpdatedAt string `json:"avatar_updated_at,omitempty"`
}

// Group 任务分组；MemberIDs 为空表示全员可做。
type Group struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Sort      int     `json:"sort"`
	MemberIDs []int64 `json:"member_ids"`
	CreatedAt string  `json:"created_at"`
}

// Chore 任务定义（循环规则在这里）。
type Chore struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	Note          string `json:"note"`
	GroupID       *int64 `json:"group_id"`
	Recurrence    string `json:"recurrence"` // none | daily | weekly | monthly
	Weekday       *int   `json:"weekday"`    // 0=周日 … 6=周六
	DayOfMonth    *int   `json:"day_of_month"`
	DueTime       string `json:"due_time"`
	MemberID      *int64 `json:"member_id"`
	RequiresClaim bool   `json:"requires_claim"`
	RequiresPhoto bool   `json:"requires_photo"`
	StartDate     string `json:"start_date"`
	Archived      bool   `json:"archived"`
	CreatedAt     string `json:"created_at"`
	// v2：时间模板（RFC3339 UTC），循环生成时按每个实例的 due_date 平移本地时刻。
	StartAt         string `json:"start_at,omitempty"`
	EndAt           string `json:"end_at,omitempty"`
	DurationMinutes *int   `json:"duration_minutes,omitempty"`
}

// Instance 某天的一次任务出现——认领/完成的对象。
type Instance struct {
	ID             int64  `json:"id"`
	ChoreID        *int64 `json:"chore_id"`
	Title          string `json:"title"`
	Note           string `json:"note"`
	GroupID        *int64 `json:"group_id"`
	DueDate        string `json:"due_date"`
	State          string `json:"state"` // open | claimed | done
	ClaimedBy      *int64 `json:"claimed_by"`
	ClaimedAt      string `json:"claimed_at"`
	CompletedBy    *int64 `json:"completed_by"`
	CompletedAt    string `json:"completed_at"`
	CompletionNote string `json:"completion_note"`
	RequiresClaim  bool   `json:"requires_claim"`
	RequiresPhoto  bool   `json:"requires_photo"`
	CreatedBy      *int64 `json:"created_by"`
	CreatedAt      string `json:"created_at"`
	// v2：任务时间（RFC3339 UTC）。老数据为空 → 视图里是 null。
	StartAt         string `json:"start_at,omitempty"`
	EndAt           string `json:"end_at,omitempty"`
	DurationMinutes *int   `json:"duration_minutes,omitempty"`
}

// Activity 活动日志（谁在什么时候对哪条实例做了什么）。
type Activity struct {
	ID         int64  `json:"id"`
	At         string `json:"at"`
	ActorID    *int64 `json:"actor_id"`
	InstanceID *int64 `json:"instance_id"`
	Action     string `json:"action"`
	Detail     string `json:"detail"`
}

// dbFile 是落盘的整体结构。
type dbFile struct {
	Version   int               `json:"version"`
	NextID    int64             `json:"next_id"`
	Members   []Member          `json:"members"`
	Groups    []Group           `json:"groups"`
	Chores    []Chore           `json:"chores"`
	Instances []Instance        `json:"instances"`
	Activity  []Activity        `json:"activity"`
	Settings  map[string]string `json:"settings"`
}

// DB 是带进程内写锁的数据层。
type DB struct {
	mu   sync.RWMutex
	path string
	file dbFile
	// loc 是"今天"的判定时区（由 main 从 TZ 注入），不参与持久化。
	loc *time.Location
}

// New 打开（或创建）数据文件；父目录不存在会自动创建。
func New(path string) (*DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("store: empty db path")
	}
	d := &DB{path: path}
	d.file = dbFile{Version: formatVersion, Settings: map[string]string{}}
	if err := d.load(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.normalizeLocked()
	d.mu.Unlock()
	return d, nil
}

// Path 返回数据文件绝对路径（/api/admin/export 用它回文件）。
func (d *DB) Path() string { return d.path }

// load 读取磁盘；文件不存在按空库处理（第一次运行）。
func (d *DB) load() error {
	raw, err := os.ReadFile(d.path)
	if errors.Is(err, os.ErrNotExist) {
		return d.saveLocked()
	}
	if err != nil {
		return fmt.Errorf("store: read %s: %w", d.path, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return d.saveLocked()
	}
	var f dbFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("store: parse %s: %w", d.path, err)
	}
	if f.Settings == nil {
		f.Settings = map[string]string{}
	}
	if f.Version == 0 {
		f.Version = formatVersion
	}
	f.Version = formatVersion
	d.file = f
	return nil
}

// normalizeLocked 做一次自愈：漏字段补齐、每天任务补到今天。
func (d *DB) normalizeLocked() {
	if d.file.Settings == nil {
		d.file.Settings = map[string]string{}
	}
	// 补 next_id（老文件可能只有 0）
	maxID := int64(0)
	bump := func(v int64) {
		if v > maxID {
			maxID = v
		}
	}
	for _, m := range d.file.Members {
		bump(m.ID)
	}
	for _, g := range d.file.Groups {
		bump(g.ID)
	}
	for _, c := range d.file.Chores {
		bump(c.ID)
	}
	for _, i := range d.file.Instances {
		bump(i.ID)
	}
	for _, a := range d.file.Activity {
		bump(a.ID)
	}
	if d.file.NextID <= maxID {
		d.file.NextID = maxID + 1
	}
}

// saveLocked 原子落盘：先写同目录临时文件，再 rename 覆盖。
// 必须在持有写锁时调用。
func (d *DB) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(d.path), 0o755); err != nil {
		return fmt.Errorf("store: mkdir: %w", err)
	}
	raw, err := json.MarshalIndent(d.file, "", " ")
	if err != nil {
		return fmt.Errorf("store: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(d.path), ".chores-*.tmp")
	if err != nil {
		return fmt.Errorf("store: temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("store: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("store: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("store: close: %w", err)
	}
	if err := os.Rename(tmpName, d.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("store: rename: %w", err)
	}
	return nil
}

func (d *DB) nextIDLocked() int64 {
	id := d.file.NextID
	d.file.NextID++
	return id
}

// now 统一 RFC3339 UTC。
func now() string { return time.Now().UTC().Format(time.RFC3339) }

// ExpandHome 之类的路径处理不在这里——store 只认绝对/相对路径原样。

// ---- 成员 ----

// ListMembers 返回未归档成员，按 sort, id 排序。
func (d *DB) ListMembers() []Member {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]Member, 0, len(d.file.Members))
	for _, m := range d.file.Members {
		if !m.Archived {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// MemberByID 按 id 取成员（含已归档，历史要显示名字）。
func (d *DB) MemberByID(id int64) (Member, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.memberLocked(id)
}

func (d *DB) memberLocked(id int64) (Member, bool) {
	for _, m := range d.file.Members {
		if m.ID == id {
			return m, true
		}
	}
	return Member{}, false
}

// CreateMember 新增成员。
func (d *DB) CreateMember(name, color, avatar string) (Member, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Member{}, fmt.Errorf("%w: name 不能为空", ErrBadInput)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if color == "" {
		color = "#0A84FF"
	}
	if avatar == "" {
		avatar = firstRune(name)
	}
	sort := 0
	for _, m := range d.file.Members {
		if m.Sort >= sort {
			sort = m.Sort + 1
		}
	}
	m := Member{ID: d.nextIDLocked(), Name: name, Color: color, Avatar: avatar, Sort: sort, CreatedAt: now()}
	d.file.Members = append(d.file.Members, m)
	if err := d.saveLocked(); err != nil {
		return Member{}, err
	}
	return m, nil
}

// UpdateMember 局部更新；nil 表示不改。
func (d *DB) UpdateMember(id int64, name, color, avatar *string, sortOrder *int, isAdmin *bool) (Member, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	idx := -1
	for i := range d.file.Members {
		if d.file.Members[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Member{}, fmt.Errorf("%w: member %d", ErrNotFound, id)
	}
	m := &d.file.Members[idx]
	if name != nil {
		v := strings.TrimSpace(*name)
		if v == "" {
			return Member{}, fmt.Errorf("%w: name 不能为空", ErrBadInput)
		}
		m.Name = v
		if avatar == nil && m.Avatar == "" {
			m.Avatar = firstRune(v)
		}
	}
	if color != nil {
		m.Color = strings.TrimSpace(*color)
	}
	if avatar != nil {
		m.Avatar = strings.TrimSpace(*avatar)
	}
	if sortOrder != nil {
		m.Sort = *sortOrder
	}
	if isAdmin != nil {
		m.IsAdmin = *isAdmin
	}
	if err := d.saveLocked(); err != nil {
		return Member{}, err
	}
	return *m, nil
}

// ArchiveMember 软删成员；历史实例保留名字。
func (d *DB) ArchiveMember(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.file.Members {
		if d.file.Members[i].ID == id {
			d.file.Members[i].Archived = true
			d.logLocked(nil, nil, "member.archive", d.file.Members[i].Name)
			return d.saveLocked()
		}
	}
	return fmt.Errorf("%w: member %d", ErrNotFound, id)
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return s
}
