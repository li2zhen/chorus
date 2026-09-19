package store

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// 本文件实现契约 v2 的"任务时间：三选二"。
//
// 规则（api/CONTRACT.md 末尾「v2 追加」）：
//   - 给两个 → 算第三个
//   - 三个都给 → 以 start_at + duration_minutes 为准重算 end_at
//   - 只给一个 → ErrBadInput（400）
//   - 一个都不给 → start_at = 请求时刻，duration_minutes = 默认 10
//   - duration_minutes 必须是 1..1440 的整数；end_at 必须晚于 start_at
const (
	// DefaultDurationMinutes 是契约自带的默认时长。
	DefaultDurationMinutes = 10
	// MaxDurationMinutes 是时长上限（24 小时）。
	MaxDurationMinutes = 1440
)

// ResolveTime 把三选二的入参解析成完整的 (start, durationMinutes, end)。
// start/end 为 nil 表示请求体里没给；now 是"请求时刻"（由调用方按注入时区提供）。
func ResolveTime(start, end *time.Time, duration *int, now time.Time) (time.Time, int, time.Time, error) {
	if duration != nil && (*duration < 1 || *duration > MaxDurationMinutes) {
		return time.Time{}, 0, time.Time{}, fmt.Errorf("%w: 时长需要是 1~1440 分钟", ErrBadInput)
	}

	var s, e time.Time
	var d int
	hasS, hasE, hasD := start != nil, end != nil, duration != nil

	switch {
	case hasS && hasE && hasD:
		// 三个都给：以 start + duration 为准
		d = *duration
		s = start.UTC()
		e = s.Add(time.Duration(d) * time.Minute)
	case hasS && hasE:
		s = start.UTC()
		e = end.UTC()
		if !e.After(s) {
			return time.Time{}, 0, time.Time{}, fmt.Errorf("%w: 结束时间必须晚于开始时间", ErrBadInput)
		}
		d = int(math.Round(e.Sub(s).Minutes()))
		if d < 1 {
			d = 1
		}
		if d > MaxDurationMinutes {
			return time.Time{}, 0, time.Time{}, fmt.Errorf("%w: 时长需要是 1~1440 分钟", ErrBadInput)
		}
	case hasS && hasD:
		s = start.UTC()
		d = *duration
		e = s.Add(time.Duration(d) * time.Minute)
	case hasE && hasD:
		d = *duration
		e = end.UTC()
		s = e.Add(-time.Duration(d) * time.Minute)
	case hasS:
		return time.Time{}, 0, time.Time{}, fmt.Errorf("%w: 时间需要给两个：开始/时长/结束", ErrBadInput)
	case hasE:
		return time.Time{}, 0, time.Time{}, fmt.Errorf("%w: 时间需要给两个：开始/时长/结束", ErrBadInput)
	case hasD:
		return time.Time{}, 0, time.Time{}, fmt.Errorf("%w: 时间需要给两个：开始/时长/结束", ErrBadInput)
	default:
		// 一个都没给：用契约默认值
		d = DefaultDurationMinutes
		s = now.UTC()
		e = s.Add(time.Duration(d) * time.Minute)
	}
	if !e.After(s) {
		return time.Time{}, 0, time.Time{}, fmt.Errorf("%w: 结束时间必须晚于开始时间", ErrBadInput)
	}
	return s, d, e, nil
}

// ParseRFC3339 解析前端传来的时间字符串；空串返回 nil（表示没给）。
func ParseRFC3339(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("%w: 时间需为 RFC3339（如 2026-09-18T09:00:00Z）", ErrBadInput)
	}
	u := t.UTC()
	return &u, nil
}

// ParseInstant 比 ParseRFC3339 宽松一档：额外接受"服务器本地时间"的写法
//
//	"2026-09-18T17:59"                ← /api/bootstrap 的 time_defaults.start_at_local 就是这个格式，
//	                                     前端把它原样回传时必须能解析（否则预填→提交这条最自然的链路会 400）
//	"2026-09-18T17:59:00"
//	"2026-09-18 17:59"
//
// 带时区的（RFC3339）按原样转 UTC；不带时区的按服务器时区 loc 解释（loc 为 nil 时退回 time.Local）。
func ParseInstant(raw string, loc *time.Location) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		u := t.UTC()
		return &u, nil
	}
	if loc == nil {
		loc = time.Local
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			u := t.UTC()
			return &u, nil
		}
	}
	return nil, &fieldError{msg: "时间格式需为 RFC3339（2026-09-18T09:00:00Z）或本地时间（2026-09-18T09:00）"}
}

// AddMinutes 返回 t 加 d 分钟（便于 HTTP 层算 end_at 给前端预填）。
func AddMinutes(t time.Time, d int) time.Time {
	return t.Add(time.Duration(d) * time.Minute)
}

// FormatRFC3339 统一出参格式。
func FormatRFC3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }
