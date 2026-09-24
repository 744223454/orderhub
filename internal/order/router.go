package order

import "github.com/gin-gonic/gin"

// UserRoutes 注册登录用户可访问的订单接口到给定路由分组（需已通过 JWT 认证中间件）。
func UserRoutes(rg *gin.RouterGroup, h *Handler) {
	rg.POST("/orders", h.CreateOrder)
	rg.GET("/orders", h.ListMyOrders)
	rg.POST("/orders/:id/pay", h.PayOrder)
}

// StaffRoutes 注册运营/管理员专属的订单管理接口到给定路由分组
// （需已通过 RequireRole 中间件，建议挂在 JWT 认证分组之下）。
func StaffRoutes(rg *gin.RouterGroup, h *Handler) {
	rg.GET("/admin/orders", h.ListAllOrders)
	rg.POST("/admin/orders/:id/ship", h.ShipOrder)
	rg.POST("/admin/orders/:id/complete", h.CompleteOrder)
	rg.POST("/admin/orders/:id/refund", h.RefundOrder)
}
