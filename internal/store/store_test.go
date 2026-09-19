package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// newTestDB 建一个临时库，时区固定 Asia/Shanghai（与 compose 一致）。
func newTestDB(t *testing.T) *DB {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load tz: %v", err)
	}
	db, err := New(filepath.Join(t.TempDir(), "chores.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	db.SetLocation(loc)
	return db
}

func mustMember(t *testing.T, db *DB, name string) Member {
	t.Helper()
	m, err := db.CreateMember(name, "#0A84FF", "")
	if err != nil {
		t.Fatalf("create member %s: %v", name, err)
	}
	return m
}

func strp(s string) *string { return &s }
func boolp(b bool) *bool    { return &b }
func intp(i int) *int       { return &i }

// C1：daily 从 start_date 起每天都有一条实例。
func TestDailyGeneratesEveryDay(t *testing.T) {
	db := newTestDB(t)
	start := db.Today()
	if _, err := db.CreateChore(ChoreInput{
		Title: strp("倒垃圾"), Recurrence: strp("daily"), StartDate: strp(start), RequiresClaim: boolp(true),
	}); err != nil {
		t.Fatalf("create chore: %v", err)
	}
	from := time.Now().In(db.location()).AddDate(0, 0, 7).Format(dateLayout)
	list := db.ListInstances(Query{From: from, To: from})
	if len(list) != 1 {
		t.Fatalf("7 天后应有 1 条，实际 %d 条", len(list))
	}
	if list[0].State != "open" {
		t.Fatalf("新实例应为 open，实际 %s", list[0].State)
	}
}

// C2：重复补齐是幂等的（UNIQUE(chore_id, due_date) 语义）。
func TestGenerateIsIdempotent(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.CreateChore(ChoreInput{
		Title: strp("洗碗"), Recurrence: strp("daily"), StartDate: strp(db.Today()), RequiresClaim: boolp(true),
	}); err != nil {
		t.Fatalf("create chore: %v", err)
	}
	first := len(db.ListInstances(Query{}))
	for i := 0; i < 3; i++ {
		if err := db.Generate(); err != nil {
			t.Fatalf("generate: %v", err)
		}
	}
	if second := len(db.ListInstances(Query{})); second != first {
		t.Fatalf("补齐不幂等：%d -> %d", first, second)
	}
}

// C3：weekly 只落在指定星期几。
func TestWeeklyOnlyOnWeekday(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.CreateChore(ChoreInput{
		Title: strp("拖地"), Recurrence: strp("weekly"), Weekday: intp(6), StartDate: strp(db.Today()), RequiresClaim: boolp(true),
	}); err != nil {
		t.Fatalf("create chore: %v", err)
	}
	loc := db.location()
	from := time.Now().In(loc).Format(dateLayout)
	to := time.Now().In(loc).AddDate(0, 0, 60).Format(dateLayout)
	list := db.ListInstances(Query{From: from, To: to})
	if len(list) < 8 {
		t.Fatalf("60 天里周六应有 >=8 条，实际 %d", len(list))
	}
	for _, inst := range list {
		d, err := time.ParseInLocation(dateLayout, inst.DueDate, loc)
		if err != nil {
			t.Fatalf("parse %s: %v", inst.DueDate, err)
		}
		if d.Weekday() != time.Saturday {
			t.Fatalf("%s 不是周六", inst.DueDate)
		}
	}
}

// C3：monthly 落在指定日；该月无此日时落到月末。
func TestMonthlyFallsBackToMonthEnd(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.CreateChore(ChoreInput{
		Title: strp("31 号大扫除"), Recurrence: strp("monthly"), DayOfMonth: intp(31), StartDate: strp(db.Today()), RequiresClaim: boolp(true),
	}); err != nil {
		t.Fatalf("create chore: %v", err)
	}
	list := db.ListInstances(Query{})
	if len(list) == 0 {
		t.Fatal("没有生成 monthly 实例")
	}
	loc := db.location()
	for _, inst := range list {
		d, err := time.ParseInLocation(dateLayout, inst.DueDate, loc)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if d.Day() != daysInMonth(d.Year(), d.Month()) {
			t.Fatalf("%s 既不是 31 号也不是月末", inst.DueDate)
		}
	}
}

// B5：并发认领只能有一个赢家，其余拿到 ErrConflict 与最新实例。
func TestClaimIsRaceSafe(t *testing.T) {
	db := newTestDB(t)
	a := mustMember(t, db, "小明")
	b := mustMember(t, db, "小王")
	inst, err := db.CreateInstance(InstanceInput{Title: strp("倒垃圾"), DueDate: strp(db.Today()), RequiresClaim: boolp(true), ActorID: &a.ID})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	const racers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, conflicts := 0, 0
	for i := 0; i < racers; i++ {
		wg.Add(1)
		who := a.ID
		if i%2 == 1 {
			who = b.ID
		}
		go func(member int64) {
			defer wg.Done()
			_, err := db.Claim(inst.ID, member)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case err != nil && errors.Is(err, ErrConflict):
				conflicts++
			default:
				t.Errorf("意外错误: %v", err)
			}
		}(who)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("应只有 1 个赢家，实际 %d", wins)
	}
	if conflicts != racers-1 {
		t.Fatalf("应有 %d 个冲突，实际 %d", racers-1, conflicts)
	}
	got, err := db.InstanceByID(inst.ID)
	if err != nil {
		t.Fatalf("读实例: %v", err)
	}
	if got.State != "claimed" || got.ClaimedBy == nil {
		t.Fatalf("认领后状态不对: %+v", got)
	}
}

// B6/B7：完成写谁/何时，撤销后回到 open 且清空完成字段。
func TestCompleteAndUncomplete(t *testing.T) {
	db := newTestDB(t)
	m := mustMember(t, db, "小李")
	inst, err := db.CreateInstance(InstanceInput{Title: strp("洗碗"), DueDate: strp(db.Today()), RequiresClaim: boolp(true), ActorID: &m.ID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	done, err := db.Complete(inst.ID, m.ID, "洗完了")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if done.State != "done" || done.CompletedBy == nil || *done.CompletedBy != m.ID || done.CompletedAt == "" {
		t.Fatalf("完成字段没写全: %+v", done)
	}
	back, err := db.Uncomplete(inst.ID, m.ID, false)
	if err != nil {
		t.Fatalf("uncomplete: %v", err)
	}
	if back.State != "open" || back.CompletedBy != nil || back.CompletedAt != "" || back.ClaimedBy != nil {
		t.Fatalf("撤销没清干净: %+v", back)
	}
}

// 非本人不能撤销/放弃。
func TestReleaseAndUncompletePermissions(t *testing.T) {
	db := newTestDB(t)
	a := mustMember(t, db, "小明")
	b := mustMember(t, db, "小王")
	inst, err := db.CreateInstance(InstanceInput{Title: strp("拖地"), DueDate: strp(db.Today()), RequiresClaim: boolp(true), ActorID: &a.ID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Claim(inst.ID, a.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := db.Release(inst.ID, b.ID, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("别人放弃应 FORBIDDEN，实际 %v", err)
	}
	if _, err := db.Release(inst.ID, a.ID, false); err != nil {
		t.Fatalf("本人放弃应成功，实际 %v", err)
	}
	if _, err := db.Complete(inst.ID, a.ID, ""); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := db.Uncomplete(inst.ID, b.ID, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("别人撤销应 FORBIDDEN，实际 %v", err)
	}
	if _, err := db.Uncomplete(inst.ID, b.ID, true); err != nil {
		t.Fatalf("管理员撤销应成功，实际 %v", err)
	}
}

// 数据落盘后重新打开，状态不丢。
func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chores.db")
	loc, _ := time.LoadLocation("Asia/Shanghai")

	db, err := New(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	db.SetLocation(loc)
	m := mustMember(t, db, "妈妈")
	inst, err := db.CreateInstance(InstanceInput{Title: strp("清猫砂"), DueDate: strp(db.Today()), RequiresClaim: boolp(true), ActorID: &m.ID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Claim(inst.ID, m.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}

	again, err := New(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := again.InstanceByID(inst.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.State != "claimed" || got.ClaimedBy == nil || *got.ClaimedBy != m.ID {
		t.Fatalf("重开后状态丢失: %+v", got)
	}
	if len(again.ListMembers()) != 1 {
		t.Fatalf("重开后成员数不对: %d", len(again.ListMembers()))
	}
}
