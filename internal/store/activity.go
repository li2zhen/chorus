package store

// logLocked 追加一条活动日志；必须在持有写锁时调用。
// actorID / instanceID 可为 nil（系统动作）。
func (d *DB) logLocked(actorID, instanceID *int64, action, detail string) {
	a := Activity{
		ID:         d.nextIDLocked(),
		At:         now(),
		ActorID:    actorID,
		InstanceID: instanceID,
		Action:     action,
		Detail:     detail,
	}
	d.file.Activity = append(d.file.Activity, a)
	// 只保留最近 2000 条，避免文件无限膨胀（历史视图靠实例本身，不靠日志）。
	const keep = 2000
	if len(d.file.Activity) > keep {
		d.file.Activity = append([]Activity(nil), d.file.Activity[len(d.file.Activity)-keep:]...)
	}
}

// ListActivity 返回最近的活动（倒序），limit<=0 表示默认 100。
func (d *DB) ListActivity(instanceID int64, limit int) []Activity {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	}
	out := make([]Activity, 0, limit)
	for i := len(d.file.Activity) - 1; i >= 0 && len(out) < limit; i-- {
		a := d.file.Activity[i]
		if instanceID > 0 && (a.InstanceID == nil || *a.InstanceID != instanceID) {
			continue
		}
		out = append(out, a)
	}
	return out
}
