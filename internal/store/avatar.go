package store

import (
	"fmt"
	"time"
)

// v2：头像。
//
// 存进数据文件（base64），不落磁盘文件——家庭用量足够，也让"备份=一个文件"这条
// 性质继续成立。白名单与外限在 HTTP 层先挡一次，这里再挡一次，保证任何调用方
// （包括 seed、测试）都不会写进非法数据。
const (
	// MaxAvatarBytes 是解码后的上限（契约：256 KB）。
	MaxAvatarBytes = 256 * 1024
)

// AllowedAvatarType 判断 content_type 是否在白名单里。
func AllowedAvatarType(contentType string) bool {
	return contentType == "image/png" || contentType == "image/jpeg"
}

// SetAvatar 写入/覆盖成员头像。
func (d *DB) SetAvatar(memberID int64, contentType string, data []byte) (Member, error) {
	if !AllowedAvatarType(contentType) {
		return Member{}, fmt.Errorf("%w: 头像只支持 image/png 或 image/jpeg", ErrUnsupportedMedia)
	}
	if len(data) == 0 {
		return Member{}, fmt.Errorf("%w: 头像内容为空", ErrBadInput)
	}
	if len(data) > MaxAvatarBytes {
		return Member{}, fmt.Errorf("%w: 头像不得超过 256 KB", ErrTooLarge)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	idx := -1
	for i := range d.file.Members {
		if d.file.Members[i].ID == memberID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Member{}, fmt.Errorf("%w: member %d", ErrNotFound, memberID)
	}
	m := &d.file.Members[idx]
	if m.Archived {
		return Member{}, fmt.Errorf("%w: member %d", ErrNotFound, memberID)
	}
	m.AvatarData = append([]byte(nil), data...)
	m.AvatarContentType = contentType
	m.AvatarUpdatedAt = FormatRFC3339(time.Now())
	if err := d.saveLocked(); err != nil {
		return Member{}, err
	}
	return *m, nil
}

// ClearAvatar 清空头像（幂等）。
func (d *DB) ClearAvatar(memberID int64) (Member, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	idx := -1
	for i := range d.file.Members {
		if d.file.Members[i].ID == memberID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Member{}, fmt.Errorf("%w: member %d", ErrNotFound, memberID)
	}
	m := &d.file.Members[idx]
	m.AvatarData = nil
	m.AvatarContentType = ""
	m.AvatarUpdatedAt = ""
	if err := d.saveLocked(); err != nil {
		return Member{}, err
	}
	return *m, nil
}

// AvatarData 读取头像字节与类型；没有头像返回 ok=false。
func (d *DB) AvatarData(memberID int64) (data []byte, contentType string, ok bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for i := range d.file.Members {
		m := d.file.Members[i]
		if m.ID != memberID {
			continue
		}
		if len(m.AvatarData) == 0 {
			return nil, "", false
		}
		ct := m.AvatarContentType
		if ct == "" {
			ct = "image/png"
		}
		return append([]byte(nil), m.AvatarData...), ct, true
	}
	return nil, "", false
}

// AvatarURL 生成成员视图里的 avatar_url；没有头像返回空串。
func (d *DB) AvatarURL(memberID int64) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for i := range d.file.Members {
		m := d.file.Members[i]
		if m.ID != memberID || len(m.AvatarData) == 0 {
			continue
		}
		url := fmt.Sprintf("/api/avatars/%d", m.ID)
		if m.AvatarUpdatedAt != "" {
			url += "?v=" + m.AvatarUpdatedAt
		}
		return url
	}
	return ""
}
