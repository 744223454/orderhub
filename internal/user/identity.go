package user

import "time"

// IdentityProvider 表示外部身份提供方。
//
// 定义在本包而不是各适配器包，是因为「用户身份来自哪里」属于用户域的领域概念：
// 适配器（如 internal/wecom）只负责实现并引用它，从而保证依赖方向始终单向
// （适配器 → user），user 包不反向依赖任何提供方。
type IdentityProvider string

const (
	// ProviderWecom 企业微信。
	ProviderWecom IdentityProvider = "wecom"
)

// Valid 判断提供方是否为预设的合法值之一。
func (p IdentityProvider) Valid() bool {
	switch p {
	case ProviderWecom:
		return true
	}
	return false
}

// UserIdentity 是「本地用户 ↔ 外部身份」的绑定关系，对应 user_identities 表。
//
// 独立成表、而不是在 users 上加一个 wecom_userid 字段，是为了后续接入微信、飞书等
// 提供方时无需改 users 表结构：新增一个提供方只是多一种 provider 取值、多一行记录。
//
// 扩展提示：复合唯一键目前是 (provider, external_id)。将来若同一个提供方要对接多个
// 租户（如多个企业微信 CorpID），userid 只在单个租户内唯一，届时需要把租户标识
// 并入唯一键，即 (provider, tenant_id, external_id)。
type UserIdentity struct {
	// ID 主键，自增。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// UserID 关联的本地用户 ID。
	UserID uint `gorm:"not null;index" json:"user_id"`
	// Provider 外部身份提供方，取值见 IdentityProvider。
	Provider IdentityProvider `gorm:"size:32;not null;uniqueIndex:uk_identity_provider_external,priority:1" json:"provider"`
	// ExternalID 提供方侧的稳定标识（企业微信为成员 userid）。
	// 与 Provider 共同构成复合唯一键：同一提供方下的一个外部账号只能绑定一个本地账号。
	ExternalID string `gorm:"size:128;not null;uniqueIndex:uk_identity_provider_external,priority:2" json:"external_id"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 最后更新时间。
	UpdatedAt time.Time `json:"updated_at"`
}
