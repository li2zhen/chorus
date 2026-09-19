package store

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// dateLayout 是契约里的日期格式。
const dateLayout = "2006-01-02"

// 时间辅助：D 的 tz 决定"今天"，但 store 不持有 tz——
// 生成器与时间判断统一走 d.nowLocal()，由 SetLocation 注入。
var defaultLocation = time.Local

// SetLocation 注入运行时时区（main 从 TZ 读）。
func (d *DB) SetLocation(loc *time.Location) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if loc != nil {
		d.loc = loc
	}
}

// nowLocal 返回注入时区的当前时间。
func (d *DB) nowLocal() time.Time {
	d.mu.RLock()
	loc := d.loc
	d.mu.RUnlock()
	if loc == nil {
		loc = defaultLocation
	}
	return time.Now().In(loc)
}

func (d *DB) location() *time.Location {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.loc == nil {
		return defaultLocation
	}
	return d.loc
}

// timeNowDate 取"今天"；只能在持有写锁时调用（它直接读 d.loc）。
func (d *DB) timeNowDate() string {
	return d.nowLocalLocked().Format(dateLayout)
}

// nowLocalLocked 是持有写锁时的"现在"；nowLocal() 是锁外的版本。
func (d *DB) nowLocalLocked() time.Time {
	loc := d.loc
	if loc == nil {
		loc = time.Local
	}
	return time.Now().In(loc)
}

func parseDate(s string) (time.Time, error) {
	return time.ParseInLocation(dateLayout, strings.TrimSpace(s), time.UTC)
}

func formatDate(t time.Time) string { return t.Format(dateLayout) }

// Query 是实例筛选条件。
type Query struct {
	From     string
	To       string
	MemberID int64
	GroupID  int64
	State    string
}

// ListInstances 按条件返回实例；默认按 due_date, id 升序。
func (d *DB) ListInstances(q Query) []Instance {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]Instance, 0, len(d.file.Instances))
	for _, inst := range d.file.Instances {
		if inst.Archived {
			continue // v5：已删除的实例对任何视图都不可见
		}
		if q.From != "" && inst.DueDate < q.From {
			continue
		}
		if q.To != "" && inst.DueDate > q.To {
			continue
		}
		if q.State != "" && inst.State != q.State {
			continue
		}
		if q.GroupID > 0 && (inst.GroupID == nil || *inst.GroupID != q.GroupID) {
			continue
		}
		if q.MemberID > 0 {
			claimed := inst.ClaimedBy != nil && *inst.ClaimedBy == q.MemberID
			completed := inst.CompletedBy != nil && *inst.CompletedBy == q.MemberID
			if !claimed && !completed {
				continue
			}
		}
		out = append(out, inst)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DueDate != out[j].DueDate {
			return out[i].DueDate < out[j].DueDate
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// InstanceByID 取单个实例。
func (d *DB) InstanceByID(id int64) (Instance, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	inst, ok := d.instanceLocked(id)
	if !ok {
		return Instance{}, fmt.Errorf("%w: instance %d", ErrNotFound, id)
	}
	return inst, nil
}

func (d *DB) instanceLocked(id int64) (Instance, bool) {
	for _, inst := range d.file.Instances {
		if inst.ID == id && !inst.Archived {
			return inst, true
		}
	}
	return Instance{}, false
}

// CreateInstance 立即发布一个实例；给了 member_id 就直接置为已认领。
// v2：先按"三选二"把时间解析出来（解析失败直接 400，不落库）。
func (d *DB) CreateInstance(in InstanceInput) (Instance, error) {
	title := ""
	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
	}
	if title == "" {
		return Instance{}, fmt.Errorf("%w: title 不能为空", ErrBadInput)
	}
	due := time.Now().Format(dateLayout)
	if in.DueDate != nil && strings.TrimSpace(*in.DueDate) != "" {
		due = strings.TrimSpace(*in.DueDate)
	}
	if _, err := parseDate(due); err != nil {
		return Instance{}, fmt.Errorf("%w: due_date 需为 YYYY-MM-DD", ErrBadInput)
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	startAt, duration, endAt, err := ResolveTime(in.StartAt, in.EndAt, in.DurationMinutes, d.nowLocalLocked())
	if err != nil {
		return Instance{}, err
	}

	inst := Instance{
		ID:              d.nextIDLocked(),
		Title:           title,
		DueDate:         due,
		State:           "open",
		CreatedBy:       in.ActorID,
		CreatedAt:       now(),
		RequiresClaim:   true,
		StartAt:         FormatRFC3339(startAt),
		EndAt:           FormatRFC3339(endAt),
		DurationMinutes: &duration,
	}
	if in.Note != nil {
		inst.Note = strings.TrimSpace(*in.Note)
	}
	if in.RequiresClaim != nil {
		inst.RequiresClaim = *in.RequiresClaim
	}
	if in.GroupID != nil && *in.GroupID > 0 {
		if _, ok := d.groupLocked(*in.GroupID); !ok {
			return Instance{}, fmt.Errorf("%w: group %d", ErrBadInput, *in.GroupID)
		}
		v := *in.GroupID
		inst.GroupID = &v
	}
	if in.MemberID != nil && *in.MemberID > 0 {
		if err := d.checkMembersLocked([]int64{*in.MemberID}); err != nil {
			return Instance{}, err
		}
		v := *in.MemberID
		inst.ClaimedBy = &v
		inst.ClaimedAt = now()
		inst.State = "claimed"
	}
	d.file.Instances = append(d.file.Instances, inst)
	d.logLocked(in.ActorID, &inst.ID, "instance.publish", inst.Title)
	if err := d.saveLocked(); err != nil {
		return Instance{}, err
	}
	return inst, nil
}

// Claim 认领：写锁内检查 state==open，输的一方拿到 ErrConflict + 当前实例。
//
// 注意：这是唯一没有走 mutate 的状态动作（它要返回"当前实例"给抢占失败的一方），
// 所以这里的查找必须自己带上 archived 判断——已删除的实例一律当"不存在"（404），
// 否则会出现「删掉的实例还能被认领」这种状态被写进去的 bug。
func (d *DB) Claim(instanceID, memberID int64) (Instance, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	idx := -1
	for i := range d.file.Instances {
		if d.file.Instances[i].ID == instanceID && !d.file.Instances[i].Archived {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Instance{}, fmt.Errorf("%w: instance %d", ErrNotFound, instanceID)
	}
	inst := &d.file.Instances[idx]
	if inst.State != "open" {
		return *inst, fmt.Errorf("%w: 已被认领", ErrConflict)
	}
	if m, ok := d.memberLocked(memberID); !ok || m.Archived {
		return Instance{}, fmt.Errorf("%w: member %d", ErrBadInput, memberID)
	}
	who := memberID
	inst.State = "claimed"
	inst.ClaimedBy = &who
	inst.ClaimedAt = now()
	d.logLocked(&memberID, &inst.ID, "instance.claim", inst.Title)
	if err := d.saveLocked(); err != nil {
		return Instance{}, err
	}
	return *inst, nil
}

// Release 放弃认领：claimed→open，仅认领人本人或管理员。
func (d *DB) Release(instanceID, actorID int64, isAdmin bool) (Instance, error) {
	return d.mutate(instanceID, actorID, isAdmin, "instance.release", func(inst *Instance) error {
		if inst.State != "claimed" {
			return fmt.Errorf("%w: 当前状态不是已认领", ErrConflict)
		}
		if !isAdmin && (inst.ClaimedBy == nil || *inst.ClaimedBy != actorID) {
			return fmt.Errorf("%w: 只能放弃自己认领的任务", ErrForbidden)
		}
		inst.State = "open"
		inst.ClaimedBy = nil
		inst.ClaimedAt = ""
		return nil
	})
}

// Complete 完成：→done；未认领也可直接完成（记完成人）。
func (d *DB) Complete(instanceID, actorID int64, note string) (Instance, error) {
	return d.mutate(instanceID, actorID, false, "instance.complete", func(inst *Instance) error {
		if inst.State == "done" {
			return fmt.Errorf("%w: 已完成", ErrConflict)
		}
		who := actorID
		inst.State = "done"
		inst.CompletedBy = &who
		inst.CompletedAt = now()
		inst.CompletionNote = strings.TrimSpace(note)
		if inst.ClaimedBy == nil {
			inst.ClaimedBy = &who
			inst.ClaimedAt = inst.CompletedAt
		}
		return nil
	})
}

// Uncomplete 撤销完成：done→open，清空完成字段，仅完成人本人或管理员。
func (d *DB) Uncomplete(instanceID, actorID int64, isAdmin bool) (Instance, error) {
	return d.mutate(instanceID, actorID, isAdmin, "instance.uncomplete", func(inst *Instance) error {
		if inst.State != "done" {
			return fmt.Errorf("%w: 当前状态不是已完成", ErrConflict)
		}
		if !isAdmin && (inst.CompletedBy == nil || *inst.CompletedBy != actorID) {
			return fmt.Errorf("%w: 只能撤销自己完成的任务", ErrForbidden)
		}
		inst.State = "open"
		inst.CompletedBy = nil
		inst.CompletedAt = ""
		inst.CompletionNote = ""
		inst.ClaimedBy = nil
		inst.ClaimedAt = ""
		return nil
	})
}

// Reschedule 改期（v5 补丁）：任意已登录成员都可以改任意实例的日期。
// 只改这一天的实例，不动它所属的系列。
func (d *DB) Reschedule(instanceID, actorID int64, isAdmin bool, dueDate string) (Instance, error) {
	dueDate = strings.TrimSpace(dueDate)
	if _, err := parseDate(dueDate); err != nil {
		return Instance{}, fmt.Errorf("%w: due_date 需为 YYYY-MM-DD", ErrBadInput)
	}
	return d.mutate(instanceID, actorID, isAdmin, "instance.reschedule", func(inst *Instance) error {
		// v5 补丁：改期不再限制"发布者或管理员"——家里谁都能把今天的活儿挪到明天；
		// 而且循环任务生成的实例没有发布者（CreatedBy 为空），旧规则会让改期永远 403。
		// 现在只要求"已登录成员"（HTTP 层已把过关）。
		inst.DueDate = dueDate
		return nil
	})
}

// mutate 是三个状态动作共用的"写锁内读-判断-改-落盘"骨架。
func (d *DB) mutate(instanceID, actorID int64, isAdmin bool, action string, fn func(*Instance) error) (Instance, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	idx := -1
	for i := range d.file.Instances {
		// 已删除（archived）的实例对状态动作不可见，与 instanceLocked / UpdateInstance 一致。
		if d.file.Instances[i].ID == instanceID && !d.file.Instances[i].Archived {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Instance{}, fmt.Errorf("%w: instance %d", ErrNotFound, instanceID)
	}
	inst := &d.file.Instances[idx]
	if err := fn(inst); err != nil {
		return *inst, err
	}
	d.logLocked(&actorID, &inst.ID, action, inst.Title)
	if err := d.saveLocked(); err != nil {
		return Instance{}, err
	}
	return *inst, nil
}

// UpdateInstanceInput 是"只改这一条实例"的入参（PATCH /api/instances/{id}）。
// nil = 不改；GroupID 传 0 表示清空分组。
type UpdateInstanceInput struct {
	Title           *string
	Note            *string
	GroupID         *int64
	StartAt         *time.Time
	EndAt           *time.Time
	DurationMinutes *int
}

// UpdateInstance 只改这一条实例，不动它所属的定义（v5）。
// 时间三个字段若给了，按"三选二"解析；给一个 → 400。
func (d *DB) UpdateInstance(id int64, in UpdateInstanceInput) (Instance, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	idx := -1
	for i := range d.file.Instances {
		if d.file.Instances[i].ID == id && !d.file.Instances[i].Archived {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Instance{}, fmt.Errorf("%w: instance %d", ErrNotFound, id)
	}
	inst := &d.file.Instances[idx]
	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" {
			return Instance{}, fmt.Errorf("%w: title 不能为空", ErrBadInput)
		}
		inst.Title = title
	}
	if in.Note != nil {
		inst.Note = strings.TrimSpace(*in.Note)
	}
	if in.GroupID != nil {
		if *in.GroupID == 0 {
			inst.GroupID = nil
		} else {
			if _, ok := d.groupLocked(*in.GroupID); !ok {
				return Instance{}, fmt.Errorf("%w: group %d", ErrBadInput, *in.GroupID)
			}
			v := *in.GroupID
			inst.GroupID = &v
		}
	}
	if in.StartAt != nil || in.EndAt != nil || in.DurationMinutes != nil {
		start, dur, end, err := ResolveTime(in.StartAt, in.EndAt, in.DurationMinutes, d.nowLocalLocked())
		if err != nil {
			return Instance{}, err
		}
		inst.StartAt = FormatRFC3339(start)
		inst.EndAt = FormatRFC3339(end)
		inst.DurationMinutes = &dur
	}
	d.logLocked(nil, &inst.ID, "instance.update", inst.Title)
	if err := d.saveLocked(); err != nil {
		return Instance{}, err
	}
	return *inst, nil
}

// DeleteInstance 只删这一条（幂等：重复删也返回 nil）。
// 不物理删除，标记 Archived；生成器把"已有实例"的键也算上它，
// 所以删掉某天的循环任务实例不会在下一轮被重新生成。
func (d *DB) DeleteInstance(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.file.Instances {
		inst := &d.file.Instances[i]
		if inst.ID != id {
			continue
		}
		if inst.Archived {
			return nil // 幂等
		}
		inst.Archived = true
		d.logLocked(nil, &inst.ID, "instance.delete", inst.Title)
		return d.saveLocked()
	}
	return fmt.Errorf("%w: instance %d", ErrNotFound, id)
}
