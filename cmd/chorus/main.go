// Command chores 是家庭家务认领板的单一二进制：
// 内嵌前端 + 单文件 JSON 数据层 + REST API，同一个进程同时服务 UI 与接口。
package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"chores/internal/config"
	"chores/internal/httpapi"
	"chores/internal/store"
	"chores/web"
)

// version 由构建时 -ldflags 注入。
var version = "dev"

func main() {
	// 环境变量只在这里读一次（见 internal/config）。
	cfg := config.Load()
	addr, dbPath, adminToken, tzName := cfg.Addr, cfg.DBPath, cfg.AdminToken, cfg.TZ

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		log.Printf("chores: 未知时区 %q，退回 UTC", tzName)
		loc = time.UTC
	}

	db, err := store.New(dbPath)
	if err != nil {
		log.Fatalf("chores: 打开数据文件失败: %v", err)
	}
	db.SetLocation(loc)

	// 启动时补齐循环实例，之后每 10 分钟一次（契约要求）。
	if err := db.Generate(); err != nil {
		log.Printf("chores: 启动补齐失败: %v", err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go db.RunGenerator(10*time.Minute, stop)

	api := httpapi.New(httpapi.Deps{Store: db, AdminToken: adminToken, Version: version}, loc)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": version})
	})
	api.Register(mux)
	if err := mountWeb(mux, web.FS()); err != nil {
		log.Fatalf("chores: mount web: %v", err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if adminToken == "admin" {
		log.Printf("chores: 提示：CHORES_ADMIN_TOKEN 仍是默认值 admin，上线前请改")
	}
	log.Printf("chores %s listening on %s (db=%s tz=%s)", version, addr, dbPath, loc)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("chores: %v", err)
	}
}

// mountWeb 把内嵌静态资源挂到 /assets/，其余非 /api、/health 路径一律回落 index.html（SPA）。
// /api/ 已经挂在更具体的模式上，这里只处理页面与静态文件。
func mountWeb(mux *http.ServeMux, root fs.FS) error {
	mux.Handle("/assets/", http.FileServer(http.FS(root)))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			// 走到这里说明是未实现的 /api 路径（具体路由已在上层匹配）。
			writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "接口不存在")
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(root, name); err != nil {
			name = "index.html"
		}
		http.ServeFileFS(w, r, root, name)
	})
	return nil
}

// writeJSONError 与 internal/httpapi 的错误体保持一致。
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
