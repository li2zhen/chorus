package httpapi

import "net/http"

// Register 把契约里的全部 /api 路由挂到 mux 上。
//
// 为什么不用子 mux + StripPrefix：把 /api/ 前缀挂到子 mux 上会把路径裁成 /，
// 那样未实现的 /api 路径会落到子 mux 的兜底（Go 默认纯文本 404），
// 前端就拿不到契约要求的 JSON 错误体。这里直接把"方法 + 完整路径"注册到主 mux，
// 最后补一个 /api/ 兜底，保证任何未实现路径都是 JSON 404。
func (a *API) Register(mux *http.ServeMux) {
	routes := map[string]http.HandlerFunc{
		// 身份
		"POST /api/auth/login":  a.postLogin,
		"POST /api/auth/logout": a.postLogout,
		"GET /api/auth/session": a.getSession,
		// 视图
		"GET /api/bootstrap": a.getBootstrap,
		"GET /api/instances": a.getInstances,
		"GET /api/chores":    a.getChores,
		"GET /api/activity":  a.getActivity,
		// 动作
		"POST /api/instances":                 a.postInstance,
		"POST /api/chores":                    a.postChore,
		"PATCH /api/chores/{id}":              a.patchChore,
		"DELETE /api/chores/{id}":             a.deleteChore,
		"POST /api/instances/{id}/claim":      a.postClaim,
		"POST /api/instances/{id}/release":    a.postRelease,
		"POST /api/instances/{id}/complete":   a.postComplete,
		"POST /api/instances/{id}/uncomplete": a.postUncomplete,
		"POST /api/instances/{id}/reschedule": a.postReschedule,
		// 管理面
		"POST /api/admin/login":  a.postAdminLogin,
		"POST /api/admin/logout": a.postAdminLogout,
		"GET /api/admin/session": a.getAdminSession,
		// v2：分组与成员不再需要管理员（任意已登录成员即可）
		"GET /api/groups":          a.getGroups,
		"GET /api/members":         a.getMembers,
		"DELETE /api/members/{id}": a.deleteAdminMember,
		"POST /api/groups":         a.postGroup,
		"PATCH /api/groups/{id}":   a.patchGroup,
		"DELETE /api/groups/{id}":  a.deleteGroup,
		"POST /api/members":        a.postMember,
		"PATCH /api/members/{id}":  a.patchMember,
		// v2：头像
		"PUT /api/members/{id}/avatar":    a.putMemberAvatar,
		"DELETE /api/members/{id}/avatar": a.deleteMemberAvatar,
		"GET /api/avatars/{id}":           a.getAvatar,
		// 老路径保留可用（向后兼容，内部同一实现）
		"POST /api/admin/members":        a.postAdminMember,
		"PATCH /api/admin/members/{id}":  a.patchAdminMember,
		"DELETE /api/admin/members/{id}": a.deleteAdminMember,
		"POST /api/admin/groups":         a.postAdminGroup,
		"PATCH /api/admin/groups/{id}":   a.patchAdminGroup,
		"DELETE /api/admin/groups/{id}":  a.deleteAdminGroup,
		"GET /api/admin/groups":          a.getAdminGroupsCompat,
		"GET /api/admin/members":         a.getAdminMembersCompat,
		"GET /api/admin/export":          a.getAdminExport,
		"POST /api/admin/seed":           a.postAdminSeed,
		"POST /api/admin/reset":          a.postAdminReset,
		// v4：管理页打的就是这两条（此前缺失 → 404 提示"接口不存在"）；与成员入口同一实现。
		"PUT /api/admin/members/{id}/avatar":    a.putAdminMemberAvatar,
		"DELETE /api/admin/members/{id}/avatar": a.deleteAdminMemberAvatar,
	}
	for pattern, handler := range routes {
		mux.Handle(pattern, withCORS(http.HandlerFunc(handler)))
	}
	mux.Handle("/api/", withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "接口不存在")
	})))
}
