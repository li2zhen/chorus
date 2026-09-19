package store

import "time"

// Today 返回当前时区下的今天（YYYY-MM-DD），供 seed 与调用方使用。
func (d *DB) Today() string {
	loc := d.location()
	return time.Now().In(loc).Format(dateLayout)
}

// MonthStart 返回当前时区下本月 1 日（YYYY-MM-DD）。
// 循环生成器的补齐起点与 seed 的 start_date 都用它，保证「本月」视图前半截有数据。
func (d *DB) MonthStart() string {
	loc := d.location()
	now := time.Now().In(loc)
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).Format(dateLayout)
}
