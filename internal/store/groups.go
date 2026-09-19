package store

import (
	"fmt"
	"sort"
	"strings"
)

// ListGroups 返回全部分组，按 sort, id 排序。
func (d *DB) ListGroups() []Group {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := append([]Group(nil), d.file.Groups...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (d *DB) groupLocked(id int64) (Group, bool) {
	for _, g := range d.file.Groups {
		if g.ID == id {
			return g, true
		}
	}
	return Group{}, false
}

// CreateGroup 新增分组；memberIDs 为空表示全员。
func (d *DB) CreateGroup(name string, memberIDs []int64) (Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Group{}, fmt.Errorf("%w: name 不能为空", ErrBadInput)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.checkMembersLocked(memberIDs); err != nil {
		return Group{}, err
	}
	sortOrder := 0
	for _, g := range d.file.Groups {
		if g.Sort >= sortOrder {
			sortOrder = g.Sort + 1
		}
	}
	g := Group{ID: d.nextIDLocked(), Name: name, Sort: sortOrder, MemberIDs: normalizeIDs(memberIDs), CreatedAt: now()}
	d.file.Groups = append(d.file.Groups, g)
	if err := d.saveLocked(); err != nil {
		return Group{}, err
	}
	return g, nil
}

// UpdateGroup 局部更新；name/memberIDs/sort 为 nil 时不动。
func (d *DB) UpdateGroup(id int64, name *string, memberIDs *[]int64, sortOrder *int) (Group, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	idx := -1
	for i := range d.file.Groups {
		if d.file.Groups[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Group{}, fmt.Errorf("%w: group %d", ErrNotFound, id)
	}
	g := &d.file.Groups[idx]
	if name != nil {
		v := strings.TrimSpace(*name)
		if v == "" {
			return Group{}, fmt.Errorf("%w: name 不能为空", ErrBadInput)
		}
		g.Name = v
	}
	if memberIDs != nil {
		if err := d.checkMembersLocked(*memberIDs); err != nil {
			return Group{}, err
		}
		g.MemberIDs = normalizeIDs(*memberIDs)
	}
	if sortOrder != nil {
		g.Sort = *sortOrder
	}
	if err := d.saveLocked(); err != nil {
		return Group{}, err
	}
	return *g, nil
}

// DeleteGroup 删除分组；引用它的任务与实例把 group_id 置空（不删任务）。
func (d *DB) DeleteGroup(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	idx := -1
	for i := range d.file.Groups {
		if d.file.Groups[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("%w: group %d", ErrNotFound, id)
	}
	name := d.file.Groups[idx].Name
	d.file.Groups = append(d.file.Groups[:idx], d.file.Groups[idx+1:]...)
	for i := range d.file.Chores {
		if d.file.Chores[i].GroupID != nil && *d.file.Chores[i].GroupID == id {
			d.file.Chores[i].GroupID = nil
		}
	}
	for i := range d.file.Instances {
		if d.file.Instances[i].GroupID != nil && *d.file.Instances[i].GroupID == id {
			d.file.Instances[i].GroupID = nil
		}
	}
	d.logLocked(nil, nil, "group.delete", name)
	return d.saveLocked()
}

// checkMembersLocked 校验成员存在且未归档。
func (d *DB) checkMembersLocked(ids []int64) error {
	for _, id := range ids {
		m, ok := d.memberLocked(id)
		if !ok || m.Archived {
			return fmt.Errorf("%w: member %d", ErrBadInput, id)
		}
	}
	return nil
}

// normalizeIDs 去重、丢零值、保持升序。
func normalizeIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return []int64{}
	}
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
