package wecom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/744223454/orderhub/internal/user"
	"github.com/gin-gonic/gin"
)

// newTestRouter 装配一个只含企业微信接口的路由。
// loginUserID 非 nil 时模拟「已通过认证中间件」，用于测绑定接口。
//
// 管理员接口也一并挂上：真实的权限判定由 auth.RequireRole 中间件完成，
// 这里只验证处理器本身的行为，因此不再套一层角色校验。
func newTestRouter(h *Handler, loginUserID *uint) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	api := router.Group("/api")
	PublicRoutes(api, h)

	authed := api.Group("", func(c *gin.Context) {
		if loginUserID != nil {
			c.Set(user.ContextUserIDKey, *loginUserID)
		}
		c.Next()
	})
	AuthedRoutes(authed, h)
	AdminRoutes(authed, h)

	return router
}

// postCallback 向回调类接口发一次 JSON 请求，并带上指定的 state Cookie。
func postCallback(t *testing.T, router *gin.Engine, path, body, stateCookie string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if stateCookie != "" {
		req.AddCookie(&http.Cookie{Name: stateCookieName, Value: stateCookie})
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// responseCookie 从响应里取出指定名字的 Cookie，不存在时返回 nil。
func responseCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

// TestAuthorizeReturnsURLAndStateCookie 锁定发起授权接口的两个产出：
// 登录链接（内含 state）与承载同一 state 的 HttpOnly Cookie。
func TestAuthorizeReturnsURLAndStateCookie(t *testing.T) {
	h := NewHandler(newFakeClient(t, newFakeWecom(t)), nil, nil)

	rec := httptest.NewRecorder()
	newTestRouter(h, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/wecom/authorize?intent=bind", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%s", rec.Code, rec.Body.String())
	}

	var body authorizeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法的 JSON: %v", err)
	}

	parsed, err := url.Parse(body.URL)
	if err != nil {
		t.Fatalf("返回的登录链接无法解析: %v", err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatal("登录链接里缺少 state")
	}
	if got := parsed.Query().Get("redirect_uri"); got != "http://dev.orderhub.local:3000/login/wecom/bind" {
		t.Errorf("bind 用途的回调地址期望指向绑定页，实际 %q", got)
	}

	cookie := responseCookie(rec, stateCookieName)
	if cookie == nil {
		t.Fatal("未下发 state Cookie，回调时无从校验，OAuth CSRF 防护形同虚设")
	}
	if cookie.Value != state {
		t.Errorf("Cookie 中的 state 与链接中的不一致：%q vs %q", cookie.Value, state)
	}
	if !cookie.HttpOnly {
		t.Error("state Cookie 必须是 HttpOnly，否则页面脚本可读取")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("state Cookie 期望 SameSite=Lax，实际 %v", cookie.SameSite)
	}
}

// TestLoginHappyPath 覆盖正常闭环：state 校验通过 → 换票 → 建号/查号 → 返回令牌与用户。
func TestLoginHappyPath(t *testing.T) {
	f := newFakeWecom(t)
	f.userResponse = func(int, string) string { return `{"errcode":0,"errmsg":"ok","userid":"ZengMaiKuan"}` }

	var gotExternalID string
	login := func(_ context.Context, externalID string) (*user.User, string, error) {
		gotExternalID = externalID
		return &user.User{ID: 7, Name: "wecom_ZengMaiKuan", Role: user.RoleUser}, "jwt-token", nil
	}

	rec := postCallback(t, newTestRouter(NewHandler(newFakeClient(t, f), login, nil), nil),
		"/api/auth/wecom/login", `{"code":"c1","state":"s1"}`, "s1")

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if gotExternalID != "ZengMaiKuan" {
		t.Errorf("应把企微 userid 交给登录逻辑，实际 %q", gotExternalID)
	}

	var body loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法的 JSON: %v", err)
	}
	if body.Token != "jwt-token" {
		t.Errorf("令牌期望 jwt-token，实际 %q", body.Token)
	}
	if body.User == nil || body.User.ID != 7 {
		t.Errorf("应答里应带上登录用户，实际 %+v", body.User)
	}

	// state 是一次性的：校验完必须立刻清掉，否则同一个 state 能被重放。
	cookie := responseCookie(rec, stateCookieName)
	if cookie == nil || cookie.MaxAge >= 0 {
		t.Errorf("响应应清除 state Cookie，实际 %+v", cookie)
	}
}

// TestLoginRejectsMismatchedState 锁定 CSRF 防护：state 对不上时既不放行，也绝不能建号。
func TestLoginRejectsMismatchedState(t *testing.T) {
	f := newFakeWecom(t)

	loginCalled := false
	login := func(_ context.Context, _ string) (*user.User, string, error) {
		loginCalled = true
		return &user.User{ID: 1, Role: user.RoleUser}, "jwt", nil
	}

	rec := postCallback(t, newTestRouter(NewHandler(newFakeClient(t, f), login, nil), nil),
		"/api/auth/wecom/login", `{"code":"c1","state":"attacker"}`, "victim")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("state 不匹配应返回 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if loginCalled {
		t.Error("state 校验未通过时绝不能进入建号/签发流程")
	}
	if got := f.userInfoFetched(); got != 0 {
		t.Errorf("state 校验未通过时不应消费授权码，实际调用了 %d 次身份接口", got)
	}
}

// TestLoginRejectsMissingStateCookie 模拟「浏览器没带上 state Cookie」：
// 例如用户从别的站点被诱导直接跳到回调页，此时必须拒绝。
func TestLoginRejectsMissingStateCookie(t *testing.T) {
	f := newFakeWecom(t)

	rec := postCallback(t, newTestRouter(NewHandler(newFakeClient(t, f), nil, nil), nil),
		"/api/auth/wecom/login", `{"code":"c1","state":"s1"}`, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺少 state Cookie 应返回 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

// TestLoginRequiresCodeAndState 锁定请求体必填校验。
// 注意「用户取消授权」不会走到这里：企微在用户拒绝时只回传 state、不带 code，
// 由前端页面直接提示，不发起本请求。
func TestLoginRequiresCodeAndState(t *testing.T) {
	h := NewHandler(newFakeClient(t, newFakeWecom(t)), nil, nil)
	router := newTestRouter(h, nil)

	for _, body := range []string{`{}`, `{"code":"c1"}`, `{"state":"s1"}`} {
		rec := postCallback(t, router, "/api/auth/wecom/login", body, "s1")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("请求体 %s 期望 400，实际 %d", body, rec.Code)
		}
	}
}

// TestLoginRejectsNonCorpMember 锁定「非企业成员」分支：
// 企微此时仍返回 errcode=0，只是给的是 openid，必须拦下且不得建号。
func TestLoginRejectsNonCorpMember(t *testing.T) {
	f := newFakeWecom(t)
	f.userResponse = func(int, string) string {
		return `{"errcode":0,"errmsg":"ok","openid":"o-abc","external_userid":"wm-abc"}`
	}

	loginCalled := false
	login := func(_ context.Context, _ string) (*user.User, string, error) {
		loginCalled = true
		return &user.User{ID: 1, Role: user.RoleUser}, "jwt", nil
	}

	rec := postCallback(t, newTestRouter(NewHandler(newFakeClient(t, f), login, nil), nil),
		"/api/auth/wecom/login", `{"code":"c1","state":"s1"}`, "s1")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("非企业成员应返回 403，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if loginCalled {
		t.Error("非企业成员绝不能进入建号流程，否则会把 openid 当作 userid 写进绑定表")
	}
}

// TestLoginRejectsInvalidAuthCode 锁定授权码失效的用户可见行为。
func TestLoginRejectsInvalidAuthCode(t *testing.T) {
	f := newFakeWecom(t)
	f.userResponse = func(int, string) string { return `{"errcode":40029,"errmsg":"invalid code"}` }

	rec := postCallback(t, newTestRouter(NewHandler(newFakeClient(t, f), nil, nil), nil),
		"/api/auth/wecom/login", `{"code":"used","state":"s1"}`, "s1")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("授权码失效应返回 401，实际 %d：%s", rec.Code, rec.Body.String())
	}
	// 用户需要看懂该做什么，而不是看到一句英文原始报错。
	if !strings.Contains(rec.Body.String(), "重新登录") {
		t.Errorf("文案应提示重新登录，实际 %q", rec.Body.String())
	}
}

// TestErrorResponseNeverEchoesWecomMessage 锁定「不回显企微 errmsg」。
// 企微的错误描述里常带出调用来源 IP、回调域名等部署信息，原样返回等于对外暴露内部配置。
func TestErrorResponseNeverEchoesWecomMessage(t *testing.T) {
	const leaky = `{"errcode":60020,"errmsg":"not allow to access from your ip, hint: from ip 113.132.219.62"}`
	f := newFakeWecom(t)
	f.userResponse = func(int, string) string { return leaky }

	rec := postCallback(t, newTestRouter(NewHandler(newFakeClient(t, f), nil, nil), nil),
		"/api/auth/wecom/login", `{"code":"c1","state":"s1"}`, "s1")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("配置类故障应返回 500，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leaked := range []string{"113.132.219.62", "not allow to access", "ip"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(leaked)) {
			t.Errorf("响应体泄漏了企微内部信息 %q：%s", leaked, body)
		}
	}
}

// TestBindRequiresLogin 锁定绑定接口必须登录后才能调用。
func TestBindRequiresLogin(t *testing.T) {
	rec := postCallback(t, newTestRouter(NewHandler(newFakeClient(t, newFakeWecom(t)), nil, nil), nil),
		"/api/auth/wecom/bind", `{"code":"c1","state":"s1"}`, "s1")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录调用绑定应返回 401，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

// TestBindHappyPath 覆盖绑定闭环：把企微身份绑到当前登录账号。
func TestBindHappyPath(t *testing.T) {
	f := newFakeWecom(t)
	f.userResponse = func(int, string) string { return `{"errcode":0,"errmsg":"ok","userid":"ZengMaiKuan"}` }

	var gotUserID uint
	var gotExternalID string
	bind := func(_ context.Context, userID uint, externalID string) error {
		gotUserID, gotExternalID = userID, externalID
		return nil
	}

	loggedInAs := uint(42)
	rec := postCallback(t, newTestRouter(NewHandler(newFakeClient(t, f), nil, bind), &loggedInAs),
		"/api/auth/wecom/bind", `{"code":"c1","state":"s1"}`, "s1")

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if gotUserID != 42 || gotExternalID != "ZengMaiKuan" {
		t.Errorf("绑定参数错误：userID=%d externalID=%q", gotUserID, gotExternalID)
	}
}

// TestBindRejectsTakenIdentity 锁定「该企微已被别人绑过」的对外语义。
func TestBindRejectsTakenIdentity(t *testing.T) {
	f := newFakeWecom(t)
	f.userResponse = func(int, string) string { return `{"errcode":0,"errmsg":"ok","userid":"ZengMaiKuan"}` }

	bind := func(_ context.Context, _ uint, _ string) error {
		return user.ErrExternalIdentityExists
	}

	loggedInAs := uint(42)
	rec := postCallback(t, newTestRouter(NewHandler(newFakeClient(t, f), nil, bind), &loggedInAs),
		"/api/auth/wecom/bind", `{"code":"c1","state":"s1"}`, "s1")

	if rec.Code != http.StatusConflict {
		t.Fatalf("身份已被占用应返回 409，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

// getDirectory 向通讯录接口发一次请求。
func getDirectory(t *testing.T, router *gin.Engine, query string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/admin/org/directory"+query, nil))
	return rec
}

// TestDirectoryEndpointRequiresConfig 锁定「未配置企业微信」时的对外行为。
// 路由是**始终注册**的（见 AdminRoutes 的说明），所以必须由处理器兜住 nil client，
// 返回一句能看懂的 503，而不是让整个进程 panic 或回一个含糊的 404。
func TestDirectoryEndpointRequiresConfig(t *testing.T) {
	rec := getDirectory(t, newTestRouter(NewHandler(nil, nil, nil), nil), "")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("未配置企微应返回 503，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "企业微信") {
		t.Errorf("文案应点明与企业微信配置有关，实际 %q", rec.Body.String())
	}
}

// TestDirectoryEndpointReturnsFlatSnapshot 锁定接口契约：部门与成员都是**扁平**列表。
// 后端一旦开始返回嵌套的树，前端「组装树」的职责就被悄悄搬走了，
// 这条用例是用来钉住那次改动的。
func TestDirectoryEndpointReturnsFlatSnapshot(t *testing.T) {
	f := newFakeWecom(t)
	f.departmentResponse = func(int) string { return twoDepartmentsResponse }
	f.memberResponse = responseByDepartment(map[string]string{
		"2": memberBody(`{"userid":"dev1","name":"张伟","position":"开发","status":4,
			"main_department":2,"department":[2],"is_leader_in_dept":[0],"direct_leader":["boss"]}`),
	})

	rec := getDirectory(t, newTestRouter(NewHandler(newFakeClient(t, f), nil, nil), nil), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%s", rec.Code, rec.Body.String())
	}

	var body DirectorySnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法的 JSON: %v", err)
	}
	if len(body.Departments) != 2 || len(body.Members) != 1 {
		t.Fatalf("期望 2 部门 + 1 成员，实际 %d / %d", len(body.Departments), len(body.Members))
	}
	if body.FetchedAt.IsZero() {
		t.Error("响应必须带 fetched_at，否则前端无法说明数据是几分钟前的")
	}
	if body.FromCache {
		t.Error("首次抓取不应标记为来自缓存")
	}

	// 扁平校验：部门对象里不能出现嵌套的 children，成员也不该带部门树。
	raw := rec.Body.String()
	if strings.Contains(raw, "children") {
		t.Errorf("后端不应返回嵌套结构，实际 %s", raw)
	}
}

// TestDirectoryEndpointRefreshParam 锁定 ?refresh=1 到缓存的接线。
func TestDirectoryEndpointRefreshParam(t *testing.T) {
	f := newFakeWecom(t)
	f.departmentResponse = func(int) string { return twoDepartmentsResponse }
	h := NewHandler(newFakeClient(t, f), nil, nil)
	router := newTestRouter(h, nil)

	var first, second DirectorySnapshot
	if err := json.Unmarshal(getDirectory(t, router, "").Body.Bytes(), &first); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if err := json.Unmarshal(getDirectory(t, router, "").Body.Bytes(), &second); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if !second.FromCache {
		t.Error("不带 refresh 的第二次请求应命中缓存")
	}

	var forced DirectorySnapshot
	if err := json.Unmarshal(getDirectory(t, router, "?refresh=1").Body.Bytes(), &forced); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if forced.FromCache {
		t.Error("?refresh=1 应绕过缓存重新抓取")
	}
	if got := f.departmentsFetched(); got != 2 {
		t.Errorf("共应抓取 2 次（首次 + 强制刷新），实际 %d 次", got)
	}

	// 只认 "1"：写成 refresh=true 之类不该意外触发重抓。
	if err := json.Unmarshal(getDirectory(t, router, "?refresh=true").Body.Bytes(), &forced); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if !forced.FromCache {
		t.Error("refresh 传非 1 的值应继续走缓存")
	}
}

// TestDirectoryEndpointMapsRateLimit 锁定 45009 的对外语义：
// 这是**可重试**的临时状态，必须给 429 + 一句「稍后重试」，
// 而不是混进 500「服务器内部错误」里让人以为是程序坏了。
func TestDirectoryEndpointMapsRateLimit(t *testing.T) {
	f := newFakeWecom(t)
	f.departmentResponse = func(int) string {
		return `{"errcode":45009,"errmsg":"api freq out of limit"}`
	}

	rec := getDirectory(t, newTestRouter(NewHandler(newFakeClient(t, f), nil, nil), nil), "")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("频率超限应返回 429，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Error("429 响应应带 Retry-After，告诉调用方等多久再试")
	}
	if !strings.Contains(rec.Body.String(), "稍后重试") {
		t.Errorf("文案应提示稍后重试，实际 %q", rec.Body.String())
	}
}

// TestDirectoryEndpointHidesWecomMessage 锁定「通讯录路径也不回显企微原文」。
// 这条路径虽然只对管理员开放，但出口 IP、CorpID 这类信息没有理由出现在响应里。
func TestDirectoryEndpointHidesWecomMessage(t *testing.T) {
	const leaky = `{"errcode":60020,"errmsg":"not allow to access from your ip, hint: from ip 113.132.219.62"}`
	f := newFakeWecom(t)
	f.departmentResponse = func(int) string { return leaky }

	rec := getDirectory(t, newTestRouter(NewHandler(newFakeClient(t, f), nil, nil), nil), "")

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("企微配置类故障应返回 502（上游拒绝服务），实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leaked := range []string{"113.132.219.62", "not allow to access", "60020"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(leaked)) {
			t.Errorf("响应体泄漏了企微内部信息 %q：%s", leaked, body)
		}
	}
	if !strings.Contains(body, "可信 IP") {
		t.Errorf("502 文案点出「可信 IP」管理员才能自救，实际 %q", body)
	}
}
