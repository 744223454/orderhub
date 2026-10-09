package user

import "github.com/gin-gonic/gin"

// AuthRoutes 注册公开的认证接口（注册 / 登录）到给定路由分组。
func AuthRoutes(rg *gin.RouterGroup, h *Handler) {
	rg.POST("/register", h.Register)
	rg.POST("/login", h.Login)
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
