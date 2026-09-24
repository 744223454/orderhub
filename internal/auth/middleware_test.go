package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/744223454/orderhub/internal/user"
	"github.com/gin-gonic/gin"
)

// TestAuthAndRequireRole 覆盖认证中间件与角色中间件的四种典型组合。
func TestAuthAndRequireRole(t *testing.T) {
	gin.SetMode(gin.TestMode)

	opsToken, err := GenerateToken(&user.User{ID: 7, Role: user.RoleOps}, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken 应成功，实际报错: %v", err)
	}

	cases := []struct {
		name       string
		header     string
		roles      []user.Role
		wantStatus int
	}{
		{"缺少令牌", "", []user.Role{user.RoleAdmin}, http.StatusUnauthorized},
		{"伪造令牌", "Bearer not.a.real.token", []user.Role{user.RoleAdmin}, http.StatusUnauthorized},
		{"非 Bearer 方案", "Basic " + opsToken, []user.Role{user.RoleAdmin}, http.StatusUnauthorized},
		{"角色不匹配", "Bearer " + opsToken, []user.Role{user.RoleAdmin}, http.StatusForbidden},
		{"角色匹配", "Bearer " + opsToken, []user.Role{user.RoleAdmin, user.RoleOps}, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/me", Auth(testSecret), RequireRole(tc.roles...), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/me", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("期望状态码 %d，实际 %d（响应体: %s）", tc.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestAuthWritesIdentityToContext 验证认证通过后身份确实写入了请求上下文。
func TestAuthWritesIdentityToContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	token, err := GenerateToken(&user.User{ID: 99, Role: user.RoleUser}, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken 应成功，实际报错: %v", err)
	}

	router := gin.New()
	router.GET("/me", Auth(testSecret), func(c *gin.Context) {
		id, ok := user.CurrentUserID(c)
		if !ok || id != 99 {
			c.JSON(http.StatusInternalServerError, gin.H{"用户ID写入失败": id, "ok": ok})
			return
		}
		role, ok := user.CurrentRole(c)
		if !ok || role != user.RoleUser {
			c.JSON(http.StatusInternalServerError, gin.H{"角色写入失败": role, "ok": ok})
			return
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("身份应正确写入上下文，实际状态码 %d，响应体 %s", rec.Code, rec.Body.String())
	}
}
