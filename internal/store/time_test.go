package store

import (
	"errors"
	"testing"
	"time"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad time %q: %v", s, err)
	}
	return v
}

// 契约 v2 "三选二"的六种组合。
func TestResolveTimeCombinations(t *testing.T) {
	now := mustParse(t, "2026-09-18T08:00:00Z")
	s := mustParse(t, "2026-09-18T09:00:00Z")
	e := mustParse(t, "2026-09-18T09:30:00Z")
	thirty := 30
	ten := 10
	zero := 0

	t.Run("一个都不给 → 默认 10 分钟", func(t *testing.T) {
		gs, gd, ge, err := ResolveTime(nil, nil, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		if !gs.Equal(now.UTC()) || gd != DefaultDurationMinutes || !ge.Equal(now.UTC().Add(10*time.Minute)) {
			t.Fatalf("got %v %d %v", gs, gd, ge)
		}
	})

	t.Run("开始+结束 → 算时长", func(t *testing.T) {
		_, gd, ge, err := ResolveTime(&s, &e, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		if gd != 30 || !ge.Equal(e.UTC()) {
			t.Fatalf("got %d %v", gd, ge)
		}
	})

	t.Run("开始+时长 → 算结束", func(t *testing.T) {
		gs, gd, ge, err := ResolveTime(&s, nil, &thirty, now)
		if err != nil {
			t.Fatal(err)
		}
		if !gs.Equal(s.UTC()) || gd != 30 || !ge.Equal(s.UTC().Add(30*time.Minute)) {
			t.Fatalf("got %v %d %v", gs, gd, ge)
		}
	})

	t.Run("结束+时长 → 算开始", func(t *testing.T) {
		gs, gd, ge, err := ResolveTime(nil, &e, &thirty, now)
		if err != nil {
			t.Fatal(err)
		}
		if !ge.Equal(e.UTC()) || gd != 30 || !gs.Equal(e.UTC().Add(-30*time.Minute)) {
			t.Fatalf("got %v %d %v", gs, gd, ge)
		}
	})

	t.Run("三个都给 → 以开始+时长为准重算结束", func(t *testing.T) {
		gs, gd, ge, err := ResolveTime(&s, &e, &ten, now)
		if err != nil {
			t.Fatal(err)
		}
		if !gs.Equal(s.UTC()) || gd != 10 || !ge.Equal(s.UTC().Add(10*time.Minute)) {
			t.Fatalf("got %v %d %v", gs, gd, ge)
		}
	})

	var nilTime *time.Time
	var nilInt *int
	for name, args := range map[string][3]any{
		"只给开始": {&s, nilTime, nilInt},
		"只给结束": {nilTime, &e, nilInt},
		"只给时长": {nilTime, nilTime, &ten},
	} {
		t.Run(name+" → 400", func(t *testing.T) {
			start, end, dur := args[0].(*time.Time), args[1].(*time.Time), args[2].(*int)
			_, _, _, err := ResolveTime(start, end, dur, now)
			if !errors.Is(err, ErrBadInput) {
				t.Fatalf("want ErrBadInput, got %v", err)
			}
		})
	}

	t.Run("时长越界 → 400", func(t *testing.T) {
		if _, _, _, err := ResolveTime(&s, nil, &zero, now); !errors.Is(err, ErrBadInput) {
			t.Fatalf("0 分钟应被拒，got %v", err)
		}
		big := MaxDurationMinutes + 1
		if _, _, _, err := ResolveTime(&s, nil, &big, now); !errors.Is(err, ErrBadInput) {
			t.Fatalf("1441 分钟应被拒，got %v", err)
		}
	})

	t.Run("结束不晚于开始 → 400", func(t *testing.T) {
		if _, _, _, err := ResolveTime(&s, &s, nil, now); !errors.Is(err, ErrBadInput) {
			t.Fatalf("相等应被拒，got %v", err)
		}
	})
}
