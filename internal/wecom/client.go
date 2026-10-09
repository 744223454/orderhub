// Package wecom 负责企业微信「扫码登录」的接入：构造登录链接、把授权码换成成员身份，
// 并管理调用凭证 access_token 的缓存与刷新。
//
// 依赖方向：wecom 单向依赖 user 包（复用其中的用户模型与身份提供方类型），
// user 包不反向依赖本包，与 order → user、auth → user 保持一致。
package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	// defaultOpenAPIBase 企业微信服务端 API 的正式地址。
	defaultOpenAPIBase = "https://qyapi.weixin.qq.com"
	// defaultLoginBase 企业微信扫码登录页的正式地址。
	defaultLoginBase = "https://login.work.weixin.qq.com"
	// loginPath 扫码登录页路径。
	loginPath = "/wwlogin/sso/login"
	// loginTypeCorpApp 表示「企业自建应用登录」。
	loginTypeCorpApp = "CorpApp"
	// tokenPath 获取 access_token 的接口路径。
	tokenPath = "/cgi-bin/gettoken"
	// userInfoPath 用授权码换成员身份的接口路径。
	userInfoPath = "/cgi-bin/auth/getuserinfo"
	// departmentListPath 获取全量部门列表的接口路径（无需参数，一次返回所有部门）。
	departmentListPath = "/cgi-bin/department/list"
	// userListPath 获取部门成员的接口路径（department_id 必填）。
	userListPath = "/cgi-bin/user/list"

	// loginCallbackPath 登录回调页路径（前端页面）。
	loginCallbackPath = "/login/wecom/callback"
	// bindCallbackPath 绑定回调页路径（前端页面）。
	bindCallbackPath = "/login/wecom/bind"

	// defaultHTTPTimeout 是调用企业微信接口的超时时间。
	defaultHTTPTimeout = 5 * time.Second
	// maxResponseBytes 是响应体读取上限，避免异常响应撑爆内存。
	maxResponseBytes = 64 << 10
	// maxDirectoryResponseBytes 是通讯录接口的响应体上限。
	//
	// 必须比 maxResponseBytes 宽松得多：user/list 一次返回一个部门的**全部**成员，
	// 大部门几 MB 很正常。64KB 会把响应截断成半截 JSON ——
	// 报出来的是一个「解析响应失败」，与真实原因（响应太大）隔了十万八千里。
	// 截断不会造成静默丢数据（JSON 解析必然失败），但会让功能直接不可用。
	maxDirectoryResponseBytes = 8 << 20
	// tokenExpiryMargin 是 access_token 的提前刷新余量。
	tokenExpiryMargin = 5 * time.Minute
	// minTokenTTL 是缓存有效期的下限。企微的 expires_in 正常恒为 7200，
	// 万一返回了异常小的值，缓存的过期时刻会落在过去，导致每次调用都重新取 token
	// 进而踩到 gettoken 的频率限制；这里退化为固定短 TTL 兜底。
	minTokenTTL = time.Minute
	// tokenGroupKey 是 singleflight 的合并键：每个 Client 只维护一个应用的 token，
	// 用固定键即可。
	tokenGroupKey = "access_token"

	// tokenTimeout 是**单次取 access_token** 的上限。
	//
	// 不能依赖调用方传来的 ctx：那是 HTTP 请求的 ctx，客户端一断开就取消。
	// 而这次取凭证是 shared（singleflight 合并后的那一份）——某个调用方关掉页面就
	// 让所有等着的请求一起失败，还会留下一个「context canceled」的假故障。
	// 所以抓取挂在脱离请求生命周期的 ctx 上，并自带超时兜底。
	//
	// 取 5s：gettoken 只是一个很小的 HTTP GET，5s 足够；它与 directoryTimeout
	// 用 20s 的原因不同 —— 目录抓取要逐部门并发拉全量成员，耗时量级完全不同。
	tokenTimeout = 5 * time.Second
)

// Intent 表示本次扫码授权的用途。
type Intent string

const (
	// IntentLogin 扫码后登录；该企业微信身份未绑定账号时自动建号。
	IntentLogin Intent = "login"
	// IntentBind 扫码后把该企业微信身份绑定到当前登录账号。
	IntentBind Intent = "bind"
)

// ParseIntent 解析前端传入的授权用途，非法值一律回落到 IntentLogin。
func ParseIntent(raw string) Intent {
	if raw == string(IntentBind) {
		return IntentBind
	}
	return IntentLogin
}

// Config 是企业微信接入所需的配置。
//
// OpenAPIBase / LoginBase / HTTPClient 仅用于测试注入，线上留空即走企微正式地址与默认客户端。
type Config struct {
	// CorpID 企业 ID，对应企微的 corpid。
	CorpID string
	// AgentID 自建应用的 AgentId。
	AgentID string
	// Secret 自建应用的凭证密钥。绝不写入日志、绝不返回给前端。
	Secret string
	// WebBaseURL 前端站点地址（形如 http://dev.orderhub.local:3000），用于拼装授权回调地址。
	// 其中**域名部分（含端口）必须与企微后台配置的「授权回调域」逐字一致**。
	// 注意别与「可信域名」混淆：两者是企微后台两套独立配置，前者给扫码登录用，后者给网页授权 / JS-SDK 用。
	WebBaseURL string
	// OpenAPIBase 服务端 API 基地址，留空使用官方地址。
	OpenAPIBase string
	// LoginBase 扫码登录页基地址，留空使用官方地址。
	LoginBase string
	// HTTPClient 自定义 HTTP 客户端，留空使用带超时的默认客户端。
	HTTPClient *http.Client
}

// Missing 返回配置中缺失的字段名，供启动阶段判断能否启用企业微信登录。
func (c Config) Missing() []string {
	var missing []string
	if c.CorpID == "" {
		missing = append(missing, "CorpID")
	}
	if c.AgentID == "" {
		missing = append(missing, "AgentID")
	}
	if c.Secret == "" {
		missing = append(missing, "Secret")
	}
	if c.WebBaseURL == "" {
		missing = append(missing, "WebBaseURL")
	}
	return missing
}

// apiResponse 是企微接口的通用响应外壳。
// 本项目只用到下面几个字段，因此不为每个接口单独定义结构体。
type apiResponse struct {
	// ErrCode 返回码，0 表示成功。
	ErrCode int `json:"errcode"`
	// ErrMsg 返回码的文字描述。仅作参考，企微可能调整，不得作为判断依据。
	ErrMsg string `json:"errmsg"`
	// AccessToken gettoken 返回的调用凭证。
	AccessToken string `json:"access_token"`
	// ExpiresIn 凭证有效期（秒）。
	ExpiresIn int `json:"expires_in"`
	// UserID 企业成员 userid。
	UserID string `json:"userid"`
	// OpenID 非企业成员的标识。
	OpenID string `json:"openid"`
}

// code 实现 codeCarrier。
func (r *apiResponse) code() int { return r.ErrCode }

// message 实现 codeCarrier。
func (r *apiResponse) message() string { return r.ErrMsg }

// codeCarrier 是企微响应里所有接口共有的那一部分：返回码。
//
// 抽出它的用途是让「判返回码 + 凭证失效重试一次」这套逻辑对**所有**接口通用，
// 而不必给每个接口各写一遍重试。各接口的响应结构体都实现它。
type codeCarrier interface {
	// code 返回企微的 errcode，0 表示成功。
	code() int
	// message 返回企微的 errmsg，仅用于日志。
	message() string
}

// Identity 是企业微信授权码换回的成员身份。
type Identity struct {
	// UserID 企业成员 userid；非企业成员为空。
	UserID string
	// OpenID 非企业成员的标识（对企业唯一）；企业成员为空。
	OpenID string
}

// IsCorpMember 判断该身份是否属于本企业成员。
func (i Identity) IsCorpMember() bool {
	return i.UserID != ""
}

// Client 是企业微信服务端 API 的客户端。
//
// 并发安全：可变状态只有 access_token 缓存与通讯录快照（各自有锁与 singleflight 保护），
// 可被多个 goroutine 同时调用。
type Client struct {
	cfg  Config
	http *http.Client

	// group 把并发的取 token 请求合并成一次真实调用。
	// 企微会限制 gettoken 的调用频率，启动瞬间若每个请求各打一次，很容易踩到限制。
	group singleflight.Group

	// mu 保护 token 与 tokenExp，两者必须同时读写，因此共用一个锁。
	mu       sync.RWMutex
	token    string
	tokenExp time.Time

	// dirMu 保护通讯录快照，与 token 分开用两把锁：两者寿命完全不同
	// （token 按小时，快照按分钟），共锁只会让读 token 的请求排队等通讯录写完。
	dirMu   sync.RWMutex
	dir     *DirectorySnapshot
	dirTime time.Time
}

// NewClient 创建企业微信客户端。配置中的地址留空时回落到企微正式地址。
func NewClient(cfg Config) *Client {
	if cfg.OpenAPIBase == "" {
		cfg.OpenAPIBase = defaultOpenAPIBase
	}
	if cfg.LoginBase == "" {
		cfg.LoginBase = defaultLoginBase
	}
	cfg.OpenAPIBase = strings.TrimRight(cfg.OpenAPIBase, "/")
	cfg.LoginBase = strings.TrimRight(cfg.LoginBase, "/")
	cfg.WebBaseURL = strings.TrimRight(cfg.WebBaseURL, "/")

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Client{cfg: cfg, http: httpClient}
}

// LoginURL 构造企业微信扫码登录链接。
//
// state 由调用方生成并负责校验；intent 决定回调落到前端哪个页面。
// 链接里的 redirect_uri 会被 URL 编码后作为查询参数拼入。
func (c *Client) LoginURL(state string, intent Intent) string {
	query := url.Values{}
	query.Set("login_type", loginTypeCorpApp)
	query.Set("appid", c.cfg.CorpID)
	query.Set("agentid", c.cfg.AgentID)
	query.Set("redirect_uri", c.callbackURL(intent))
	query.Set("state", state)
	return c.cfg.LoginBase + loginPath + "?" + query.Encode()
}

// callbackURL 拼装授权回调地址。
//
// 回调页放在前端：企微只校验回调地址的**域名**是否落在「授权回调域」下，路径可自定。
// 让页面而非后端接收授权码，是标准的授权码流程 —— 后端因此不必把访问令牌塞进 URL
// （会进浏览器历史、Referer 与各级日志），也省掉了一张一次性票据的存储。
func (c *Client) callbackURL(intent Intent) string {
	if intent == IntentBind {
		return c.cfg.WebBaseURL + bindCallbackPath
	}
	return c.cfg.WebBaseURL + loginCallbackPath
}

// ExchangeCode 用授权码换回成员身份。
//
// 有一个必须留意的接口语义：**非企业成员授权时企微同样返回 errcode=0**，
// 只是把 userid 换成了 openid。因此不能只看错误码，还要判 UserID 是否为空，
// 调用方应通过 Identity.IsCorpMember 决定是否放行。
//
// 授权码只能用一次、5 分钟未使用即过期，重复使用会返回 40029。
func (c *Client) ExchangeCode(ctx context.Context, code string) (Identity, error) {
	if code == "" {
		return Identity{}, ErrInvalidAuthCode
	}

	resp, err := c.callWithToken(ctx, userInfoPath, url.Values{"code": {code}})
	if err != nil {
		return Identity{}, err
	}
	return Identity{UserID: resp.UserID, OpenID: resp.OpenID}, nil
}

// doWithToken 取一次 access_token 并调用接口，同时把本次使用的 token 返回给调用方
// ——遇到凭证失效时，调用方可以据此定点清除缓存。
func (c *Client) doWithToken(ctx context.Context, path string, params url.Values) (*apiResponse, string, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, "", err
	}

	resp, err := c.getJSON(ctx, path, withToken(params, token))
	if err != nil {
		return nil, token, err
	}
	return resp, token, nil
}

// withToken 复制一份查询参数并附上 access_token。
// 复制而不是就地改：调用方的 params 可能被复用（例如重试时再传一次）。
func withToken(params url.Values, token string) url.Values {
	query := url.Values{}
	for key, values := range params {
		query[key] = values
	}
	query.Set("access_token", token)
	return query
}

// callWithToken 调用接口，并在遇到「凭证失效」类错误码时刷新凭证重试一次。
//
// 只重试一次是刻意的：企微可能出于运营需要提前让 access_token 失效，不重试会把
// 偶发失效当成调用失败；而无限重试会在配置本身有问题（例如 Secret 写错）时变成死循环，
// 还会持续消耗 gettoken 的调用配额。
func (c *Client) callWithToken(ctx context.Context, path string, params url.Values) (*apiResponse, error) {
	resp, token, err := c.doWithToken(ctx, path, params)
	if err != nil {
		return nil, err
	}
	if resp.ErrCode == 0 {
		return resp, nil
	}

	if isTokenInvalid(resp.ErrCode) {
		// 只清掉自己刚用过的那个 token：并发场景下别的 goroutine 可能已经换上了新 token，
		// 不看一眼就清，会把它们的凭证一起作废。
		c.invalidateToken(token)

		retried, _, retryErr := c.doWithToken(ctx, path, params)
		switch {
		case retryErr != nil:
			return nil, retryErr
		case retried.ErrCode == 0:
			return retried, nil
		default:
			return nil, translateErrCode(retried.ErrCode, retried.ErrMsg)
		}
	}

	return nil, translateErrCode(resp.ErrCode, resp.ErrMsg)
}

// callWithTokenInto 与 callWithToken 同构，但把响应解析到调用方指定的结构体
// （dst 须实现 codeCarrier），用于字段比通用外壳更多的接口 —— 例如
// department/list 多一个 department[]、user/list 多一个 userlist[]。
//
// ⚠️ 实测结论，别再凭印象改写：这两个接口**成功时返回的也是对象**
// （`{"errcode":0,"errmsg":"ok","department":[...]}`），不是裸数组。
// 官方文档示例只贴了数组部分，很容易让人以为外层没有 errcode 外壳，
// 于是把「成功响应」按数组解析、再对失败响应做特判，多出一整条不该存在的分支。
func (c *Client) callWithTokenInto(ctx context.Context, path string, params url.Values, dst codeCarrier) error {
	code, msg, token, err := c.fetchInto(ctx, path, params, dst)
	if err != nil {
		return err
	}
	if code == 0 {
		return nil
	}
	if !isTokenInvalid(code) {
		return translateErrCode(code, msg)
	}

	// 与 callWithToken 同样只重试一次、只清自己用过的那个 token。
	c.invalidateToken(token)

	retriedCode, retriedMsg, _, retryErr := c.fetchInto(ctx, path, params, dst)
	if retryErr != nil {
		return retryErr
	}
	if retriedCode == 0 {
		return nil
	}
	return translateErrCode(retriedCode, retriedMsg)
}

// fetchInto 取一次 access_token 并调用接口，返回本次的返回码与所用凭证。
// 返回值偏多，但都是重试逻辑必需的：没有 token 就无法定点清缓存。
func (c *Client) fetchInto(
	ctx context.Context,
	path string,
	params url.Values,
	dst codeCarrier,
) (code int, msg string, token string, err error) {
	token, err = c.accessToken(ctx)
	if err != nil {
		return 0, "", "", err
	}

	if err := c.getJSONInto(ctx, path, withToken(params, token), maxDirectoryResponseBytes, dst); err != nil {
		return 0, "", token, err
	}
	return dst.code(), dst.message(), token, nil
}

// accessToken 返回可用的 access_token，必要时向企微获取。
// 有效期内的并发调用会被 singleflight 合并成一次真实请求。
func (c *Client) accessToken(ctx context.Context) (string, error) {
	if token, ok := c.cachedToken(); ok {
		return token, nil
	}

	value, err, _ := c.group.Do(tokenGroupKey, func() (any, error) {
		// 二次检查：等待合并期间可能已有别的 goroutine 取回了新凭证。
		if token, ok := c.cachedToken(); ok {
			return token, nil
		}

		// 脱离调用方 ctx 并自带超时：这次取凭证可能同时服务多个调用方，
		// 不能被其中任何一个的断连拖下水（详见 tokenTimeout 的说明）。
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tokenTimeout)
		defer cancel()

		token, err := c.fetchToken(fetchCtx)
		if err != nil {
			return nil, err
		}

		return token, nil
	})
	if err != nil {
		return "", err
	}

	token, ok := value.(string)
	if !ok {
		return "", ErrWecomUnavailable
	}
	return token, nil
}

// cachedToken 返回尚未过期的凭证。
func (c *Client) cachedToken() (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.token == "" || time.Now().After(c.tokenExp) {
		return "", false
	}
	return c.token, true
}

// fetchToken 向企微获取 access_token 并写入缓存。
func (c *Client) fetchToken(ctx context.Context) (string, error) {
	query := url.Values{}
	query.Set("corpid", c.cfg.CorpID)
	query.Set("corpsecret", c.cfg.Secret)

	resp, err := c.getJSON(ctx, tokenPath, query)
	if err != nil {
		return "", err
	}
	if resp.ErrCode != 0 {
		return "", translateErrCode(resp.ErrCode, resp.ErrMsg)
	}
	if resp.AccessToken == "" {
		return "", ErrWecomUnavailable
	}

	// 提前 tokenExpiryMargin 判定过期，避免临界点上「刚取到就失效」。
	ttl := time.Duration(resp.ExpiresIn)*time.Second - tokenExpiryMargin
	if ttl < minTokenTTL {
		ttl = minTokenTTL
	}

	c.mu.Lock()
	c.token = resp.AccessToken
	c.tokenExp = time.Now().Add(ttl)
	c.mu.Unlock()

	return resp.AccessToken, nil
}

// invalidateToken 清除缓存中指定的凭证。
// 传入 expected 是为了避免误清：并发场景下别的 goroutine 可能刚换上新凭证。
func (c *Client) invalidateToken(expected string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == expected {
		c.token = ""
		c.tokenExp = time.Time{}
	}
}

// getJSON 发起 GET 请求并解析 JSON 响应外壳。
//
// path 与 query 分开传，是为了让错误文案里只出现接口路径。
// 绝不能把完整 URL 写进错误：gettoken 的查询串里带着 corpsecret，
// 一旦进了日志就等于泄漏凭证。
func (c *Client) getJSON(ctx context.Context, path string, query url.Values) (*apiResponse, error) {
	var parsed apiResponse
	if err := c.getJSONInto(ctx, path, query, maxResponseBytes, &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

// getJSONInto 发起 GET 并把响应解析到 dst，limit 指定响应体读取上限。
func (c *Client) getJSONInto(ctx context.Context, path string, query url.Values, limit int64, dst any) error {
	body, err := c.doGet(ctx, path, query, limit)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("解析企业微信 %s 响应失败: %w", path, err)
	}
	return nil
}

// doGet 执行一次 GET 并返回响应体，负责请求构造、超时、体积上限与状态码检查。
// 错误文案里只出现接口路径，不出现完整 URL（原因见 getJSON 的说明）。
func (c *Client) doGet(ctx context.Context, path string, query url.Values, limit int64) ([]byte, error) {
	endpoint := c.cfg.OpenAPIBase + path + "?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("构造企业微信 %s 请求失败: %s", path, c.describeRequestError(err))
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求企业微信 %s 失败: %s", path, c.describeRequestError(err))
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("读取企业微信 %s 响应失败: %s", path, c.describeRequestError(err))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s 返回状态码 %d", ErrWecomUnavailable, path, resp.StatusCode)
	}
	return body, nil
}

// describeRequestError 提取请求错误中不含 URL 的部分，并兜底抹掉 corpsecret。
//
// http.Client.Do 返回的 *url.Error 会把完整请求 URL 写进 Error()，
// 直接包装就等于把密钥写进日志。这里取内层错误（描述的是连接、超时这类故障本身），
// 再用 redact 做最后一道保险 —— 泄密的代价远高于日志少一点上下文。
func (c *Client) describeRequestError(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return c.redact(urlErr.Err.Error())
	}
	return c.redact(err.Error())
}

// redact 把文案里可能出现的 corpsecret 替换掉。
func (c *Client) redact(message string) string {
	if c.cfg.Secret == "" {
		return message
	}
	return strings.ReplaceAll(message, c.cfg.Secret, "***")
}

// isTokenInvalid 判断错误码是否属于「access_token 不可用」，遇到时应刷新凭证后重试。
// 取值来自企业微信全局错误码文档。
func isTokenInvalid(code int) bool {
	switch code {
	case 40014, // 不合法的 access_token
		41001, // 缺少 access_token 参数
		42001, // access_token 已超时
		42007: // access_token 已失效，需重新获取
		return true
	}
	return false
}

// translateErrCode 把企微 errcode 翻译成本模块的哨兵错误。
//
// 纪律：只按 errcode 数值判断，绝不比较 errmsg —— 企微明确说明 errmsg 仅作参考。
// 翻译不出来的错误码一律归为技术错误，细节只进日志、不回显给调用方。
func translateErrCode(code int, msg string) error {
	switch code {
	case 40029:
		return fmt.Errorf("%w: %s", ErrInvalidAuthCode, msg)
	case 45009:
		// 频率超限是**可重试**的临时状态，与配置类故障分开：调用方据此提示
		// 「稍后重试」，而不是让人去查配置。
		return fmt.Errorf("%w: errcode=%d msg=%s", ErrRateLimited, code, msg)
	case 60011:
		// 无权限：多数情况是自建应用的可见范围没覆盖目标部门。
		return fmt.Errorf("%w: errcode=%d msg=%s", ErrNoPermission, code, msg)
	case 40001, // 不合法的 Secret / access_token 获取凭证失败
		40013, // 不合法的 CorpID
		50001, // 回调地址未登记可信域名
		60020: // 调用来源 IP 不在企业可信 IP 列表
		return fmt.Errorf("%w: errcode=%d msg=%s", ErrCorpConfig, code, msg)
	}
	return fmt.Errorf("%w: errcode=%d msg=%s", ErrWecomUnavailable, code, msg)
}
