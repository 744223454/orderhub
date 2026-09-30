package wecom

import "github.com/gin-gonic/gin"

// PublicRoutes 注册公开的企业微信接口到给定路由分组（无需登录态）。
//
// 授权链接与回调换票都必须匿名可访问：前者发生在登录之前，
// 后者是浏览器顶层跳转过来的，带不了 Authorization 头。
func PublicRoutes(rg *gin.RouterGroup, h *Handler) {
	rg.GET("/auth/wecom/authorize", h.Authorize)
	rg.POST("/auth/wecom/login", h.Login)
}

// AuthedRoutes 注册需要登录态的企业微信接口到给定路由分组
// （需已通过 auth.Auth 中间件）。
func AuthedRoutes(rg *gin.RouterGroup, h *Handler) {
	rg.POST("/auth/wecom/bind", h.Bind)
}
