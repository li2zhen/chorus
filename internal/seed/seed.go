// Package seed 写入演示数据：4 个成员、2 个分组、6 个任务（含每天/每周/每月各一个）。
//
// 只在库为空时执行，重复调用不会重复插入。
package seed

import (
	"fmt"

	"chores/internal/store"
)

// Result 汇报本次实际插入了什么。
type Result struct {
	Seeded   bool     `json:"seeded"`
	Members  int      `json:"members"`
	Groups   int      `json:"groups"`
	Chores   int      `json:"chores"`
	Reasons  []string `json:"reasons,omitempty"`
	Existing bool     `json:"existing_db"`
}

// Apply 写入演示数据。库非空时直接返回 existing_db=true。
func Apply(db *store.DB) (Result, error) {
	if len(db.ListMembers()) > 0 || len(db.ListChores()) > 0 {
		return Result{Existing: true, Reasons: []string{"库非空，已跳过"}}, nil
	}
	res := Result{}

	// 成员：颜色取 iOS 系统色。
	members := []struct{ name, color, avatar string }{
		{"小明", "#0A84FF", "明"},
		{"小王", "#34C759", "王"},
		{"小李", "#FF9F0A", "李"},
		{"妈妈", "#FF375F", "妈"},
	}
	ids := map[string]int64{}
	for _, m := range members {
		created, err := db.CreateMember(m.name, m.color, m.avatar)
		if err != nil {
			return res, fmt.Errorf("seed: member %s: %w", m.name, err)
		}
		ids[m.name] = created.ID
		res.Members++
	}

	// 分组：家务（全员）/ 宠物（除妈妈）
	choreGroup, err := db.CreateGroup("家务", nil)
	if err != nil {
		return res, fmt.Errorf("seed: group 家务: %w", err)
	}
	res.Groups++
	petGroup, err := db.CreateGroup("宠物", []int64{ids["小明"], ids["小王"], ids["小李"]})
	if err != nil {
		return res, fmt.Errorf("seed: group 宠物: %w", err)
	}
	res.Groups++

	// 任务定义要给定循环字段
	b := func(v bool) *bool { return &v }
	s := func(v string) *string { return &v }
	i := func(v int) *int { return &v }
	gid := func(v int64) *int64 { return &v }
	// 用本月 1 日当 start_date：循环补齐会把"本月 1 日 → 今天"整段补上，
	// 前端「本月」日历前半截不会是空的。
	startToday := db.MonthStart()

	defs := []store.ChoreInput{
		{Title: s("倒垃圾"), Note: s("厨余和可回收分开"), GroupID: gid(choreGroup.ID), Recurrence: s("daily"), RequiresClaim: b(true), StartDate: s(startToday)},
		{Title: s("洗碗"), GroupID: gid(choreGroup.ID), Recurrence: s("daily"), RequiresClaim: b(true), StartDate: s(startToday)},
		{Title: s("清猫砂"), Note: s("结团铲干净"), GroupID: gid(petGroup.ID), Recurrence: s("daily"), MemberID: gid(ids["小王"]), RequiresClaim: b(true), StartDate: s(startToday)},
		{Title: s("拖地"), GroupID: gid(choreGroup.ID), Recurrence: s("weekly"), Weekday: i(6), RequiresClaim: b(true), StartDate: s(startToday)},
		{Title: s("换床单"), GroupID: gid(choreGroup.ID), Recurrence: s("weekly"), Weekday: i(0), RequiresClaim: b(true), StartDate: s(startToday)},
		{Title: s("清洗空调滤网"), GroupID: gid(choreGroup.ID), Recurrence: s("monthly"), DayOfMonth: i(1), RequiresClaim: b(true), StartDate: s(startToday)},
	}
	for _, d := range defs {
		if _, err := db.CreateChore(d); err != nil {
			return res, fmt.Errorf("seed: chore %v: %w", d.Title, err)
		}
		res.Chores++
	}

	res.Seeded = true
	return res, nil
}
