package user

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// TokenIssuer 为已认证用户签发访问令牌。
// 由外部（auth 包）注入实现，以保证 user 模块不反向依赖 auth 模块。
type TokenIssuer func(u *User) (string, error)

// Handler 承载用户模块的 HTTP 接口。
type Handler struct {
	service *Service
	issue   TokenIssuer
}

// NewHandler 创建用户接口处理器，issue 用于登录成功后签发访问令牌。
func NewHandler(service *Service, issue TokenIssuer) *Handler {
	return &Handler{
		service: service,
		issue:   issue,
	}
}

type registerRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Role     string `json:"role"`
}

// Register 用户注册
// @Summary 用户注册
// @Tags 用户
// @Accept json
// @Produce json
// @Param request body registerRequest true "注册信息"
// @Success 200 {object} User
// @Failure 400 {object} map[string]string
// @Router /register [post]
func (h *Handler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 公开注册接口不允许自选角色，只能注册为普通用户，
	// 否则任意访问者都能把自己直接提权为运营或管理员。
	if req.Role != "" && Role(req.Role) != RoleUser {
		c.JSON(http.StatusBadRequest, gin.H{"error": "注册接口不允许指定角色"})
		return
	}

	created, err := h.service.Register(c.Request.Context(), req.Username, req.Password, RoleUser)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, created)
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// loginResponse 是登录成功后返回的载荷。
type loginResponse struct {
	// Token Bearer 访问令牌，后续请求需放入 Authorization 头。
	Token string `json:"token"`
	// User 登录成功的用户信息（不含密码哈希）。
	User *User `json:"user"`
}

// Login 用户登录
// @Summary 用户登录
// @Tags 用户
// @Accept json
// @Produce json
// @Param request body loginRequest true "登录信息"
// @Success 200 {object} loginResponse
// @Failure 401 {object} map[string]string
// @Router /login [post]
func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	u, err := h.service.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	token, err := h.issue(u)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发令牌失败"})
		return
	}

	c.JSON(http.StatusOK, loginResponse{Token: token, User: u})
}

// Me 获取当前登录用户
// @Summary 获取当前登录用户
// @Tags 用户
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /me [get]
func (h *Handler) Me(c *gin.Context) {
	userID, ok := CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	role, _ := CurrentRole(c)
	c.JSON(http.StatusOK, gin.H{
		"user_id": userID,
		"role":    role,
	})
}
