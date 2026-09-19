package store

// Reset 把数据重置为"刚安装"状态：成员、分组、任务定义、实例、活动日志全部清空。
//
// 设计取舍：
//   - 不删除数据文件本身（Windows 上删除被占用的文件容易失败），而是在内存里
//     重建结构后走同一条原子落盘路径；
//   - 库本来就是空的也返回 nil（幂等），前端按钮不用区分"第一次点"和"再点一次"；
//   - settings 与 next_id 一起重置，保证 reset 之后 id 从头开始，行为等同全新安装。
func (d *DB) Reset() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.file = dbFile{
		Version:   formatVersion,
		NextID:    1,
		Members:   []Member{},
		Groups:    []Group{},
		Chores:    []Chore{},
		Instances: []Instance{},
		Activity:  []Activity{},
		Settings:  map[string]string{},
	}
	return d.saveLocked()
}
