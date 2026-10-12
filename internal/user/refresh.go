package user

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

// RefreshToken 是一枚刷新令牌的服务端记录，对应 refresh_tokens 表。
//
// 与访问令牌（JWT、无状态、验签即信）相反，刷新令牌是**有状态的不透明串**：
// 它的合法性只能通过查表判断。三个设计要点：
//
//  1. **只存哈希**：客户端持有原始随机串（只在签发响应中出现一次），
//     服务端保存的是它的 SHA-256 哈希——数据库泄露时拿到的值不能直接拿去刷新。
//  2. **轮转（rotation）**：每次刷新都换发新串，旧串立即置 revoked_at。
//     旧串被再次使用是一个强信号——同一枚令牌出现在两处，其中一处必为泄露——
//     此时撤销该用户的全部刷新令牌（reuse detection，见 RFC 9700）。
//  3. **可控撤销**：登出撤销单条、改密撤销全部。这正是 JWT 做不到、
//     而必须由这张表承担的能力。
type RefreshToken struct {
	// ID 主键，自增。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// UserID 所属用户 ID。
	UserID uint `gorm:"not null;index" json:"user_id"`
	// TokenHash 令牌原始串的 SHA-256 十六进制哈希（原始串永不落库）。
	TokenHash string `gorm:"size:64;not null;uniqueIndex" json:"-"`
	// ExpiresAt 过期时间。到点后即使未撤销也不再可用于刷新。
	ExpiresAt time.Time `gorm:"not null" json:"expires_at"`
	// RevokedAt 撤销时间；nil 表示仍然有效（登出 / 改密 / 轮转都会写入它）。
	RevokedAt *time.Time `json:"revoked_at"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `json:"created_at"`
}

// newRefreshTokenSecret 生成一枚新的刷新令牌：返回给客户端的原始串与落库用的哈希。
//
// 原始串必须用 crypto/rand 生成（256 位随机数后 base64url 编码）。
// 不能用 math/rand：它的序列可被预测，令牌就失去了「不可枚举」这一根本属性。
// base64url 无填充编码保证串里只有 URL 安全字符，放进 JSON 或将来放进 Cookie 都不需要转义。
func newRefreshTokenSecret() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("生成刷新令牌随机数失败: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, hashRefreshToken(raw), nil
}

// hashRefreshToken 计算刷新令牌的落库哈希。
//
// 为什么不加盐、不做慢哈希（对比密码的 bcrypt）：哈希的用途是「防数据库泄露后令牌被直接使用」，
// 而输入本身就是 256 位高熵随机串，不存在字典 / 暴力枚举的可行路径——
// 加盐迭代只会让每次刷新多烧 CPU 而不增加任何安全收益。
func hashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
