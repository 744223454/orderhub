package order

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/744223454/orderhub/internal/user"
	"github.com/gin-gonic/gin"
)

// itoa 把订单 ID 转成路径参数形式的字符串。
func itoa(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}

// extractID 从订单 JSON 响应中取出 id 字段。
func extractID(t *testing.T, body string) uint {
	t.Helper()
	var o Order
	if err := json.Unmarshal([]byte(body), &o); err != nil {
		t.Fatalf("解析响应失败: %v（响应体: %s）", err, body)
	}
	if o.ID == 0 {
		t.Fatalf("响应中没有订单 ID: %s", body)
	}
	return o.ID
}

// assertSingleJSONObject 断言响应体是「单个」合法 JSON 对象。
// 若某个错误分支漏了 return，处理会继续往下走并再次调用 c.JSON，
// 导致多个 JSON 对象被拼进同一个响应体——状态码仍是第一次写入的那个，
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

// newTestRouter 构造带模拟认证信息的路由。
// injectUser 为 false 时模拟「未经认证中间件」的场景。
func newTestRouter(h *Handler, userID uint, injectUser bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if injectUser {
			c.Set(user.ContextUserIDKey, userID)
			c.Set(user.ContextRoleKey, user.RoleUser)
		}
		c.Next()
	})

	r.POST("/orders", h.CreateOrder)
	r.GET("/orders", h.ListMyOrders)
	r.POST("/orders/:id/pay", h.PayOrder)
	r.GET("/admin/orders", h.ListAllOrders)
	r.POST("/admin/orders/:id/ship", h.ShipOrder)
	r.POST("/admin/orders/:id/complete", h.CompleteOrder)
	r.POST("/admin/orders/:id/refund", h.RefundOrder)
	return r
}

// doJSON 发起一次请求并返回响应记录。
func doJSON(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestHandlerCreateOrder 覆盖下单的正常与异常路径。
func TestHandlerCreateOrder(t *testing.T) {
	service := NewService(setupTestRepo(t))
	router := newTestRouter(NewHandler(service), 1, true)

	t.Run("正常创建", func(t *testing.T) {
		rec := doJSON(t, router, http.MethodPost, "/orders", `{"product_name":"机械键盘","amount":29900}`)
		if rec.Code != http.StatusOK {
			t.Errorf("期望 200，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}
	})

	t.Run("金额非法返回 400", func(t *testing.T) {
		rec := doJSON(t, router, http.MethodPost, "/orders", `{"product_name":"机械键盘","amount":0}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("期望 400，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}
		assertSingleJSONObject(t, rec.Body.String())
	})

	t.Run("缺少字段返回 400", func(t *testing.T) {
		rec := doJSON(t, router, http.MethodPost, "/orders", `{}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("期望 400，实际 %d", rec.Code)
		}
		assertSingleJSONObject(t, rec.Body.String())
	})
}

// TestHandlerUnauthorized 验证未认证时返回 401（而非 400）。
func TestHandlerUnauthorized(t *testing.T) {
	service := NewService(setupTestRepo(t))
	router := newTestRouter(NewHandler(service), 0, false)

	rec := doJSON(t, router, http.MethodPost, "/orders", `{"product_name":"x","amount":100}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("未认证应返回 401，实际 %d", rec.Code)
	}
}

// TestHandlerListMyOrdersEmptyReturnsArray 锁定「空列表输出 [] 而非 null」这一约定。
// 仓库层查不到数据时返回 nil 切片，直接序列化会得到 null，前端 .map() 会抛错。
func TestHandlerListMyOrdersEmptyReturnsArray(t *testing.T) {
	service := NewService(setupTestRepo(t))
	router := newTestRouter(NewHandler(service), 999, true)

	rec := doJSON(t, router, http.MethodGet, "/orders", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("空列表应序列化为 []，实际: %s", got)
	}
}

// TestHandlerListAllOrdersEmptyReturnsArray 管理端列表同样要兜底成 []。
func TestHandlerListAllOrdersEmptyReturnsArray(t *testing.T) {
	service := NewService(setupTestRepo(t))
	router := newTestRouter(NewHandler(service), 1, true)

	rec := doJSON(t, router, http.MethodGet, "/admin/orders", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) == "null" {
		t.Error("管理端空列表不应输出 null")
	}
}

// TestHandlerPayOrderReadsPathParam 验证路径参数确实被读到。
// 若 handler 读取的参数名与路由声明不一致，合法 ID 也会被判为不合法。
func TestHandlerPayOrderReadsPathParam(t *testing.T) {
	service := NewService(setupTestRepo(t))
	router := newTestRouter(NewHandler(service), 1, true)

	o := newPendingOrder(t, service, 1)

	rec := doJSON(t, router, http.MethodPost, "/orders/"+itoa(o.ID)+"/pay", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("合法订单 ID 支付应返回 200，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
	}
}

// TestHandlerPathParamInvalid 验证非法路径参数返回 400。
func TestHandlerPathParamInvalid(t *testing.T) {
	service := NewService(setupTestRepo(t))
	router := newTestRouter(NewHandler(service), 1, true)

	cases := []struct {
		name string
		path string
	}{
		{"非数字", "/orders/abc/pay"},
		{"负数", "/orders/-1/pay"},
		{"零", "/orders/0/pay"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, router, http.MethodPost, tc.path, "")
			if rec.Code != http.StatusBadRequest {
				t.Errorf("期望 400，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestHandlerErrorMapping 验证服务层错误被映射为正确的状态码。
func TestHandlerErrorMapping(t *testing.T) {
	service := NewService(setupTestRepo(t))
	router := newTestRouter(NewHandler(service), 1, true)

	t.Run("订单不存在返回 404", func(t *testing.T) {
		rec := doJSON(t, router, http.MethodPost, "/orders/99999999/pay", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("期望 404，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}
	})

	t.Run("越权支付他人订单返回 404", func(t *testing.T) {
		o := newPendingOrder(t, service, 2) // 属于用户 2
		rec := doJSON(t, router, http.MethodPost, "/orders/"+itoa(o.ID)+"/pay", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("期望 404（不暴露他人订单是否存在），实际 %d", rec.Code)
		}
	})

	t.Run("状态不允许返回 409", func(t *testing.T) {
		o := newPendingOrder(t, service, 1)
		rec := doJSON(t, router, http.MethodPost, "/admin/orders/"+itoa(o.ID)+"/ship", "")
		if rec.Code != http.StatusConflict {
			t.Errorf("待支付订单发货应返回 409，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}
	})
}

// TestHandlerFullLifecycle 走一遍下单到完成的完整链路。
func TestHandlerFullLifecycle(t *testing.T) {
	service := NewService(setupTestRepo(t))
	router := newTestRouter(NewHandler(service), 1, true)

	rec := doJSON(t, router, http.MethodPost, "/orders", `{"product_name":"链路测试","amount":500}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("下单失败: %d %s", rec.Code, rec.Body.String())
	}
	orderID := extractID(t, rec.Body.String())

	for _, step := range []struct {
		path string
		want int
	}{
		{"/orders/" + itoa(orderID) + "/pay", http.StatusOK},
		{"/admin/orders/" + itoa(orderID) + "/ship", http.StatusOK},
		{"/admin/orders/" + itoa(orderID) + "/complete", http.StatusOK},
	} {
		rec := doJSON(t, router, http.MethodPost, step.path, "")
		if rec.Code != step.want {
			t.Fatalf("%s 期望 %d，实际 %d（响应体: %s）", step.path, step.want, rec.Code, rec.Body.String())
		}
	}
}
