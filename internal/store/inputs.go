package store

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// ChoreInput 是创建/更新任务定义的入参（PATCH 语义：nil 表示不改）。
// 字段名与 api/CONTRACT.md 的 JSON 一一对应。
type ChoreInput struct {
	Title         *string `json:"title"`
	Note          *string `json:"note"`
	GroupID       *int64  `json:"group_id"`
	Recurrence    *string `json:"recurrence"`
	Weekday       *int    `json:"weekday"`
	DayOfMonth    *int    `json:"day_of_month"`
	DueTime       *string `json:"due_time"`
	MemberID      *int64  `json:"member_id"`
	RequiresClaim *bool   `json:"requires_claim"`
	RequiresPhoto *bool   `json:"requires_photo"`
	StartDate     *string `json:"start_date"`

	// v2：时间模板。三个都是指针 + 原始 JSON 标记：
	//   nil            → 请求里没这个字段（不改）
	//   present && nil → 显式给了 null（清空，回到"未设置"）
	//   present && set → 用这个值
	StartAt         *time.Time      `json:"-"`
	EndAt           *time.Time      `json:"-"`
	DurationMinutes *int            `json:"-"`
	RawStartAt      json.RawMessage `json:"start_at"`
	RawEndAt        json.RawMessage `json:"end_at"`
	RawDuration     json.RawMessage `json:"duration_minutes"`
}

// InstanceInput 是直接发布一个实例的入参。
type InstanceInput struct {
	Title         *string `json:"title"`
	Note          *string `json:"note"`
	GroupID       *int64  `json:"group_id"`
	DueDate       *string `json:"due_date"`
	MemberID      *int64  `json:"member_id"`
	RequiresClaim *bool   `json:"requires_claim"`
	// ActorID 不是请求体字段，由 HTTP 层用登录 Cookie 填。
	ActorID *int64 `json:"-"`

	// v2：任务时间（三选二）。Raw* 由 JSON 解码填充，StartAt/EndAt/DurationMinutes
	// 由 HTTP 层用 ParseTimeFields 解析后填入。
	StartAt         *time.Time
	EndAt           *time.Time
	DurationMinutes *int

	RawStartAt  json.RawMessage `json:"start_at"`
	RawEndAt    json.RawMessage `json:"end_at"`
	RawDuration json.RawMessage `json:"duration_minutes"`
}

// Nullable 标记一个字段在 JSON 里出现没出现。
type Nullable struct {
	Present bool
	IsNull  bool
}

// ParseTimeFields 读取三个原始 JSON 值并填进目标字段。
//
// 语义：字段没出现 → 保持 nil（PATCH 里表示"不改"）；出现且为 null → 显式清空；
// 出现且有值 → 解析成具体值。返回 (IsNull → 是否显式清空全部三个)。
func ParseTimeFields(rawStart, rawEnd, rawDur json.RawMessage, loc *time.Location) (Nullable, *time.Time, *time.Time, *int, error) {
	presence := Nullable{Present: len(rawStart) > 0 || len(rawEnd) > 0 || len(rawDur) > 0}
	allNull := true
	for _, raw := range []json.RawMessage{rawStart, rawEnd, rawDur} {
		if len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			allNull = false
		}
	}
	presence.IsNull = presence.Present && allNull
	if presence.IsNull {
		return presence, nil, nil, nil, nil
	}

	var start, end *time.Time
	var dur *int
	if len(rawStart) > 0 && !bytes.Equal(bytes.TrimSpace(rawStart), []byte("null")) {
		var s string
		if err := json.Unmarshal(rawStart, &s); err != nil {
			return presence, nil, nil, nil, badTime()
		}
		t, err := ParseInstant(s, loc)
		if err != nil {
			return presence, nil, nil, nil, err
		}
		start = t
	}
	if len(rawEnd) > 0 && !bytes.Equal(bytes.TrimSpace(rawEnd), []byte("null")) {
		var s string
		if err := json.Unmarshal(rawEnd, &s); err != nil {
			return presence, nil, nil, nil, badTime()
		}
		t, err := ParseInstant(s, loc)
		if err != nil {
			return presence, nil, nil, nil, err
		}
		end = t
	}
	if len(rawDur) > 0 && !bytes.Equal(bytes.TrimSpace(rawDur), []byte("null")) {
		var v int
		if err := json.Unmarshal(rawDur, &v); err != nil {
			return presence, nil, nil, nil, badDuration()
		}
		dur = &v
	}
	return presence, start, end, dur, nil
}

func badTime() error {
	return &fieldError{msg: "时间需为 RFC3339（如 2026-09-18T09:00:00Z）"}
}

func badDuration() error {
	return &fieldError{msg: "时长需要是 1~1440 分钟的整数"}
}

// fieldError 让 HTTP 层把它映射成 BAD_REQUEST。
type fieldError struct{ msg string }

func (e *fieldError) Error() string { return e.msg }

// Unwrap 让 *fieldError 也命中 HTTP 层的 ErrBadInput 分支（→ 400 而不是 500），
// 同时它的 Error() 已经是干净文案，不会再带内部前缀。
func (e *fieldError) Unwrap() error { return ErrBadInput }

// clearTemplate 判断 PATCH 是否要求清空时间模板。
func (in ChoreInput) ClearTemplate() bool {
	if !in.hasAnyRaw() {
		return false
	}
	return isEmptyRaw(in.RawStartAt) && isEmptyRaw(in.RawEndAt) && isEmptyRaw(in.RawDuration)
}

func (in ChoreInput) hasAnyRaw() bool {
	return len(in.RawStartAt) > 0 || len(in.RawEndAt) > 0 || len(in.RawDuration) > 0
}

func isEmptyRaw(raw json.RawMessage) bool {
	return len(raw) == 0 || strings.EqualFold(strings.TrimSpace(string(raw)), "null")
}
