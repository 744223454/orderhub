package auth

import (
	"github.com/744223454/orderhub/internal/user"
	"github.com/golang-jwt/jwt/v5"
)

// Claims 是访问令牌携带的自定义声明。
type Claims struct {
	// UserID 令牌所属用户的主键。
	UserID uint `json:"uid"`
	// Role 令牌签发时该用户的角色，用于免查库的权限判断。
	Role user.Role `json:"role"`
	jwt.RegisteredClaims
}
