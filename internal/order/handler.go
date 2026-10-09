package order

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/744223454/orderhub/internal/user"
	"github.com/gin-gonic/gin"
)

// Handler 承载订单模块的 HTTP 接口。
type Handler struct {
	service *Service
}

// NewHandler 创建订单接口处理器。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// respondOrderError 把服务层错误映射为 HTTP 响应。
//
// 业务错误按其语义返回对应状态码；未识别的错误视为技术故障，
// 返回 500 通用文案并追加到 gin 的错误链（由日志中间件记录），
// 避免把 SQL 报错等内部细节暴露给调用方。
func respondOrderError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrOrderNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, ErrInvalidTransition), errors.Is(err, ErrStatusConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, ErrInvalidProductName), errors.Is(err, ErrInvalidAmount):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器内部错误"})
	}
}

// parseOrderID 解析路径中的订单 ID。
// 解析失败或不合法时已写入 400 响应并返回 false，调用方应直接 return。
func parseOrderID(c *gin.Context) (uint, bool) {
	// 用 ParseUint 而非 Atoi：前者直接拒绝负数与非法字符，
	// 避免 int 转 uint 时负数变成巨大正整数这类隐蔽问题。
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "订单 ID 不合法"})
		return 0, false
	}
	return uint(id), true
}

// emptyIfNil 把 nil 切片换成空切片，序列化后是 [] 而不是 null。
//
// gorm 泛型 Find 的空结果目前已是非 nil 空切片（v1.31.2 实测），
// 这里仍显式兜底：把「items 恒为 []」的响应契约固定在序列化边界，
// 不依赖 ORM 的实现细节——前端拿到 null 调 .map() 会直接抛错。
func emptyIfNil(orders []Order) []Order {
	if orders == nil {
		return []Order{}
	}
	return orders
}

type createOrderRequest struct {
	// ProductName 商品名称。
	ProductName string `json:"product_name" binding:"required"`
	// Amount 订单金额，单位为分。
	Amount int64 `json:"amount" binding:"required"`
}

// CreateOrder 创建订单
// @Summary 创建订单（登录用户）
// @Tags 订单
// @Accept json
// @Produce json
// @Param request body createOrderRequest true "下单信息"
// @Success 200 {object} Order
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /orders [post]
func (h *Handler) CreateOrder(c *gin.Context) {
	var req createOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 不透传 gin 的绑定错误（英文内部字段名），对调用方无意义。
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	userID, ok := user.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	o, err := h.service.CreateOrder(c.Request.Context(), userID, req.ProductName, req.Amount)
	if err != nil {
		respondOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, o)
}

// listOrdersResponse 是订单列表的返回载荷。
//
// 形状与 user 包的 listUsersResponse 保持一致（items/total/page/page_size）：
// 前端要靠 total 算总页数，两个列表页也复用同一套分页组件。
type listOrdersResponse struct {
	// Items 当前页的订单。
	Items []Order `json:"items"`
	// Total 满足条件的订单总数（不受分页限制）。
	Total int64 `json:"total"`
	// Page 生效后的页码，从 1 开始。
	Page int `json:"page"`
	// PageSize 生效后的每页条数。
	PageSize int `json:"page_size"`
}

// ListMyOrders 我的订单列表
// @Summary 查询自己的订单列表（分页）
// @Tags 订单
// @Produce json
// @Param page query int false "页码，从 1 开始（默认 1）"
// @Param page_size query int false "每页条数（默认 20，上限 100）"
// @Success 200 {object} listOrdersResponse
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /orders [get]
func (h *Handler) ListMyOrders(c *gin.Context) {
	userID, ok := user.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 解析失败一律交给服务层回落到默认值，不在这里报错。
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	result, err := h.service.ListUserOrders(c.Request.Context(), userID, page, pageSize)
	if err != nil {
		respondOrderError(c, err)
		return
	}

	c.JSON(http.StatusOK, listOrdersResponse{
		Items:    emptyIfNil(result.Orders),
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
	})
}

// PayOrder 支付订单
// @Summary 支付自己的订单（pending → paid）
// @Tags 订单
// @Produce json
// @Param id path int true "订单 ID"
// @Success 200 {object} Order
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security BearerAuth
// @Router /orders/{id}/pay [post]
func (h *Handler) PayOrder(c *gin.Context) {
	orderID, ok := parseOrderID(c)
	if !ok {
		return
	}

	userID, ok := user.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	o, err := h.service.PayOrder(c.Request.Context(), userID, orderID)
	if err != nil {
		respondOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, o)
}

// ListAllOrders 全部订单列表（管理端）
// @Summary 查询全部订单（仅运营/管理员，分页）
// @Tags 订单管理
// @Produce json
// @Param page query int false "页码，从 1 开始（默认 1）"
// @Param page_size query int false "每页条数（默认 20，上限 100）"
// @Success 200 {object} listOrdersResponse
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /admin/orders [get]
func (h *Handler) ListAllOrders(c *gin.Context) {
	// 解析失败一律交给服务层回落到默认值，不在这里报错。
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	result, err := h.service.ListAllOrders(c.Request.Context(), page, pageSize)
	if err != nil {
		respondOrderError(c, err)
		return
	}

	c.JSON(http.StatusOK, listOrdersResponse{
		Items:    emptyIfNil(result.Orders),
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
	})
}

// ShipOrder 发货
// @Summary 发货（paid → shipped，仅运营/管理员）
// @Tags 订单管理
// @Produce json
// @Param id path int true "订单 ID"
// @Success 200 {object} Order
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security BearerAuth
// @Router /admin/orders/{id}/ship [post]
func (h *Handler) ShipOrder(c *gin.Context) {
	orderID, ok := parseOrderID(c)
	if !ok {
		return
	}

	o, err := h.service.ShipOrder(c.Request.Context(), orderID)
	if err != nil {
		respondOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, o)
}

// CompleteOrder 完成订单
// @Summary 完成订单（shipped → completed，仅运营/管理员）
// @Tags 订单管理
// @Produce json
// @Param id path int true "订单 ID"
// @Success 200 {object} Order
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security BearerAuth
// @Router /admin/orders/{id}/complete [post]
func (h *Handler) CompleteOrder(c *gin.Context) {
	orderID, ok := parseOrderID(c)
	if !ok {
		return
	}

	o, err := h.service.CompleteOrder(c.Request.Context(), orderID)
	if err != nil {
		respondOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, o)
}

// RefundOrder 退款
// @Summary 退款（paid/shipped → refunded，仅运营/管理员）
// @Tags 订单管理
// @Produce json
// @Param id path int true "订单 ID"
// @Success 200 {object} Order
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security BearerAuth
// @Router /admin/orders/{id}/refund [post]
func (h *Handler) RefundOrder(c *gin.Context) {
	orderID, ok := parseOrderID(c)
	if !ok {
		return
	}

	o, err := h.service.RefundOrder(c.Request.Context(), orderID)
	if err != nil {
		respondOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, o)
}
