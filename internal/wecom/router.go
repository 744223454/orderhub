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

// AdminRoutes 注册仅管理员可访问的企业通讯录接口到给定路由分组
// （需已通过 auth.RequireRole(user.RoleAdmin) 中间件）。
//
// 挂管理员而不是运营：通讯录是**全公司**的人员信息，比订单数据敏感；
// 运营的职责是处理订单，没有读整个通讯录的理由。
//
// 与登录接口有两种不同的注册策略，这是有意的：
// 登录接口在未配置企微时**不注册**（能力不存在，404 合理）；
// 本组接口**始终注册**，未配置时由处理器返回 503 —— 前端的组织架构页是常驻的，
// 收到 503 + 一句「未配置企业微信」，比收到 gin 的 404 纯文本（前端只能显示「请求失败」）
// 更容易看懂。
func AdminRoutes(rg *gin.RouterGroup, h *Handler) {
	rg.GET("/admin/org/directory", h.Directory)
}
