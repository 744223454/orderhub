package wecom

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeWecom 模拟企业微信服务端：记录各接口被调用的次数，并允许单个用例覆写响应。
//
// 计数是这里的关键——「token 是否被缓存」「并发是否被合并」「失效是否只重试一次」
// 这几条都没有可观察的返回值，只能通过真实请求次数来断定。
type fakeWecom struct {
	server *httptest.Server

	mu              sync.Mutex
	tokenCalls      int
	userCalls       int
	departmentCalls int
	memberCalls     int

	// tokenResponse 按调用序号返回响应体，返回空串表示用默认成功响应。
	tokenResponse func(call int) string
	// userResponse 按调用序号与本次使用的 access_token 返回响应体。
	userResponse func(call int, accessToken string) string
	// departmentResponse 按调用序号返回部门列表响应体（数组或错误对象），空串表示默认。
	departmentResponse func(call int) string
	// memberResponse 按调用序号与目标部门 id 返回成员列表响应体，空串表示默认。
	memberResponse func(call int, departmentID string) string
}

// newFakeWecom 启动一台模拟企微服务，测试结束时自动关闭。
func newFakeWecom(t *testing.T) *fakeWecom {
	t.Helper()

	f := &fakeWecom{}
	mux := http.NewServeMux()
	mux.HandleFunc(tokenPath, f.handleToken)
	mux.HandleFunc(userInfoPath, f.handleUserInfo)
	mux.HandleFunc(departmentListPath, f.handleDepartmentList)
	mux.HandleFunc(userListPath, f.handleUserList)
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)

	return f
}

func (f *fakeWecom) handleToken(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	f.tokenCalls++
	call := f.tokenCalls
	respond := f.tokenResponse
	f.mu.Unlock()

	// respond 在锁外调用：并发用例里它可能刻意阻塞，握着锁会卡住计数逻辑。
	body := `{"errcode":0,"errmsg":"ok","access_token":"token-1","expires_in":7200}`
	if respond != nil {
		if custom := respond(call); custom != "" {
			body = custom
		}
	}
	writeJSONBody(w, body)
}

func (f *fakeWecom) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.userCalls++
	call := f.userCalls
	respond := f.userResponse
	f.mu.Unlock()

	body := `{"errcode":0,"errmsg":"ok","userid":"ZengMaiKuan"}`
	if respond != nil {
		if custom := respond(call, r.URL.Query().Get("access_token")); custom != "" {
			body = custom
		}
	}
	writeJSONBody(w, body)
}

func (f *fakeWecom) tokensFetched() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenCalls
}

func (f *fakeWecom) userInfoFetched() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.userCalls
}

// handleDepartmentList 模拟 department/list：与真实接口一致，返回**对象**
// （`{"errcode":0,...,"department":[...]}`），而不是裸数组。
func (f *fakeWecom) handleDepartmentList(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	f.departmentCalls++
	call := f.departmentCalls
	respond := f.departmentResponse
	f.mu.Unlock()

	body := `{"errcode":0,"errmsg":"ok","department":[{"id":1,"name":"根部门","parentid":0,"order":100000000,"department_leader":["boss"]}]}`
	if respond != nil {
		if custom := respond(call); custom != "" {
			body = custom
		}
	}
	writeJSONBody(w, body)
}

// handleUserList 模拟 user/list：返回指定部门的成员。
func (f *fakeWecom) handleUserList(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.memberCalls++
	call := f.memberCalls
	respond := f.memberResponse
	f.mu.Unlock()

	body := `{"errcode":0,"errmsg":"ok","userlist":[]}`
	if respond != nil {
		if custom := respond(call, r.URL.Query().Get("department_id")); custom != "" {
			body = custom
		}
	}
	writeJSONBody(w, body)
}

func (f *fakeWecom) departmentsFetched() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.departmentCalls
}

func (f *fakeWecom) membersFetched() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.memberCalls
}

func writeJSONBody(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

// newFakeClient 构造一个指向模拟服务的客户端。
func newFakeClient(t *testing.T, f *fakeWecom) *Client {
	t.Helper()
	return NewClient(Config{
		CorpID:      "ww-test",
		AgentID:     "1000002",
		Secret:      "test-secret-value",
		WebBaseURL:  "http://dev.orderhub.local:3000",
		OpenAPIBase: f.server.URL,
	})
}

// TestLoginURLCarriesEncodedCallback 锁定扫码登录链接的构造：
// 回调地址必须做 URL 编码后拼进查询串（它自身含 :// 与 /），参数名与取值也要与企微文档一致。
// 这条最值得自动化——少编码一层、或把域名拼错，本地表现为企微直接拒绝，排查成本很高。
func TestLoginURLCarriesEncodedCallback(t *testing.T) {
	client := NewClient(Config{
		CorpID:     "ww-test",
		AgentID:    "1000002",
		Secret:     "s",
		WebBaseURL: "http://dev.orderhub.local:3000/",
	})

	cases := []struct {
		name       string
		intent     Intent
		redirectTo string
	}{
		{"登录", IntentLogin, "http://dev.orderhub.local:3000/login/wecom/callback"},
		{"绑定", IntentBind, "http://dev.orderhub.local:3000/login/wecom/bind"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := client.LoginURL("state-123", tc.intent)

			// 用 url.Parse + Query() 反解，等同于验证「编码能被正确还原」。
			// 若存在双重编码，redirect_uri 会多出一层 %25，这里会立刻失败。
			parsed, err := url.Parse(raw)
			if err != nil {
				t.Fatalf("登录链接无法解析: %v", err)
			}
			if parsed.Host != "login.work.weixin.qq.com" {
				t.Errorf("host 期望 login.work.weixin.qq.com，实际 %q", parsed.Host)
			}
			if parsed.Path != loginPath {
				t.Errorf("path 期望 %q，实际 %q", loginPath, parsed.Path)
			}

			query := parsed.Query()
			want := map[string]string{
				"login_type":   loginTypeCorpApp,
				"appid":        "ww-test",
				"agentid":      "1000002",
				"redirect_uri": tc.redirectTo,
				"state":        "state-123",
			}
			for key, expected := range want {
				if got := query.Get(key); got != expected {
					t.Errorf("参数 %s 期望 %q，实际 %q", key, expected, got)
				}
			}
		})
	}
}

// TestConfigMissing 锁定「全缺才算未启用」的判断依据。
func TestConfigMissing(t *testing.T) {
	if got := len(Config{}.Missing()); got != 4 {
		t.Fatalf("配置字段数期望 4，实际 %d；新增字段时请同步更新本用例", got)
	}
	missing := Config{CorpID: "ww", AgentID: "1", Secret: "s"}.Missing()
	if len(missing) != 1 || missing[0] != "WebBaseURL" {
		t.Errorf("期望只缺 WebBaseURL，实际 %v", missing)
	}
}

// TestParseIntent 锁定「非法用途回落到登录」，避免前端传错值导致绑定流程被静默当成登录。
func TestParseIntent(t *testing.T) {
	if got := ParseIntent("bind"); got != IntentBind {
		t.Errorf("bind 期望解析为 IntentBind，实际 %q", got)
	}
	for _, raw := range []string{"", "login", "BIND", "drop table"} {
		if got := ParseIntent(raw); got != IntentLogin {
			t.Errorf("%q 期望回落到 IntentLogin，实际 %q", raw, got)
		}
	}
}

// TestExchangeCodeReusesCachedToken 锁定 access_token 的缓存：
// 两次换票只应取一次凭证。企微会限制 gettoken 的调用频率，每次都取迟早被拦。
func TestExchangeCodeReusesCachedToken(t *testing.T) {
	f := newFakeWecom(t)
	client := newFakeClient(t, f)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		identity, err := client.ExchangeCode(ctx, "code-"+string(rune('a'+i)))
		if err != nil {
			t.Fatalf("第 %d 次换票失败: %v", i+1, err)
		}
		if identity.UserID != "ZengMaiKuan" {
			t.Fatalf("期望拿到 userid，实际 %+v", identity)
		}
	}

	if got := f.tokensFetched(); got != 1 {
		t.Errorf("3 次换票应只取 1 次 access_token，实际取了 %d 次", got)
	}
	if got := f.userInfoFetched(); got != 3 {
		t.Errorf("期望调用 3 次身份接口，实际 %d 次", got)
	}
}

// TestAccessTokenCoalescesConcurrentCalls 锁定并发合并：
// 100 个并发调用只应触发 1 次 gettoken。
//
// 做法是让首个 gettoken 请求挂在服务端不返回，使其余调用全部堆在 singleflight 上，
// 再放行。这样无论它们何时到达，都只会有一次真实请求。
func TestAccessTokenCoalescesConcurrentCalls(t *testing.T) {
	f := newFakeWecom(t)
	client := newFakeClient(t, f)

	arrived := make(chan struct{})
	gate := make(chan struct{})
	var once sync.Once
	f.tokenResponse = func(int) string {
		once.Do(func() { close(arrived) })
		<-gate
		return ""
	}

	const workers = 100
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := client.ExchangeCode(context.Background(), "code"); err != nil {
				t.Errorf("并发换票失败: %v", err)
			}
		}()
	}

	<-arrived
	close(gate)
	wg.Wait()

	if got := f.tokensFetched(); got != 1 {
		t.Errorf("100 个并发调用应只取 1 次 access_token，实际 %d 次", got)
	}
}

// TestAccessTokenIgnoresCallerCancellation 锁定练习一修的那个缺陷：
// 首个请求的ctx 被取消**不得**影响等在同一个 singleflight 上的其他请求。
//
// 这条用例会真的抓得住缺陷，原因在于它卡住了服务端让首个 gettoken 挂起：
//
//  1. 请求 A 用一个**可取消**的 ctx 进来，它是首个进入 fetchToken 的；
//  2. 服务端挂起，A 停在 singleflight 里，此时 group 里只有它一份；
//  3. 取消 A 的 ctx —— 这是「客户端关掉页面」对应的动作；
//  4. 请求 B 用正常的 ctx 进来，它会挂在同一个 key 上；
//  5. 放行服务端。
//
// 修复前：fetchToken 捕获的是 A 的 ctx，第 3 步取消会直接打断在途的 HTTP 请求，
// 于是 A 与 B 一起拿到 context canceled，而 B 的 ctx 其实好端端的。
// 修复后：闭包内用 WithoutCancel 派生，B 能正常拿到 token。
//
// ⚠️ **只断言 B 成功，不断言 A 成功** —— 这不是放宽标准，而是分清了两类请求：
//
//	gettoken是**共享**的（singleflight 合并后所有调用方共用同一份结果），
//	  所以它绝不能被任一调用方的生命周期绑架 ⇒ 必须脱离 ctx。B 验证的就是这条。
//	getuserinfo 是**每个调用方独占**的，A 的客户端既然已经走了，
//	  就没有任何理由再去取它的身份信息，让它失败反而是正确行为
//	  （省掉一次注定没人要的HTTP 请求）。
//
// 换句话说：修复的边界正好是「共享的那一段」。
func TestAccessTokenIgnoresCallerCancellation(t *testing.T) {
	f := newFakeWecom(t)
	client := newFakeClient(t, f)

	arrived := make(chan struct{})
	gate := make(chan struct{})
	var once sync.Once
	f.tokenResponse = func(int) string {
		once.Do(func() { close(arrived) })
		<-gate
		return ""
	}

	// A：首个进入者，用可取消的 ctx。
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()

	go func() {
		// A 的结果不在此断言，理由见函数注释。它失败（甚至成功）都不影响本用例的结论。
		_, _ = client.ExchangeCode(ctxA, "code-A")
	}()

	<-arrived // A 已经进到 fetchToken 里了

	// 关键动作：A 的客户端断开。这必须**不影响** B。
	cancelA()

	// B：在同一个 singleflight key 上排队，用一个完全正常的 ctx。
	type result struct {
		identity Identity
		err      error
	}
	resB := make(chan result, 1)
	go func() {
		identity, err := client.ExchangeCode(context.Background(), "code-B")
		resB <- result{identity, err}
	}()

	close(gate) // 放行服务端，两个 goroutine 一起收尾

	got := <-resB
	if got.err != nil {
		t.Fatalf("A 被取消不应拖累 B（B 有自己活着的 ctx），实际: %v", got.err)
	}
	if got.identity.UserID != "ZengMaiKuan" {
		t.Errorf("B 应正常拿到身份，实际: %+v", got.identity)
	}
	// 两次换票共用同一份 gettoken 结果，不应因为 A 的取消而多打一次。
	if n := f.tokensFetched(); n != 1 {
		t.Errorf("A 与 B 应共用 1 次 gettoken，实际 %d 次", n)
	}
}

// TestInvalidTokenRefreshesAndRetries 锁定「凭证失效 → 刷新后重试一次」：
// 首次身份接口返回 42001（token 超时），客户端应清缓存重取并成功。
func TestInvalidTokenRefreshesAndRetries(t *testing.T) {
	f := newFakeWecom(t)
	client := newFakeClient(t, f)
	f.userResponse = func(call int, _ string) string {
		if call == 1 {
			return `{"errcode":42001,"errmsg":"access_token expired"}`
		}
		return `{"errcode":0,"errmsg":"ok","userid":"ZengMaiKuan"}`
	}

	identity, err := client.ExchangeCode(context.Background(), "code")
	if err != nil {
		t.Fatalf("刷新凭证后应换票成功，实际报错: %v", err)
	}
	if identity.UserID != "ZengMaiKuan" {
		t.Errorf("期望拿到 userid，实际 %+v", identity)
	}
	if got := f.tokensFetched(); got != 2 {
		t.Errorf("应取 2 次 access_token（首次 + 失效后重取），实际 %d 次", got)
	}
	if got := f.userInfoFetched(); got != 2 {
		t.Errorf("应调用 2 次身份接口（首次 + 重试），实际 %d 次", got)
	}
}

// TestInvalidTokenRetriesOnlyOnce 锁定重试上限：
// 凭证持续失效时最多重试一次，绝不能陷入死循环——配置写错（如 Secret 不对）时
// 无上限重试会持续消耗 gettoken 配额。
func TestInvalidTokenRetriesOnlyOnce(t *testing.T) {
	f := newFakeWecom(t)
	client := newFakeClient(t, f)
	f.userResponse = func(int, string) string {
		return `{"errcode":42001,"errmsg":"access_token expired"}`
	}

	_, err := client.ExchangeCode(context.Background(), "code")
	if err == nil {
		t.Fatal("凭证持续失效时应返回错误")
	}
	if got := f.userInfoFetched(); got != 2 {
		t.Errorf("最多重试一次，身份接口应被调用 2 次，实际 %d 次", got)
	}
}

// TestExchangeCodeTranslatesErrCodes 锁定 errcode 到哨兵的翻译。
func TestExchangeCodeTranslatesErrCodes(t *testing.T) {
	cases := []struct {
		name     string
		response string
		wantErr  error
	}{
		{"授权码已使用或超时", `{"errcode":40029,"errmsg":"invalid code"}`, ErrInvalidAuthCode},
		{"调用来源 IP 不在可信 IP 列表", `{"errcode":60020,"errmsg":"not allow to access from your ip"}`, ErrCorpConfig},
		{"回调地址未登记可信域名", `{"errcode":50001,"errmsg":"redirect_uri unauthorized"}`, ErrCorpConfig},
		// 下面两条是从「配置异常」里拆出来的：频率超限是可重试的临时状态，
		// 无权限是可见范围没配。混成一类会把排查方向直接带偏。
		{"接口调用超过限制", `{"errcode":45009,"errmsg":"api freq out of limit"}`, ErrRateLimited},
		{"无权读取该成员或部门", `{"errcode":60011,"errmsg":"no privilege to access/modify contact/party/agent"}`, ErrNoPermission},
		{"未识别的错误码归为技术错误", `{"errcode":12345,"errmsg":"unknown"}`, ErrWecomUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeWecom(t)
			client := newFakeClient(t, f)
			f.userResponse = func(int, string) string { return tc.response }

			_, err := client.ExchangeCode(context.Background(), "code")
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("期望哨兵 %v，实际 %v", tc.wantErr, err)
			}
		})
	}
}

// TestExchangeCodeRejectsEmptyCode 空授权码不必打接口，直接判定无效。
func TestExchangeCodeRejectsEmptyCode(t *testing.T) {
	f := newFakeWecom(t)
	client := newFakeClient(t, f)

	if _, err := client.ExchangeCode(context.Background(), ""); !errors.Is(err, ErrInvalidAuthCode) {
		t.Errorf("空授权码应返回 ErrInvalidAuthCode，实际 %v", err)
	}
	if got := f.userInfoFetched(); got != 0 {
		t.Errorf("空授权码不应发起请求，实际调用 %d 次", got)
	}
}

// TestNonCorpMemberReturnsOpenID 锁定一个很容易踩的接口语义：
// 非企业成员授权时企微**同样返回 errcode=0**，只是把 userid 换成了 openid。
// 调用方必须靠 IsCorpMember 判断，绝不能只看错误码。
func TestNonCorpMemberReturnsOpenID(t *testing.T) {
	f := newFakeWecom(t)
	client := newFakeClient(t, f)
	f.userResponse = func(int, string) string {
		return `{"errcode":0,"errmsg":"ok","openid":"o-abc","external_userid":"wm-abc"}`
	}

	identity, err := client.ExchangeCode(context.Background(), "code")
	if err != nil {
		t.Fatalf("非企业成员不应报错（errcode 仍为 0），实际: %v", err)
	}
	if identity.IsCorpMember() {
		t.Errorf("只拿到 openid 时必须判为非企业成员，实际 Identity=%+v", identity)
	}
	if identity.OpenID != "o-abc" {
		t.Errorf("openid 期望 o-abc，实际 %q", identity.OpenID)
	}
}

// TestSecretNeverLeaksIntoError 锁定凭证脱敏。
// gettoken 的查询串里带着 corpsecret，而 http.Client.Do 的 *url.Error 会把完整 URL
// 写进错误文案——一旦进了日志就等于泄漏密钥，因此这里专门造一次传输层失败来验证。
func TestSecretNeverLeaksIntoError(t *testing.T) {
	const secret = "super-secret-corp-key"

	// 起一台服务再立刻关掉，得到一个必然连不上的地址。
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	client := NewClient(Config{
		CorpID:      "ww-test",
		AgentID:     "1",
		Secret:      secret,
		WebBaseURL:  "http://dev.orderhub.local:3000",
		OpenAPIBase: deadURL,
	})

	_, err := client.ExchangeCode(context.Background(), "code")
	if err == nil {
		t.Fatal("服务不可达时应返回错误")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("错误文案泄漏了 corpsecret: %v", err)
	}
}
