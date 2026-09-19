package httpapi

import (
	"net/http"
	"os"
	"path/filepath"

	"chores/internal/seed"
)

// getAdminExport 直接把数据文件回给浏览器（Content-Disposition attachment）。
func (a *API) getAdminExport(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	path := a.deps.Store.Path()
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "读不到数据文件")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "读不到数据文件")
		return
	}
	name := filepath.Base(path)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// postAdminSeed 写入演示数据；仅当库为空时执行（幂等）。
func (a *API) postAdminSeed(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	result, err := seed.Apply(a.deps.Store)
	if err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

// postAdminReset 把数据重置为"刚安装"状态（发布前的"清空数据"按钮打它）。
//
// 与 seed 是配套的一对：seed 写入演示数据 → 看完 reset 清掉。
// 幂等：库本来就是空的也返回 200，前端不用区分首次与再次点击。
func (a *API) postAdminReset(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if err := a.deps.Store.Reset(); err != nil {
		writeDomainError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": map[string]any{"reset": true}})
}
