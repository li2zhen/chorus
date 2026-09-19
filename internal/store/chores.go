package store

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

var validRecurrence = map[string]bool{"none": true, "daily": true, "weekly": true, "monthly": true}

// ListChores 返回未归档的任务定义（管理页用）。
func (d *DB) ListChores() []Chore {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]Chore, 0, len(d.file.Chores))
	for _, c := range d.file.Chores {
		if !c.Archived {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// CreateChore 建定义并补齐从 start_date 到今天的实例。
func (d *DB) CreateChore(in ChoreInput) (Chore, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, err := d.buildChoreLocked(in, nil)
	if err != nil {
		return Chore{}, err
	}
	c.ID = d.nextIDLocked()
	d.file.Chores = append(d.file.Chores, c)
	d.logLocked(nil, nil, "chore.create", c.Title)
	if err := d.generateLocked(time.Now()); err != nil {
		return Chore{}, err
	}
	if err := d.saveLocked(); err != nil {
		return Chore{}, err
	}
	return c, nil
}

// UpdateChore 局部更新；recurrence 变更只影响未来实例。
func (d *DB) UpdateChore(id int64, in ChoreInput) (Chore, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	idx := -1
	for i := range d.file.Chores {
		if d.file.Chores[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Chore{}, fmt.Errorf("%w: chore %d", ErrNotFound, id)
	}
	updated, err := d.buildChoreLocked(in, &d.file.Chores[idx])
	if err != nil {
		return Chore{}, err
	}
	updated.ID = d.file.Chores[idx].ID
	updated.CreatedAt = d.file.Chores[idx].CreatedAt
	updated.Archived = d.file.Chores[idx].Archived
	changedRecurrence := updated.Recurrence != d.file.Chores[idx].Recurrence ||
		!samePtr(updated.Weekday, d.file.Chores[idx].Weekday) ||
		!samePtr(updated.DayOfMonth, d.file.Chores[idx].DayOfMonth)
	d.file.Chores[idx] = updated
	// 未完成的实例跟随新标题/备注/分组/负责人（历史已完成的不动）
	for i := range d.file.Instances {
		inst := &d.file.Instances[i]
		if inst.ChoreID == nil || *inst.ChoreID != id || inst.State == "done" {
			continue
		}
		inst.Title = updated.Title
		inst.Note = updated.Note
		inst.GroupID = updated.GroupID
		inst.RequiresClaim = updated.RequiresClaim
		inst.RequiresPhoto = updated.RequiresPhoto
	}
	d.logLocked(nil, nil, "chore.update", updated.Title)
	if err := d.generateLocked(time.Now()); err != nil {
		return Chore{}, err
	}
	_ = changedRecurrence
	if err := d.saveLocked(); err != nil {
		return Chore{}, err
	}
	return updated, nil
}

// ArchiveChore 软删定义；历史实例保留。
func (d *DB) ArchiveChore(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.file.Chores {
		if d.file.Chores[i].ID == id {
			d.file.Chores[i].Archived = true
			d.logLocked(nil, nil, "chore.archive", d.file.Chores[i].Title)
			return d.saveLocked()
		}
	}
	return fmt.Errorf("%w: chore %d", ErrNotFound, id)
}

// buildChoreLocked 校验并构造定义；base 非空时表示更新（未给字段沿用旧值）。
func (d *DB) buildChoreLocked(in ChoreInput, base *Chore) (Chore, error) {
	c := Chore{
		Recurrence:    "none",
		RequiresClaim: true,
		StartDate:     d.timeNowDate(),
	}
	if base != nil {
		c = *base
	}
	if in.Title != nil {
		c.Title = strings.TrimSpace(*in.Title)
	}
	if in.Note != nil {
		c.Note = strings.TrimSpace(*in.Note)
	}
	if in.GroupID != nil {
		if *in.GroupID == 0 {
			c.GroupID = nil
		} else {
			if _, ok := d.groupLocked(*in.GroupID); !ok {
				return Chore{}, fmt.Errorf("%w: group %d", ErrBadInput, *in.GroupID)
			}
			v := *in.GroupID
			c.GroupID = &v
		}
	}
	if in.Recurrence != nil {
		c.Recurrence = strings.TrimSpace(*in.Recurrence)
	}
	if in.Weekday != nil {
		if *in.Weekday < 0 || *in.Weekday > 6 {
			return Chore{}, fmt.Errorf("%w: weekday 需为 0..6", ErrBadInput)
		}
		v := *in.Weekday
		c.Weekday = &v
	}
	if in.DayOfMonth != nil {
		if *in.DayOfMonth < 1 || *in.DayOfMonth > 31 {
			return Chore{}, fmt.Errorf("%w: day_of_month 需为 1..31", ErrBadInput)
		}
		v := *in.DayOfMonth
		c.DayOfMonth = &v
	}
	if in.DueTime != nil {
		c.DueTime = strings.TrimSpace(*in.DueTime)
	}
	if in.MemberID != nil {
		if *in.MemberID == 0 {
			c.MemberID = nil
		} else {
			if err := d.checkMembersLocked([]int64{*in.MemberID}); err != nil {
				return Chore{}, err
			}
			v := *in.MemberID
			c.MemberID = &v
		}
	}
	if in.RequiresClaim != nil {
		c.RequiresClaim = *in.RequiresClaim
	}
	if in.RequiresPhoto != nil {
		c.RequiresPhoto = *in.RequiresPhoto
	}
	if in.StartDate != nil {
		c.StartDate = strings.TrimSpace(*in.StartDate)
	}
	// v2：时间模板（三选二）。
	//   - 请求里没有这三个字段 → 新建用默认值（现在+10min），更新沿用旧模板
	//   - 显式全给 null        → 清空模板
	//   - 给了其中若干         → 按"三选二"解析（只给一个会在这里 400）
	switch {
	case in.ClearTemplate():
		c.StartAt, c.EndAt, c.DurationMinutes = "", "", nil
	case in.StartAt != nil || in.EndAt != nil || in.DurationMinutes != nil:
		start, dur, end, err := ResolveTime(in.StartAt, in.EndAt, in.DurationMinutes, d.nowLocalLocked())
		if err != nil {
			return Chore{}, err
		}
		c.StartAt = FormatRFC3339(start)
		c.EndAt = FormatRFC3339(end)
		c.DurationMinutes = &dur
	case base == nil:
		start, dur, end, err := ResolveTime(nil, nil, nil, d.nowLocalLocked())
		if err != nil {
			return Chore{}, err
		}
		c.StartAt = FormatRFC3339(start)
		c.EndAt = FormatRFC3339(end)
		c.DurationMinutes = &dur
	}
	if c.Title == "" {
		return Chore{}, fmt.Errorf("%w: title 不能为空", ErrBadInput)
	}
	if !validRecurrence[c.Recurrence] {
		return Chore{}, fmt.Errorf("%w: recurrence 需为 none/daily/weekly/monthly", ErrBadInput)
	}
	if c.StartDate == "" {
		c.StartDate = d.timeNowDate()
	}
	if _, err := parseDate(c.StartDate); err != nil {
		return Chore{}, fmt.Errorf("%w: start_date 需为 YYYY-MM-DD", ErrBadInput)
	}
	if c.Recurrence == "weekly" && c.Weekday == nil {
		return Chore{}, fmt.Errorf("%w: weekly 需要 weekday", ErrBadInput)
	}
	if c.Recurrence == "monthly" && c.DayOfMonth == nil {
		return Chore{}, fmt.Errorf("%w: monthly 需要 day_of_month", ErrBadInput)
	}
	if c.CreatedAt == "" {
		c.CreatedAt = now()
	}
	return c, nil
}

func samePtr(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
