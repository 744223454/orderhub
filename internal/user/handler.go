package user

import (
	"errors"
	"net/http"
	"strconv"
	"time"

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

// respondUserError 把服务层错误映射为 HTTP 响应。
//
// 业务错误按其语义返回对应状态码与文案；未识别的错误一律视为技术错误，
// 返回 500 通用文案并追加到 gin 的错误链（由日志中间件记录），
// 避免把 SQL 报错、连接信息等内部细节暴露给调用方。
func respondUserError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrUsernameExists):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, ErrInvalidCredentials):
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
	case errors.Is(err, ErrInvalidRole),
		errors.Is(err, ErrInvalidUsername),
		errors.Is(err, ErrInvalidPassword),
		errors.Is(err, ErrInvalidProvider),
		errors.Is(err, ErrInvalidExternalID):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, ErrLastAdmin),
		errors.Is(err, ErrExternalIdentityExists):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, ErrUserNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
	case errors.Is(err, ErrIdentityNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "该用户未绑定此登录方式"})
	default:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器内部错误"})
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
// @Failure 409 {object} map[string]string
// @Router /register [post]
func (h *Handler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 不透传 gin 的绑定错误（英文内部字段名），对调用方无意义。
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
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
		respondUserError(c, err)
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
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /login [post]
func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	u, err := h.service.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		respondUserError(c, err)
		return
	}

	token, err := h.issue(u)
	if err != nil {
		respondUserError(c, err)
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

// parseUserID 解析路径中的用户 ID。
// 解析失败或不合法时已写入 400 响应并返回 false，调用方应直接 return。
func parseUserID(c *gin.Context) (uint, bool) {
	// 用 ParseUint 而非 Atoi：前者直接拒绝负数与非法字符，
	// 避免 int 转 uint 时负数变成巨大正整数这类隐蔽问题。
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户 ID 不合法"})
		return 0, false
	}
	return uint(id), true
}

// adminUserView 是管理端看到的用户信息。
//
// 单独定义一个视图类型、而不是直接返回 User：一是要把「有没有密码」和「绑了哪些外部身份」
// 一并带出去，二是管理端是权限敏感的面，暴露哪些字段应该逐项写明白，
// 而不是等模型加了字段就自动跟着泄露出去。
type adminUserView struct {
	// ID 用户主键。
	ID uint `json:"id"`
	// Name 用户名。
	Name string `json:"name"`
	// Role 用户角色。
	Role Role `json:"role"`
	// HasPassword 账号是否设置了可用密码。企业微信扫码自动建出的账号没有密码。
	HasPassword bool `json:"has_password"`
	// Identities 账号绑定的外部身份。
	Identities []identityView `json:"identities"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 最后更新时间。
	UpdatedAt time.Time `json:"updated_at"`
}

// identityView 是一条外部身份绑定关系。
type identityView struct {
	// ID 绑定关系主键。
	ID uint `json:"id"`
	// Provider 提供方，如 wecom。
	Provider IdentityProvider `json:"provider"`
	// ExternalID 提供方侧的标识，如企业微信 userid。
	ExternalID string `json:"external_id"`
}

// listUsersResponse 是用户列表的返回载荷。
//
// 与订单列表的裸数组不同，这里必须带上 total —— 前端要据此算总页数。
// 这也是本项目第一个分页接口，形状就定在这里。
type listUsersResponse struct {
	// Items 当前页的用户。
	Items []adminUserView `json:"items"`
	// Total 满足条件的用户总数（不受分页限制）。
	Total int64 `json:"total"`
	// Page 生效后的页码，从 1 开始。
	Page int `json:"page"`
	// PageSize 生效后的每页条数。
	PageSize int `json:"page_size"`
}

// toAdminUserViews 把用户与其外部身份组装成管理端视图。
func toAdminUserViews(users []User, identities map[uint][]UserIdentity) []adminUserView {
	views := make([]adminUserView, 0, len(users))
	for i := range users {
		u := &users[i]

		// 用空切片而不是 nil：序列化后是 [] 而不是 null，前端可以直接 .map()。
		bound := make([]identityView, 0, len(identities[u.ID]))
		for _, identity := range identities[u.ID] {
			bound = append(bound, identityView{
				ID:         identity.ID,
				Provider:   identity.Provider,
				ExternalID: identity.ExternalID,
			})
		}

		views = append(views, adminUserView{
			ID:          u.ID,
			Name:        u.Name,
			Role:        u.Role,
			HasPassword: hasUsablePassword(u),
			Identities:  bound,
			CreatedAt:   u.CreatedAt,
			UpdatedAt:   u.UpdatedAt,
		})
	}
	return views
}

type adminCreateUserRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	// Role 目标角色，取值与 user.Role 一致（user / ops / admin）。
	Role string `json:"role" binding:"required"`
}

// AdminCreateUser 管理员建号
// @Summary 创建用户（仅管理员）
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param request body adminCreateUserRequest true "新用户信息"
// @Success 200 {object} adminUserView
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security BearerAuth
// @Router /admin/users [post]
func (h *Handler) AdminCreateUser(c *gin.Context) {
	// TODO(怀民): 实现管理员建号。要点：
	//  1. 绑定请求体，失败返回 400「请求参数不合法」（与 Register 一致，不透传 gin 的英文绑定错误）；
	//  2. 调服务层 Register 建号 —— 角色合法性、用户名 / 密码长度、用户名重复（409）
	//     都由服务层原地判定，handler 不要重复校验；
	//  3. 错误统一交给 respondUserError 映射；
	//  4. 成功返回 adminUserView（可复用 toAdminUserViews），不签发令牌 —— 建号不是登录。
	// 实现完成后删除下面这行占位响应。
	var req adminCreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	created, err := h.service.Register(c.Request.Context(), req.Username, req.Password, Role(req.Role))
	if err != nil {
		respondUserError(c, err)
		return
	}
	c.JSON(http.StatusOK, toAdminUserViews([]User{*created}, nil)[0])
}

// ListUsers 用户列表（管理端）
// @Summary 查询用户列表（仅管理员）
// @Tags 用户管理
// @Produce json
// @Param page query int false "页码，从 1 开始（默认 1）"
// @Param page_size query int false "每页条数（默认 20，上限 100）"
// @Param keyword query string false "用户名模糊匹配；纯数字时同时匹配用户 ID"
// @Success 200 {object} listUsersResponse
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /admin/users [get]
func (h *Handler) ListUsers(c *gin.Context) {
	// 解析失败一律交给服务层回落到默认值，不在这里报错。
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	result, err := h.service.ListUsers(c.Request.Context(), c.Query("keyword"), page, pageSize)
	if err != nil {
		respondUserError(c, err)
		return
	}

	userIDs := make([]uint, 0, len(result.Users))
	for _, u := range result.Users {
		userIDs = append(userIDs, u.ID)
	}
	// 一次批量查出本页所有用户的外部身份，避免逐行查询。
	identities, err := h.service.ListIdentities(c.Request.Context(), userIDs)
	if err != nil {
		respondUserError(c, err)
		return
	}

	c.JSON(http.StatusOK, listUsersResponse{
		Items:    toAdminUserViews(result.Users, identities),
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
	})
}

type updateRoleRequest struct {
	// Role 目标角色，取值与 user.Role 一致。
	Role string `json:"role" binding:"required"`
}

// UpdateUserRole 修改用户角色（管理端）
// @Summary 修改用户角色（仅管理员）
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param id path int true "用户 ID"
// @Param request body updateRoleRequest true "目标角色"
// @Success 200 {object} adminUserView
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security BearerAuth
// @Router /admin/users/{id}/role [patch]
func (h *Handler) UpdateUserRole(c *gin.Context) {
	userID, ok := parseUserID(c)
	if !ok {
		return
	}

	var req updateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 不透传 gin 的绑定错误（英文内部字段名），对调用方无意义。
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	if err := h.service.UpdateRole(c.Request.Context(), userID, Role(req.Role)); err != nil {
		respondUserError(c, err)
		return
	}

	// 回读一次再返回，让前端能就地替换那一行，而不必整表重拉。
	updated, err := h.service.FindUserByID(c.Request.Context(), userID)
	if err != nil {
		respondUserError(c, err)
		return
	}
	identities, err := h.service.ListIdentities(c.Request.Context(), []uint{userID})
	if err != nil {
		respondUserError(c, err)
		return
	}
	c.JSON(http.StatusOK, toAdminUserViews([]User{*updated}, identities)[0])
}

// UnbindIdentity 解绑用户的外部身份（管理端）
// @Summary 解绑用户的外部身份（仅管理员）
// @Tags 用户管理
// @Produce json
// @Param id path int true "用户 ID"
// @Param provider path string true "身份提供方，如 wecom"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /admin/users/{id}/identities/{provider} [delete]
func (h *Handler) UnbindIdentity(c *gin.Context) {
	userID, ok := parseUserID(c)
	if !ok {
		return
	}

	provider := IdentityProvider(c.Param("provider"))
	if err := h.service.UnbindIdentity(c.Request.Context(), userID, provider); err != nil {
		respondUserError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已解绑"})
}
