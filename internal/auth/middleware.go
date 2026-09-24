package auth

import (
	"net/http"
	"strings"

	"github.com/744223454/orderhub/internal/user"
	"github.com/gin-gonic/gin"
)

// bearerPrefix 是 Authorization 头要求的认证方案前缀。
const bearerPrefix = "Bearer "

// Auth 返回认证中间件：解析 Authorization 头中的 Bearer 令牌，
// 校验通过后把用户 ID 与角色写入请求上下文，失败则中断并返回 401。
func Auth(secret string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		header := ctx.GetHeader("Authorization")
		if !strings.HasPrefix(header, bearerPrefix) {
			abortUnauthorized(ctx, "缺少 Bearer 令牌")
			return
		}

		claims, err := ParseToken(strings.TrimSpace(strings.TrimPrefix(header, bearerPrefix)), secret)
		if err != nil {
			abortUnauthorized(ctx, "令牌无效或已过期")
			return
		}

		ctx.Set(user.ContextUserIDKey, claims.UserID)
		ctx.Set(user.ContextRoleKey, claims.Role)
		ctx.Next()
	}
}

// RequireRole 返回角色校验中间件：仅当上下文中的角色属于 roles 之一时放行，
// 否则返回 403；上下文中没有角色（即未经过 Auth 中间件）时返回 401。
func RequireRole(roles ...user.Role) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		role, ok := user.CurrentRole(ctx)
		if !ok {
			abortUnauthorized(ctx, "未认证")
			return
		}

		for _, allowed := range roles {
			if role == allowed {
				ctx.Next()
				return
			}
		}
		ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "权限不足"})
	}
}

// abortUnauthorized 中断请求并返回 401。
func abortUnauthorized(ctx *gin.Context, message string) {
	ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": message})
}
