package user

import "github.com/gin-gonic/gin"

// AuthRoutes 注册公开的认证接口（注册 / 登录）到给定路由分组。
func AuthRoutes(rg *gin.RouterGroup, h *Handler) {
	rg.POST("/register", h.Register)
	rg.POST("/login", h.Login)
}
