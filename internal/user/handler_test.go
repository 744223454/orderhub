package user

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 本文件覆盖「管理员建号」（POST /api/admin/users）的接口行为。
//
// 跑法：
//
//	go test ./internal/user/ -run TestHandlerAdminCreateUser -v

// newAdminTestHandler 构造用户模块的处理器。
//
// issue 桩在「被调用」时直接记失败：建号不是登录，接口不应签发令牌。
// 实现若误把建号当作登录来写（顺手复用 loginResponse 的返回形状），
// 这条信号会先于其它断言出现，省得对着响应体逐个字段排查。
func newAdminTestHandler(t *testing.T, service *Service) *Handler {
	t.Helper()
	return NewHandler(service, func(*User) (string, error) {
		t.Error("管理员建号不应签发访问令牌")
		return "", nil
	})
}

// newAdminTestRouter 构造挂了管理员路由的测试路由器。
// 刻意调用真实的 AdminRoutes 而不是手工重挂路由——挂载本身也是被测对象：
// handler 写好了但忘了在 router.go 注册，接口就是 404，一样是缺口。
func newAdminTestRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	AdminRoutes(r.Group("/api"), h)
	return r
}

// doJSON 发起一次请求并返回响应记录。
func doJSON(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// assertSingleJSONObject 断言响应体是「单个」合法 JSON 对象。
// 若某个分支漏了 return，处理会继续往下走并再次调用 c.JSON，
// 多个 JSON 对象被拼进同一个响应体——状态码仍是第一次写入的那个，
// 因此只看状态码查不出问题，必须检查响应体本身。
func assertSingleJSONObject(t *testing.T, body string) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(body))
	var first map[string]any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("响应体不是合法 JSON: %v（响应体: %s）", err, body)
	}
	var extra map[string]any
	if err := dec.Decode(&extra); err == nil {
		t.Errorf("响应体包含多段 JSON，说明有分支未 return: %s", body)
	}
}

// TestHandlerAdminCreateUser 覆盖建号的正常路径。
func TestHandlerAdminCreateUser(t *testing.T) {
	service := setupTestService(t)
	router := newAdminTestRouter(newAdminTestHandler(t, service))

	t.Run("创建普通用户并保证密码可用", func(t *testing.T) {
		// 名字前缀刻意取短：服务层限制用户名 1~16 个字符，
		// 而 uniqueSuffix 约 12 位，前缀过长会在校验上翻车——那报的是 400，不是本用例想测的东西。
		name := "ac" + uniqueSuffix()
		rec := doJSON(t, router, http.MethodPost, "/api/admin/users",
			fmt.Sprintf(`{"username":%q,"password":"pass1234","role":"user"}`, name))
		if rec.Code != http.StatusOK {
			t.Fatalf("期望 200，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}
		assertSingleJSONObject(t, rec.Body.String())

		var got adminUserView
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("响应不是 adminUserView 形状: %v（原始: %s）", err, rec.Body.String())
		}
		if got.ID == 0 || got.Name != name || got.Role != RoleUser {
			t.Errorf("响应应包含新建用户的 id/name/role，实际 %+v", got)
		}
		// 建号设了密码，管理端视图必须如实报告「有密码」；
		// 若实现误返回了 User 这种更瘦的形状，这里会先失败。
		if !got.HasPassword {
			t.Error("建号设置了密码，has_password 应为 true")
		}
		if len(got.Identities) != 0 {
			t.Errorf("新账号不应有外部身份，实际 %+v", got.Identities)
		}
		// 时间戳不能是零值：手拼视图时最容易漏掉，管理端「创建时间」列会显示公元 1 年。
		if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
			t.Errorf("created_at / updated_at 不应为零值，实际 %+v", got)
		}

		// 响应体不得出现令牌或密码相关字段——建号不是登录，也不回显密码。
		var raw map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("解析响应失败: %v", err)
		}
		for _, forbidden := range []string{"token", "password", "password_hash"} {
			if _, exists := raw[forbidden]; exists {
				t.Errorf("响应体不应包含 %q 字段", forbidden)
			}
		}
		// identities 必须序列化成 [] 而不是 null：管理端表格对它是直接 .map() 的，
		// null 会让页面崩。与订单列表「空 items 输出 []」同属一条契约。
		if v, exists := raw["identities"]; !exists || v == nil {
			t.Errorf("identities 应为 []（不能缺字段、也不能为 null），实际: %v", v)
		}

		// 最硬的一条：用刚设的密码能直接登录。
		// 它证明密码确实以「按键入的原文」落库且哈希可用——
		// 参数传错位（如把 role 传成 password）这类错误只有这一步能发现。
		if _, err := service.Login(context.Background(), name, "pass1234"); err != nil {
			t.Errorf("新建账号应能直接用该密码登录，实际: %v", err)
		}
	})

	t.Run("创建运营账号", func(t *testing.T) {
		name := "aco" + uniqueSuffix()
		rec := doJSON(t, router, http.MethodPost, "/api/admin/users",
			fmt.Sprintf(`{"username":%q,"password":"pass1234","role":"ops"}`, name))
		if rec.Code != http.StatusOK {
			t.Fatalf("期望 200，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}

		var got adminUserView
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("响应不是 adminUserView 形状: %v（原始: %s）", err, rec.Body.String())
		}
		if got.Role != RoleOps {
			t.Errorf("角色应为 %q，实际 %q", RoleOps, got.Role)
		}

		// 回读一次：响应是给前端看的，库里的值才是事实。
		stored, err := service.FindUserByID(context.Background(), got.ID)
		if err != nil {
			t.Fatalf("回读用户失败: %v", err)
		}
		if stored.Role != RoleOps {
			t.Errorf("角色应落库为 %q，实际 %q", RoleOps, stored.Role)
		}
	})
}

// TestHandlerAdminCreateUserDuplicate 锁定重复用户名 → 409。
func TestHandlerAdminCreateUserDuplicate(t *testing.T) {
	service := setupTestService(t)
	router := newAdminTestRouter(newAdminTestHandler(t, service))

	name := "acd" + uniqueSuffix()
	body := fmt.Sprintf(`{"username":%q,"password":"pass1234","role":"user"}`, name)

	first := doJSON(t, router, http.MethodPost, "/api/admin/users", body)
	if first.Code != http.StatusOK {
		t.Fatalf("首次建号应成功，实际 %d（响应体: %s）", first.Code, first.Body.String())
	}

	second := doJSON(t, router, http.MethodPost, "/api/admin/users", body)
	if second.Code != http.StatusConflict {
		t.Errorf("重复用户名应返回 409，实际 %d（响应体: %s）", second.Code, second.Body.String())
	}
	assertSingleJSONObject(t, second.Body.String())
}

// TestHandlerAdminCreateUserInvalidInput 覆盖请求体校验失败一律 400。
func TestHandlerAdminCreateUserInvalidInput(t *testing.T) {
	service := setupTestService(t)
	router := newAdminTestRouter(newAdminTestHandler(t, service))

	name := "aci" + uniqueSuffix()
	cases := []struct {
		name string
		body string
	}{
		{"空 body", ""},
		{"空对象", `{}`},
		{"缺用户名", `{"password":"pass1234","role":"user"}`},
		{"缺密码", fmt.Sprintf(`{"username":%q,"role":"user"}`, name)},
		{"缺角色", fmt.Sprintf(`{"username":%q,"password":"pass1234"}`, name)},
		{"角色非法", fmt.Sprintf(`{"username":%q,"password":"pass1234","role":"superuser"}`, name)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, router, http.MethodPost, "/api/admin/users", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("期望 400，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
			}
			assertSingleJSONObject(t, rec.Body.String())
		})
	}
}
