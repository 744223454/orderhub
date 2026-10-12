package user

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 本文件覆盖「修改密码」（PATCH /api/me/password）与 GET /api/me 的 has_password 字段。
//
// 跑法：
//
//	go test ./internal/user/ -run 'TestHandlerChangePassword|TestHandlerMeReportsHasPassword' -v

// newPasswordTestHandler 构造用户模块的处理器。
//
// issue 桩在「被调用」时直接记失败：修改密码不是登录，接口不应签发令牌。
func newPasswordTestHandler(t *testing.T, service *Service) *Handler {
	t.Helper()
	return NewHandler(service, func(*User) (string, error) {
		t.Error("修改密码不应签发访问令牌")
		return "", nil
	})
}

// withUserID 返回桩中间件：把登录用户写进请求上下文，模拟 auth.Auth 在真实链路里的效果。
//
// 本包是包内测试（package user），不能 import internal/auth —— 依赖方向是 auth → user，
// 反向引用会构成循环。真正「缺令牌 / 令牌无效 → 401」由 auth 包的中间件测试覆盖。
func withUserID(id uint) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ContextUserIDKey, id)
		c.Set(ContextRoleKey, RoleUser)
		c.Next()
	}
}

// newMeTestRouter 构造挂了自助路由（/me、/me/password）的测试路由器。
// 刻意调用真实的 MeRoutes：handler 写好了但忘了注册路由，接口就是 404，一样是缺口。
func newMeTestRouter(h *Handler, userID uint) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	MeRoutes(r.Group("/api", withUserID(userID)), h)
	return r
}

// assertMeHasPassword 断言 /me 响应里有布尔类型的 has_password 字段且取值正确。
//
// 不能只反序列化到 bool 就断言：字段缺失时零值同样是 false，
// 「压根没实现」会被误判成「无密码」——必须先确认字段确实存在。
func assertMeHasPassword(t *testing.T, body []byte, want bool) {
	t.Helper()

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("解析 /me 响应失败: %v（原始: %s）", err, body)
	}
	got, ok := raw["has_password"].(bool)
	if !ok {
		t.Fatalf("/me 响应必须包含布尔字段 has_password（设置页据此渲染表单），实际: %s", body)
	}
	if got != want {
		t.Errorf("has_password 应为 %v，实际 %v", want, got)
	}
}

// TestHandlerChangePassword 覆盖正常路径：有密码账号凭原密码修改、无密码账号直接设置。
func TestHandlerChangePassword(t *testing.T) {
	service := setupTestService(t)
	handler := newPasswordTestHandler(t, service)

	t.Run("有密码账号凭原密码修改", func(t *testing.T) {
		name := "cp" + uniqueSuffix()
		u, err := service.Register(context.Background(), name, "oldpass123", RoleUser)
		if err != nil {
			t.Fatalf("预置用户失败: %v", err)
		}
		router := newMeTestRouter(handler, u.ID)

		rec := doJSON(t, router, http.MethodPatch, "/api/me/password",
			`{"old_password":"oldpass123","new_password":"newpass456"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("期望 200，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}
		assertSingleJSONObject(t, rec.Body.String())

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("解析响应失败: %v", err)
		}
		if resp["message"] != "密码已修改" {
			t.Errorf("成功响应应为 message=密码已修改，实际: %s", rec.Body.String())
		}

		// 最硬的两条：新密码能登录、旧密码不能 —— 密码确实换了、而且没有换错。
		if _, err := service.Login(context.Background(), name, "newpass456"); err != nil {
			t.Errorf("改密后应能用新密码登录，实际: %v", err)
		}
		if _, err := service.Login(context.Background(), name, "oldpass123"); err == nil {
			t.Error("改密后旧密码不应还能登录")
		}
	})

	t.Run("无密码账号直接设置且不破坏企微绑定", func(t *testing.T) {
		externalID := "cpwx" + uniqueSuffix()
		u, err := service.LoginByExternal(context.Background(), ProviderWecom, externalID)
		if err != nil {
			t.Fatalf("外部身份建号失败: %v", err)
		}
		router := newMeTestRouter(handler, u.ID)

		// 不带 old_password —— 无密码账号没有原密码可验。
		rec := doJSON(t, router, http.MethodPatch, "/api/me/password", `{"new_password":"newpass456"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("期望 200，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}

		if _, err := service.Login(context.Background(), u.Name, "newpass456"); err != nil {
			t.Errorf("设置密码后应能用密码登录，实际: %v", err)
		}
		// 企微身份仍指向同一账号：扫码登录仍回到它，而不是另建新号。
		again, err := service.LoginByExternal(context.Background(), ProviderWecom, externalID)
		if err != nil {
			t.Fatalf("企微登录失败: %v", err)
		}
		if again.ID != u.ID {
			t.Errorf("企微登录应回到同一账号（id=%d），实际 id=%d", u.ID, again.ID)
		}
	})

	t.Run("未注入登录上下文应 401", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		bare := gin.New()
		MeRoutes(bare.Group("/api"), handler) // 不挂桩中间件：模拟路由被误挂到认证层之外
		rec := doJSON(t, bare, http.MethodPatch, "/api/me/password", `{"new_password":"newpass456"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("缺少登录上下文应返回 401，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}
	})
}

// TestHandlerChangePasswordWrongOldPassword 锁定原密码错误 / 未提供 → 400，且密码保持不变。
func TestHandlerChangePasswordWrongOldPassword(t *testing.T) {
	service := setupTestService(t)
	handler := newPasswordTestHandler(t, service)

	name := "cpw" + uniqueSuffix()
	u, err := service.Register(context.Background(), name, "oldpass123", RoleUser)
	if err != nil {
		t.Fatalf("预置用户失败: %v", err)
	}
	router := newMeTestRouter(handler, u.ID)

	cases := []struct {
		name string
		body string
	}{
		{"原密码错误", `{"old_password":"wrongpass1","new_password":"newpass456"}`},
		{"缺少原密码", `{"new_password":"newpass456"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, router, http.MethodPatch, "/api/me/password", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("期望 400，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
			}
			assertSingleJSONObject(t, rec.Body.String())

			var resp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("解析响应失败: %v", err)
			}
			if resp["error"] != "原密码不正确" {
				t.Errorf("错误文案应为「原密码不正确」，实际: %v", resp["error"])
			}
		})
	}

	// 两次被拒之后，密码必须原样：旧密码仍可登录、新密码不可。
	if _, err := service.Login(context.Background(), name, "oldpass123"); err != nil {
		t.Errorf("被拒后旧密码应仍然有效，实际: %v", err)
	}
	if _, err := service.Login(context.Background(), name, "newpass456"); err == nil {
		t.Error("被拒后新密码不应已经生效")
	}
}

// TestHandlerChangePasswordInvalidNewPassword 锁定新密码校验：长度非法与空值一律 400。
//
// 「无密码账号同样校验长度」单独列一条：跳过原密码校验时，
// 最容易顺手把长度校验也一起跳掉。
func TestHandlerChangePasswordInvalidNewPassword(t *testing.T) {
	service := setupTestService(t)
	handler := newPasswordTestHandler(t, service)

	name := "cpi" + uniqueSuffix()
	u, err := service.Register(context.Background(), name, "oldpass123", RoleUser)
	if err != nil {
		t.Fatalf("预置用户失败: %v", err)
	}
	router := newMeTestRouter(handler, u.ID)

	cases := []struct {
		name         string
		body         string
		wantContains string
	}{
		{"新密码过短", `{"old_password":"oldpass123","new_password":"1234567"}`, "密码长度不合法"},
		{"新密码过长", `{"old_password":"oldpass123","new_password":"12345678901234567"}`, "密码长度不合法"},
		{"新密码为空", `{"old_password":"oldpass123","new_password":""}`, "请求参数不合法"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, router, http.MethodPatch, "/api/me/password", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("期望 400，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
			}
			assertSingleJSONObject(t, rec.Body.String())

			var resp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("解析响应失败: %v", err)
			}
			msg, _ := resp["error"].(string)
			if !strings.Contains(msg, tc.wantContains) {
				t.Errorf("错误文案应包含 %q，实际: %v", tc.wantContains, resp["error"])
			}
		})
	}

	t.Run("无密码账号同样校验长度", func(t *testing.T) {
		externalID := "cpiwx" + uniqueSuffix()
		u, err := service.LoginByExternal(context.Background(), ProviderWecom, externalID)
		if err != nil {
			t.Fatalf("外部身份建号失败: %v", err)
		}
		router := newMeTestRouter(handler, u.ID)

		rec := doJSON(t, router, http.MethodPatch, "/api/me/password", `{"new_password":"1234567"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("期望 400，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}
	})
}

// TestHandlerMeReportsHasPassword 锁定 /me 的 has_password 字段（false → true 的完整转变）。
func TestHandlerMeReportsHasPassword(t *testing.T) {
	service := setupTestService(t)
	handler := newPasswordTestHandler(t, service)

	externalID := "mewx" + uniqueSuffix()
	u, err := service.LoginByExternal(context.Background(), ProviderWecom, externalID)
	if err != nil {
		t.Fatalf("外部身份建号失败: %v", err)
	}
	router := newMeTestRouter(handler, u.ID)

	rec := doJSON(t, router, http.MethodGet, "/api/me", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
	}
	assertMeHasPassword(t, rec.Body.Bytes(), false)

	// 设置密码后同一接口应变为 true —— 设置页据此把表单从「设置」切成「修改」。
	setRec := doJSON(t, router, http.MethodPatch, "/api/me/password", `{"new_password":"newpass456"}`)
	if setRec.Code != http.StatusOK {
		t.Fatalf("设置密码期望 200，实际 %d（响应体: %s）", setRec.Code, setRec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/api/me", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
	}
	assertMeHasPassword(t, rec.Body.Bytes(), true)
}
