package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// v2 之前写入的实例没有时间字段：读进来必须是零值，再次落盘时也必须"省略"这三个键
// （而不是写成 ""/0），否则前端会把空串/0 当成有效时间渲染。
func TestLegacyInstanceWithoutTimesStaysNull(t *testing.T) {
	legacy := []byte(`{"id":1,"title":"倒垃圾","due_date":"2026-09-01","state":"open","requires_claim":true}`)
	var inst Instance
	if err := json.Unmarshal(legacy, &inst); err != nil {
		t.Fatalf("unmarshal legacy instance: %v", err)
	}
	if inst.StartAt != "" || inst.EndAt != "" || inst.DurationMinutes != nil {
		t.Fatalf("老实例的时间字段应为零值，得到 %q/%q/%v", inst.StartAt, inst.EndAt, inst.DurationMinutes)
	}
	out, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"start_at", "end_at", "duration_minutes"} {
		if strings.Contains(string(out), `"`+key+`"`) {
			t.Fatalf("零值时间不该出现在落盘 JSON 里（发现 %s）：%s", key, out)
		}
	}
}

// 有效时间必须完整往返（omitempty 不能把真值吃掉）。
func TestInstanceWithTimesRoundTrips(t *testing.T) {
	dur := 20
	in := Instance{
		ID: 2, Title: "拖地", DueDate: "2026-09-18", State: "open",
		StartAt: "2026-09-18T11:30:00Z", EndAt: "2026-09-18T11:50:00Z", DurationMinutes: &dur,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Instance
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.StartAt != in.StartAt || back.EndAt != in.EndAt ||
		back.DurationMinutes == nil || *back.DurationMinutes != dur {
		t.Fatalf("时间字段往返不一致: %s", raw)
	}
}

// 回归：/api/bootstrap 的 time_defaults.start_at_local 是"无时区的本地时间"，
// 前端把它原样回传必须能解析（否则预填→提交这条最自然的链路会 400）。
// 以及"只给一个字段"必须是契约原文的报错。
func TestParseInstantAcceptsLocalPrefillFormat(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	got, err := ParseInstant("2026-09-18T17:59", loc)
	if err != nil || got == nil {
		t.Fatalf("本地裸时间应被接受: %v", err)
	}
	if want := "2026-09-18T09:59:00Z"; got.UTC().Format(time.RFC3339) != want {
		t.Fatalf("应按服务器时区解释: got %s want %s", got.UTC().Format(time.RFC3339), want)
	}
	// 带时区的 RFC3339 仍按原意解析
	rfc, err := ParseInstant("2026-09-18T17:59:00+08:00", loc)
	if err != nil || rfc == nil || rfc.UTC().Format(time.RFC3339) != "2026-09-18T09:59:00Z" {
		t.Fatalf("RFC3339 解析异常: %v %v", rfc, err)
	}
	// 只给一个 → 契约原文
	if _, _, _, err := ResolveTime(got, nil, nil, *got); err == nil ||
		!strings.Contains(err.Error(), "时间需要给两个：开始/时长/结束") {
		t.Fatalf("只给一个字段的错误应为契约原文，得到 %v", err)
	}
}
