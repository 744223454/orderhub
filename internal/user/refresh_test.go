package user

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// 本文件覆盖「刷新令牌会话链」（批次 1 的验收靶，实现前先红、逐块转绿）：
//   - 登录响应携带 refresh_token（POST /api/login）
//   - 刷新轮转：POST /api/refresh 换发新令牌对、旧串作废
//   - 重放检测：已轮转的旧串再次使用 → 该用户全部刷新令牌被撤销（连坐）
//   - 过期 / 登出 / 改密后的刷新一律被拒
//
// 跑法：
//
//	go test ./internal/user/ -run 'TestHandlerLoginIssuesRefreshToken|TestHandlerRefreshRotation|TestServiceRotateRefreshRejectsExpired|TestHandlerRefreshInvalid|TestHandlerLogout|TestHandlerChangePasswordRevokes' -v
//
// 红 → 绿 的推进顺序（与待办清单一致）：
//  1. service.IssueRefresh + handler.Login 接线 → 登录响应出现 refresh_token；
//  2. 仓储四方法 + service.RotateRefresh + handler.Refresh → /api/refresh 走通；
//  3. service.RevokeRefresh + handler.Logout → /api/logout 走通；
//  4. ChangePassword 末尾全撤 → 改密后刷新被拒。
//
// 断言策略：绝大多数用例走真实 HTTP 链路（借真实 AuthRoutes / MeRoutes 挂载）；
// 只有「过期令牌」必须绕过接口直接造数据（同一事务内插入一条已过期的记录）。

// newRefreshTestHandler 构造处理器：issue 桩返回固定假访问令牌。
//
// 本文件的断言对象是「刷新令牌与会话撤销」，访问令牌的真伪不重要——
// 真 JWT 的签发与校验由 auth 包的测试覆盖。桩返回固定值，
// 让「刷新响应里带新 access」这类断言可以精确比对。
func newRefreshTestHandler(t *testing.T, service *Service) *Handler {
	t.Helper()
	return NewHandler(service, func(*User) (string, error) {
		return "test-access-token", nil
	})
}

// newAuthTestRouter 构造挂了公开认证路由（login / register / refresh / logout）的测试路由器。
// 刻意调用真实的 AuthRoutes：路由挂载本身也是被测对象（挂了 handler 却忘了注册路由 = 404 缺口）。
func newAuthTestRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	AuthRoutes(r.Group("/api"), h)
	return r
}

// refreshFromBody 从响应体中取出非空的 refresh_token 字段。
//
// 必须按「字段存在、类型为字符串、值非空」三步断言：字段缺失时反序列化得到的
// 是空串零值，只看最终取值会把「压根没实现」误判成「实现了但值恰好为空」。
func refreshFromBody(t *testing.T, body []byte) string {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("解析响应失败: %v（原始: %s）", err, body)
	}
	value, ok := raw["refresh_token"].(string)
	if !ok || value == "" {
		t.Fatalf("响应必须包含非空字符串字段 refresh_token，实际: %s", body)
	}
	return value
}

// loginAndGetRefresh 通过真实登录接口登录并取出刷新令牌。
func loginAndGetRefresh(t *testing.T, router *gin.Engine, name, password string) string {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, name, password)
	rec := doJSON(t, router, http.MethodPost, "/api/login", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("登录期望 200，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
	}
	return refreshFromBody(t, rec.Body.Bytes())
}

// refreshViaHTTP 发起一次刷新请求，断言状态码；成功时返回轮转后的新刷新令牌。
func refreshViaHTTP(t *testing.T, router *gin.Engine, refresh string, wantCode int) string {
	t.Helper()
	body := fmt.Sprintf(`{"refresh_token":%q}`, refresh)
	rec := doJSON(t, router, http.MethodPost, "/api/refresh", body)
	if rec.Code != wantCode {
		t.Fatalf("刷新（期望 %d）实际 %d（响应体: %s）", wantCode, rec.Code, rec.Body.String())
	}
	assertSingleJSONObject(t, rec.Body.String())
	if wantCode != http.StatusOK {
		return ""
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("解析刷新响应失败: %v（原始: %s）", err, rec.Body.String())
	}
	token, ok := raw["token"].(string)
	if !ok || token == "" {
		t.Fatalf("刷新响应必须包含非空字符串字段 token，实际: %s", rec.Body.String())
	}
	return refreshFromBody(t, rec.Body.Bytes())
}

// TestHandlerLoginIssuesRefreshToken 锁定登录响应形状：登录必须成套返回 access + refresh。
func TestHandlerLoginIssuesRefreshToken(t *testing.T) {
	service := setupTestService(t)
	router := newAuthTestRouter(newRefreshTestHandler(t, service))

	name := "lr" + uniqueSuffix()
	if _, err := service.Register(context.Background(), name, "password123", RoleUser); err != nil {
		t.Fatalf("预置用户失败: %v", err)
	}

	first := loginAndGetRefresh(t, router, name, "password123")
	second := loginAndGetRefresh(t, router, name, "password123")

	// 两次登录（模拟两台设备）必须得到不同的串——固定串或可预测实现在这里现形。
	if first == second {
		t.Error("两次登录应签发不同的刷新令牌，实际相同")
	}
}

// TestHandlerRefreshRotationAndReplayDetection 是会话链的核心用例：轮转 + 重放连坐。
func TestHandlerRefreshRotationAndReplayDetection(t *testing.T) {
	service := setupTestService(t)
	router := newAuthTestRouter(newRefreshTestHandler(t, service))

	name := "rr" + uniqueSuffix()
	if _, err := service.Register(context.Background(), name, "password123", RoleUser); err != nil {
		t.Fatalf("预置用户失败: %v", err)
	}

	raw1 := loginAndGetRefresh(t, router, name, "password123")
	rawOther := loginAndGetRefresh(t, router, name, "password123") // 第二次登录 = 另一台设备

	// 两台设备各自正常轮转：旧串作废、拿到新串。
	raw2 := refreshViaHTTP(t, router, raw1, http.StatusOK)
	if raw2 == raw1 {
		t.Fatal("轮转必须换发新串，实际返回了旧串")
	}
	rawOther2 := refreshViaHTTP(t, router, rawOther, http.StatusOK)
	if rawOther2 == rawOther {
		t.Fatal("轮转必须换发新串，实际返回了旧串")
	}

	// 重放已被轮转的 raw1：必须拒绝（一枚串出现在两处 = 泄露信号）。
	refreshViaHTTP(t, router, raw1, http.StatusUnauthorized)

	// 连坐：该用户「全部」刷新令牌被撤销——包括另一台设备还在用的新串。
	// 这是 reuse detection 的核心语义，防止攻击者与受害者在「谁先刷新」上赛跑。
	refreshViaHTTP(t, router, raw2, http.StatusUnauthorized)
	refreshViaHTTP(t, router, rawOther2, http.StatusUnauthorized)
}

// TestHandlerRefreshInvalidInput 锁定刷新的失败分支：无效串 401、缺参 400。
func TestHandlerRefreshInvalidInput(t *testing.T) {
	service := setupTestService(t)
	router := newAuthTestRouter(newRefreshTestHandler(t, service))

	t.Run("无效令牌返回 401", func(t *testing.T) {
		rec := doJSON(t, router, http.MethodPost, "/api/refresh", `{"refresh_token":"not-a-real-token"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("期望 401，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}
		assertSingleJSONObject(t, rec.Body.String())

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("解析响应失败: %v", err)
		}
		msg, _ := resp["error"].(string)
		if !strings.Contains(msg, "刷新令牌") {
			t.Errorf("错误文案应提及刷新令牌，实际: %v", resp["error"])
		}
	})

	for _, tc := range []struct {
		name string
		body string
	}{
		{"缺少字段", `{}`},
		{"空串", `{"refresh_token":""}`},
	} {
		t.Run(tc.name+"返回 400", func(t *testing.T) {
			rec := doJSON(t, router, http.MethodPost, "/api/refresh", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("期望 400，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestHandlerLogoutRevokesAndIsIdempotent 锁定登出：撤销后不能再用刷新，且重复调用幂等。
func TestHandlerLogoutRevokesAndIsIdempotent(t *testing.T) {
	service := setupTestService(t)
	router := newAuthTestRouter(newRefreshTestHandler(t, service))

	name := "lo" + uniqueSuffix()
	if _, err := service.Register(context.Background(), name, "password123", RoleUser); err != nil {
		t.Fatalf("预置用户失败: %v", err)
	}
	raw := loginAndGetRefresh(t, router, name, "password123")

	rec := doJSON(t, router, http.MethodPost, "/api/logout", fmt.Sprintf(`{"refresh_token":%q}`, raw))
	if rec.Code != http.StatusOK {
		t.Fatalf("登出期望 200，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
	}
	assertSingleJSONObject(t, rec.Body.String())

	// 撤销生效：该串不能再用于刷新。
	refreshViaHTTP(t, router, raw, http.StatusUnauthorized)

	// 幂等：同一串再次登出、以及从未存在过的串登出，都应返回 200
	// （「撤销」的目标是「以后用不了」，三种情形下目标都已达成）。
	for _, body := range []string{
		fmt.Sprintf(`{"refresh_token":%q}`, raw),
		fmt.Sprintf(`{"refresh_token":"never-existed-%s"}`, uniqueSuffix()),
	} {
		rec := doJSON(t, router, http.MethodPost, "/api/logout", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("登出应幂等返回 200，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}
		assertSingleJSONObject(t, rec.Body.String())
	}
}

// TestHandlerChangePasswordRevokesRefreshes 锁定改密的全撤语义：
// 密码变了，所有旧会话的续期能力必须立即消失。
func TestHandlerChangePasswordRevokesRefreshes(t *testing.T) {
	service := setupTestService(t)
	handler := newRefreshTestHandler(t, service)

	name := "cr" + uniqueSuffix()
	u, err := service.Register(context.Background(), name, "password123", RoleUser)
	if err != nil {
		t.Fatalf("预置用户失败: %v", err)
	}

	authRouter := newAuthTestRouter(handler)
	raw := loginAndGetRefresh(t, authRouter, name, "password123")

	// 改密走真实 MeRoutes（复用 password_test.go 的同名 helper 与桩中间件）。
	meRouter := newMeTestRouter(handler, u.ID)
	rec := doJSON(t, meRouter, http.MethodPatch, "/api/me/password",
		`{"old_password":"password123","new_password":"newpass456"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("改密期望 200，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
	}

	// 改密前签发的刷新令牌必须全部失效。
	refreshViaHTTP(t, authRouter, raw, http.StatusUnauthorized)
}

// TestServiceRotateRefreshRejectsExpired 锁定过期语义。
//
// 过期无法通过接口造出（接口签发的串总是新鲜的），故直接调用仓储接口在同一事务内
// 插入一条已过期的记录——这是唯一一个绕过 HTTP 层的用例，也是验收靶里唯一会
// 直接触碰「未实现的仓储桩」的用例，故排在文件末尾运行。
func TestServiceRotateRefreshRejectsExpired(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()

	u, err := service.Register(ctx, "rx"+uniqueSuffix(), "password123", RoleUser)
	if err != nil {
		t.Fatalf("预置用户失败: %v", err)
	}

	raw := "expired-" + uniqueSuffix()
	if err := service.repo.CreateRefreshToken(ctx, &RefreshToken{
		UserID:    u.ID,
		TokenHash: hashRefreshToken(raw),
		ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("预置过期令牌失败: %v", err)
	}

	if _, _, err := service.RotateRefresh(ctx, raw); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("过期刷新令牌应返回 ErrInvalidRefreshToken，实际: %v", err)
	}
}
