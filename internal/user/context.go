package user

import "github.com/gin-gonic/gin"

// 认证中间件写入、业务处理器读取的上下文键。
// 定义在本包内，使 auth 包与 user 包共用同一组键，同时保持依赖单向（auth 依赖 user）。
const (
	// ContextUserIDKey 当前登录用户 ID 的上下文键。
	ContextUserIDKey = "auth.user_id"
	// ContextRoleKey 当前登录用户角色的上下文键。
	ContextRoleKey = "auth.role"
)

// CurrentUserID 从请求上下文读取当前登录用户的 ID。
func CurrentUserID(c *gin.Context) (uint, bool) {
	value, ok := c.Get(ContextUserIDKey)
	if !ok {
		return 0, false
	}
	id, ok := value.(uint)
	return id, ok
}

// CurrentRole 从请求上下文读取当前登录用户的角色。
func CurrentRole(c *gin.Context) (Role, bool) {
	value, ok := c.Get(ContextRoleKey)
	if !ok {
		return "", false
	}
	role, ok := value.(Role)
	return role, ok
}
