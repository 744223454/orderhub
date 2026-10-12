package user

import "github.com/gin-gonic/gin"

// AuthRoutes 注册公开的认证接口（注册 / 登录 / 刷新 / 登出）到给定路由分组。
//
// refresh 与 logout 刻意挂在公开层（不经过 auth.Auth）：它们的凭据是刷新令牌本身，
// 而不是访问令牌——访问令牌恰好过期时也必须能刷新、能登出，
// 否则「过期即锁死」会在客户端造出一堆只能清本地缓存的死角。
func AuthRoutes(rg *gin.RouterGroup, h *Handler) {
	rg.POST("/register", h.Register)
	rg.POST("/login", h.Login)
	rg.POST("/refresh", h.Refresh)
	rg.POST("/logout", h.Logout)
}

// MeRoutes 注册当前登录用户的自助接口（查询本人信息 / 修改密码）到给定路由分组
// （需已通过 auth.Auth 中间件）。
func MeRoutes(rg *gin.RouterGroup, h *Handler) {
	rg.GET("/me", h.Me)
	rg.PATCH("/me/password", h.ChangePassword)
}

// AdminRoutes 注册仅管理员可访问的用户管理接口到给定路由分组
// （需已通过 auth.RequireRole(user.RoleAdmin) 中间件）。
//
// 刻意不挂到运营也能进的 staff 组上：角色管理本身就是权限的边界，
// 让运营能改角色等于把提权能力交出去。
func AdminRoutes(rg *gin.RouterGroup, h *Handler) {
	rg.POST("/admin/users", h.AdminCreateUser)
	rg.GET("/admin/users", h.ListUsers)
	rg.PATCH("/admin/users/:id/role", h.UpdateUserRole)
	rg.DELETE("/admin/users/:id/identities/:provider", h.UnbindIdentity)
}
