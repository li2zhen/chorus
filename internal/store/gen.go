package store

import (
	"log"
	"time"
)

// 生成器规则（api/CONTRACT.md）：
//   - 启动与每 10 分钟补齐：从每个定义的 start_date 到"未来 horizonDays 天"
//   - (chore_id, due_date) 幂等：已存在同定义的同日实例就跳过
//     注意：手工发布的实例 chore_id 为空，不参与幂等键
//   - 非循环定义（none）只在 start_date 生成那一天
const (
	horizonDays = 90
	// catchUpDays 是补历史的硬上限，防止 start_date 写得很早时瞬间铺满。
	catchUpDays = 366
	// maxInstances 是防御性上限。
	maxInstances = 20000
)

// Generate 立即执行一次补齐（启动、定时器、以及对外的一个手动触发点）。
func (d *DB) Generate() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.generateLocked(time.Now()); err != nil {
		return err
	}
	return d.saveLocked()
}

// RunGenerator 每 interval 跑一次补齐，直到 stop 关闭。
func (d *DB) RunGenerator(interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if err := d.Generate(); err != nil {
				log.Printf("chores: generator: %v", err)
			}
		}
	}
}

// generateLocked 补齐所有定义的实例；必须在写锁内调用。
func (d *DB) generateLocked(nowT time.Time) error {
	loc := d.loc
	if loc == nil {
		loc = time.Local
	}
	today := time.Date(nowT.In(loc).Year(), nowT.In(loc).Month(), nowT.In(loc).Day(), 0, 0, 0, 0, loc)
	existing := make(map[string]bool, len(d.file.Instances))
	for _, inst := range d.file.Instances {
		if inst.ChoreID != nil {
			// v5：已删除（archived）的实例也算"这天已经有过了"，
			// 否则删掉某天的循环实例会在下一轮重新冒出来。
			existing[key(*inst.ChoreID, inst.DueDate)] = true
		}
	}
	added := false
	for i := range d.file.Chores {
		c := &d.file.Chores[i]
		if c.Archived {
			continue
		}
		start, err := time.ParseInLocation(dateLayout, c.StartDate, loc)
		if err != nil {
			continue
		}
		// 补齐起点：不早于 start_date，但至少回到本月 1 日，
		// 这样前端「本月」日历的前半截不会是空的（合同补充要求）。
		from := start
		monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, loc)
		if from.After(monthStart) {
			from = monthStart
		}
		// 再往前最多补 catchUpDays
		if earliest := today.AddDate(0, 0, -catchUpDays); from.Before(earliest) {
			from = earliest
		}
		to := today.AddDate(0, 0, horizonDays)
		if c.Recurrence == "none" {
			// 一次性任务：只在 start_date 生成
			if err := d.materialize(c, start, existing, &added); err != nil {
				return err
			}
			continue
		}
		for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
			if len(d.file.Instances) >= maxInstances {
				return nil
			}
			if !matchesRecurrence(c, day) {
				continue
			}
			if err := d.materialize(c, day, existing, &added); err != nil {
				return err
			}
		}
	}
	_ = added
	return nil
}

// materialize 生成一天实例（幂等）。
func (d *DB) materialize(c *Chore, day time.Time, existing map[string]bool, added *bool) error {
	due := formatDate(day)
	k := key(c.ID, due)
	if existing[k] {
		return nil
	}
	startAt, endAt, dur, err := d.templateTimesLocked(c, due)
	if err != nil {
		return err
	}
	inst := Instance{
		ID:              d.nextIDLocked(),
		ChoreID:         &c.ID,
		Title:           c.Title,
		Note:            c.Note,
		GroupID:         c.GroupID,
		DueDate:         due,
		State:           "open",
		RequiresClaim:   c.RequiresClaim,
		RequiresPhoto:   c.RequiresPhoto,
		CreatedAt:       now(),
		StartAt:         startAt,
		EndAt:           endAt,
		DurationMinutes: dur,
	}
	if c.MemberID != nil {
		who := *c.MemberID
		inst.ClaimedBy = &who
		inst.ClaimedAt = now()
		inst.State = "claimed"
	}
	d.file.Instances = append(d.file.Instances, inst)
	existing[k] = true
	*added = true
	return nil
}

// matchesRecurrence 判断某一天是否该出现这条定义。
func matchesRecurrence(c *Chore, day time.Time) bool {
	switch c.Recurrence {
	case "daily":
		return true
	case "weekly":
		if c.Weekday == nil {
			return false
		}
		return int(day.Weekday()) == *c.Weekday
	case "monthly":
		if c.DayOfMonth == nil {
			return false
		}
		want := *c.DayOfMonth
		if last := daysInMonth(day.Year(), day.Month()); want > last {
			// 该月无此日：落到月末
			want = last
		}
		return day.Day() == want
	default:
		return false
	}
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func key(choreID int64, due string) string {
	return itoa(choreID) + "|" + due
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
