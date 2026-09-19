package store

import (
	"fmt"
	"time"
)

// v2：循环定义上的时间当模板，生成的每个实例按自己的 due_date 平移，
// 保持"本地时刻"一致（例如每天 19:30 倒垃圾）。
//
// 为什么按本地时刻而不是 UTC：due_date 本身是本地日历日，
// 跨夏令时/换时区时用户期望的还是"那天 19:30"。
func (d *DB) templateTimesLocked(c *Chore, dueDate string) (string, string, *int, error) {
	if c.StartAt == "" {
		return "", "", nil, nil
	}
	tplStart, err := time.Parse(time.RFC3339, c.StartAt)
	if err != nil {
		// 模板坏了不该拖垮整个生成器：这一天就当作没有时间。
		return "", "", nil, nil
	}
	due, err := time.ParseInLocation(dateLayout, dueDate, d.locationLocked())
	if err != nil {
		return "", "", nil, fmt.Errorf("%w: due_date %q", ErrBadInput, dueDate)
	}
	loc := d.locationLocked()
	localStart := tplStart.In(loc)
	start := time.Date(due.Year(), due.Month(), due.Day(),
		localStart.Hour(), localStart.Minute(), localStart.Second(), localStart.Nanosecond(), loc)

	dur := DefaultDurationMinutes
	if c.DurationMinutes != nil {
		dur = *c.DurationMinutes
	}
	if c.EndAt != "" && c.DurationMinutes == nil {
		if tplEnd, err := time.Parse(time.RFC3339, c.EndAt); err == nil {
			if minutes := int(tplEnd.Sub(tplStart).Round(time.Minute).Minutes()); minutes >= 1 {
				dur = minutes
			}
		}
	}
	end := start.Add(time.Duration(dur) * time.Minute)
	return FormatRFC3339(start), FormatRFC3339(end), &dur, nil
}

// locationLocked 是持锁版本；store 内部多处在写锁里读时区，避免重复取锁造成死锁。
func (d *DB) locationLocked() *time.Location {
	if d.loc == nil {
		return time.Local
	}
	return d.loc
}
