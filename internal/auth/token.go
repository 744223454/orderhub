package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/744223454/orderhub/internal/user"
	"github.com/golang-jwt/jwt/v5"
)

// tokenIssuer 是签发者标识。签发与校验共用同一常量，
// 避免两处分别写死后校验必然失败。
const tokenIssuer = "my-modular-app"

// ErrInvalidToken 表示令牌未通过校验（格式错误、签名无效或已过期等）。
var ErrInvalidToken = errors.New("无效的令牌")

// GenerateToken 使用 HS256 为指定用户签发访问令牌。
func GenerateToken(u *user.User, secret string, expire time.Duration) (string, error) {
	claims := Claims{
		UserID: u.ID,
		Role:   u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expire)),
			Issuer:    tokenIssuer,
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("签发令牌失败: %w", err)
	}
	return signed, nil
}

// ParseToken 解析并校验 JWT，校验项包括签名算法、签名有效性、签发者与过期时间。
//
// 返回的错误同时包装了 ErrInvalidToken 与 jwt 包自身的哨兵错误，
// 调用方可用 errors.Is(err, jwt.ErrTokenExpired) 判断具体失败原因，
// 不要依赖错误文案做字符串比较。
func ParseToken(tokenString, secret string) (*Claims, error) {
	claims := new(Claims)
	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(_ *jwt.Token) (any, error) {
			return []byte(secret), nil
		},
		// 显式限定签名算法，防止 alg 混淆类攻击。
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}
