package wecom

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/744223454/orderhub/internal/user"
	"github.com/gin-gonic/gin"
)

// 授权状态（OAuth state）相关常量。
const (
	// stateCookieName 是承载 state 的 Cookie 名。
	stateCookieName = "orderhub_oauth_state"
	// stateCookiePath 限定 Cookie 只在 /api 下发送。
	// 它带 HttpOnly，页面脚本读不到；回调页向 /api/auth/wecom/login 发请求时由浏览器自动带上。
	stateCookiePath = "/api"
	// stateTTL 是 state 的有效期，需覆盖「打开扫码页 → 扫码 → 确认授权」的全过程。
	stateTTL = 10 * time.Minute
	// stateBytes 是 state 随机串的字节数。
	stateBytes = 16
)

// LoginFunc 用企业微信 userid 完成本地登录，返回已登录用户与访问令牌。
//
// 由 main.go 注入（内部组合 user.Service.LoginByExternal 与 auth.GenerateToken），
// 使本模块不必依赖 auth 包，只依赖 user 包的公开类型。
type LoginFunc func(ctx context.Context, externalID string) (*user.User, string, error)

// BindFunc 把企业微信身份绑定到指定账号。由 main.go 注入。
type BindFunc func(ctx context.Context, userID uint, externalID string) error

// Handler 承载企业微信登录相关的 HTTP 接口。
type Handler struct {
	client *Client
	login  LoginFunc
	bind   BindFunc
}

// NewHandler 创建企业微信接口处理器。
func NewHandler(client *Client, login LoginFunc, bind BindFunc) *Handler {
	return &Handler{client: client, login: login, bind: bind}
}

// respondWecomError 把服务层错误映射为 HTTP 响应。
//
// 与其它模块最大的差别：**对外文案一律是本模块写死的一句中文，绝不回显 err.Error()**。
// 企微的 errmsg 是英文，且 50001（回调地址未登记可信域名）、60020（调用来源 IP 不在可信 IP 列表）
// 这类错误会带出回调域名、出口 IP 等部署信息，原样返回等于对外暴露内部配置。
// 因此未识别的错误一律 500 + 通用文案，细节只追加到 gin 的错误链（由日志中间件记录）。
func respondWecomError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidAuthCode):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "授权已失效，请重新登录"})
	case errors.Is(err, ErrInvalidState):
		c.JSON(http.StatusBadRequest, gin.H{"error": "授权状态校验失败，请重新发起登录"})
	case errors.Is(err, ErrNotCorpMember):
		c.JSON(http.StatusForbidden, gin.H{"error": "仅企业成员可使用企业微信登录"})
	case errors.Is(err, user.ErrExternalIdentityExists):
		c.JSON(http.StatusConflict, gin.H{"error": "该企业微信已绑定其他账号"})
	case errors.Is(err, user.ErrUserNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "账号不存在"})
	case errors.Is(err, user.ErrInvalidProvider), errors.Is(err, user.ErrInvalidExternalID):
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
	default:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器内部错误"})
	}
}

// authorizeResponse 是发起授权接口的返回载荷。
type authorizeResponse struct {
	// URL 企业微信扫码登录页地址，前端应整页跳转到它（不是 fetch）。
	URL string `json:"url"`
}

// newState 生成一个不可预测的授权状态串。
func newState() (string, error) {
	buf := make([]byte, stateBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成授权状态失败: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// consumeState 校验并立即作废本次授权状态。
//
// 校验通过后必须马上清 Cookie：state 是一次性的，留着就等于允许它被重放。
// 因此不论成败都清，省得残留值让下一次校验的判断更绕。
func consumeState(c *gin.Context, got string) error {
	expected, err := c.Cookie(stateCookieName)

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(stateCookieName, "", -1, stateCookiePath, "", false, true)

	// 定长比较：字符串比较会在首个不同字符处提前返回，理论上可被用来逐字节猜测。
	if err != nil || expected == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(got)) != 1 {
		return ErrInvalidState
	}
	return nil
}

// Authorize 获取企业微信扫码登录链接
// @Summary 获取企业微信扫码登录链接
// @Tags 企业微信登录
// @Produce json
// @Param intent query string false "授权用途：login（默认，扫码登录）或 bind（绑定到当前账号）"
// @Success 200 {object} authorizeResponse
// @Failure 500 {object} map[string]string
// @Router /auth/wecom/authorize [get]
func (h *Handler) Authorize(c *gin.Context) {
	intent := ParseIntent(c.Query("intent"))

	state, err := newState()
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器内部错误"})
		return
	}

	// state 存进 HttpOnly Cookie，回调时用它比对，挡住「拿别人的授权码诱导我登录」这类
	// OAuth CSRF：攻击者能让浏览器跳到回调页，却读不到、也塞不进这个 Cookie。
	//
	// SameSite 选 Lax 而不是 None 是必须的取舍：本地是 http，None 必须搭配 Secure 才会被
	// 浏览器接受，否则直接失效；而 Lax 对「跨站顶层 GET 导航」是放行的，企微 302 回调恰好是这种请求。
	// 它的前提是前后端同站 —— 本项目前端只请求同源 /api/*（经 next.config.ts 的 rewrites 代理），
	// 该前提成立。将来若前后端分属不同站点，这里要改成按站点配置或换成签名 state。
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(stateCookieName, state, int(stateTTL.Seconds()), stateCookiePath, "", false, true)

	c.JSON(http.StatusOK, authorizeResponse{URL: h.client.LoginURL(state, intent)})
}

// callbackRequest 是回调换票与绑定接口的请求体。
type callbackRequest struct {
	// Code 企微回调带回的授权码，只能用一次、5 分钟过期。
	Code string `json:"code" binding:"required"`
	// State 发起授权时下发的状态串，用于防 CSRF。
	State string `json:"state" binding:"required"`
}

// bindCallback 解析回调请求体，失败时已写入 400 响应并返回 false。
//
// 注意「用户取消授权」不归这里管：企微在用户拒绝时只回传 state、不带 code，
// 前端页面据此直接提示已取消，根本不会发这个请求。
func bindCallback(c *gin.Context) (callbackRequest, bool) {
	var req callbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 不透传 gin 的绑定错误（英文内部字段名），对调用方没有意义。
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return callbackRequest{}, false
	}
	return req, true
}

// loginResponse 是扫码登录成功后的响应载荷。
// 形状与密码登录接口（POST /api/login）保持一致，前端可以复用同一套会话写入逻辑。
type loginResponse struct {
	// Token Bearer 访问令牌，后续请求需放入 Authorization 头。
	Token string `json:"token"`
	// User 登录成功的用户信息（不含密码哈希）。
	User *user.User `json:"user"`
}

// Login 企业微信扫码登录（用回调带回的授权码换票）
// @Summary 企业微信扫码登录
// @Tags 企业微信登录
// @Accept json
// @Produce json
// @Param request body callbackRequest true "回调参数"
// @Success 200 {object} loginResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Router /auth/wecom/login [post]
func (h *Handler) Login(c *gin.Context) {
	req, ok := bindCallback(c)
	if !ok {
		return
	}
	if err := consumeState(c, req.State); err != nil {
		respondWecomError(c, err)
		return
	}

	identity, err := h.client.ExchangeCode(c.Request.Context(), req.Code)
	if err != nil {
		respondWecomError(c, err)
		return
	}
	if !identity.IsCorpMember() {
		// 非企业成员授权时企微同样返回 errcode=0，只是把 userid 换成了 openid。
		// 必须在这里拦下，否则会拿 openid 去建号，污染 user_identities。
		respondWecomError(c, ErrNotCorpMember)
		return
	}

	u, token, err := h.login(c.Request.Context(), identity.UserID)
	if err != nil {
		respondWecomError(c, err)
		return
	}
	c.JSON(http.StatusOK, loginResponse{Token: token, User: u})
}

// Bind 把企业微信身份绑定到当前登录账号
// @Summary 绑定企业微信身份
// @Tags 企业微信登录
// @Accept json
// @Produce json
// @Param request body callbackRequest true "回调参数"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security BearerAuth
// @Router /auth/wecom/bind [post]
func (h *Handler) Bind(c *gin.Context) {
	userID, ok := user.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	req, ok := bindCallback(c)
	if !ok {
		return
	}
	if err := consumeState(c, req.State); err != nil {
		respondWecomError(c, err)
		return
	}

	identity, err := h.client.ExchangeCode(c.Request.Context(), req.Code)
	if err != nil {
		respondWecomError(c, err)
		return
	}
	if !identity.IsCorpMember() {
		respondWecomError(c, ErrNotCorpMember)
		return
	}

	if err := h.bind(c.Request.Context(), userID, identity.UserID); err != nil {
		respondWecomError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "绑定成功"})
}
