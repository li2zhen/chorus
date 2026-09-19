// Package config 是**唯一**读环境变量的地方。
//
// 命名演进：项目从 chores 改名为 chorus，所以每个变量都按
// CHORUS_<名> → CHORES_<名> → 默认值 的顺序解析：
// 新文档写 CHORUS_*，老的 compose/脚本用 CHORES_* 也不会坏。
package config

import (
	"os"
	"strings"
)

// 变量名后缀（两个前缀共用）。
const (
	nameAddr       = "ADDR"
	nameDB         = "DB"
	nameAdminToken = "ADMIN_TOKEN"
)

// Env 是进程启动时解析好的全部配置。
type Env struct {
	// Addr 是监听地址，CHORUS_ADDR / CHORES_ADDR，默认 :2022。
	Addr string
	// DBPath 是数据文件路径，CHORUS_DB / CHORES_DB，默认 /data/chorus.db。
	DBPath string
	// AdminToken 是 /admin 的口令：CHORUS_ADMIN_TOKEN → CHORES_ADMIN_TOKEN → admin。
	AdminToken string
	// TZ 是时区名（沿用 TZ，不改名），默认 Asia/Shanghai。
	TZ string
}

// Load 读一次环境变量并返回解析结果。
func Load() Env {
	return Env{
		Addr:       lookup(nameAddr, ":2022"),
		DBPath:     lookup(nameDB, "/data/chorus.db"),
		AdminToken: lookup(nameAdminToken, "admin"),
		TZ:         one("TZ", "Asia/Shanghai"),
	}
}

// lookup 按 CHORUS_ → CHORES_ → 默认值 的顺序取一个变量。
func lookup(suffix, fallback string) string {
	if v := one("CHORUS_"+suffix, ""); v != "" {
		return v
	}
	if v := one("CHORES_"+suffix, ""); v != "" {
		return v
	}
	return fallback
}

// one 读单个键并去掉空白。
func one(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
