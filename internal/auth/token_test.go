package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/744223454/orderhub/internal/user"
	"github.com/golang-jwt/jwt/v5"
)

// testSecret 仅用于测试，长度需满足 HMAC 使用要求。
const testSecret = "unit-test-secret-key-0123456789"

func TestGenerateAndParseToken(t *testing.T) {
	want := &user.User{ID: 42, Role: user.RoleOps}

	token, err := GenerateToken(want, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken 应成功，实际报错: %v", err)
	}

	claims, err := ParseToken(token, testSecret)
	if err != nil {
		t.Fatalf("ParseToken 应成功，实际报错: %v", err)
	}
	if claims.UserID != want.ID {
		t.Errorf("UserID 应为 %d，实际为 %d", want.ID, claims.UserID)
	}
	if claims.Role != want.Role {
		t.Errorf("Role 应为 %q，实际为 %q", want.Role, claims.Role)
	}
}

func TestParseTokenWithWrongSecret(t *testing.T) {
	token, err := GenerateToken(&user.User{ID: 1, Role: user.RoleUser}, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken 应成功，实际报错: %v", err)
	}

	if _, err := ParseToken(token, "another-secret-key-0123456789"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("使用错误密钥应返回 ErrInvalidToken，实际: %v", err)
	}
}

// TestParseTokenExpiredBySentinelError 锁定「用哨兵错误而非错误文案判断」这一修复。
func TestParseTokenExpiredBySentinelError(t *testing.T) {
	token, err := GenerateToken(&user.User{ID: 1, Role: user.RoleUser}, testSecret, -time.Minute)
	if err != nil {
		t.Fatalf("GenerateToken 应成功，实际报错: %v", err)
	}

	_, err = ParseToken(token, testSecret)
	if err == nil {
		t.Fatal("过期令牌应解析失败")
	}
	if !errors.Is(err, jwt.ErrTokenExpired) {
		t.Errorf("应能用 errors.Is 匹配 jwt.ErrTokenExpired，实际: %v", err)
	}
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("应同时匹配 ErrInvalidToken，实际: %v", err)
	}
}

// TestParseTokenRejectsUnexpectedAlg 锁定「显式限定签名算法」这一加固。
func TestParseTokenRejectsUnexpectedAlg(t *testing.T) {
	claims := Claims{
		UserID: 1,
		Role:   user.RoleUser,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    tokenIssuer,
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("签发 HS512 令牌失败: %v", err)
	}

	if _, err := ParseToken(token, testSecret); !errors.Is(err, jwt.ErrTokenSignatureInvalid) {
		t.Fatalf("非 HS256 签名的令牌应被拒绝，实际: %v", err)
	}
}
