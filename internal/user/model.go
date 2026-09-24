package user

import "time"

// Role 表示用户角色。
type Role string

const (
	// RoleAdmin 管理员：可管理订单与用户。
	RoleAdmin Role = "admin"
	// RoleUser 普通用户：可下单并查看自己的订单。
	RoleUser Role = "user"
	// RoleOps 运营：可管理订单与用户。
	RoleOps Role = "ops"
)

// Valid 判断角色是否为预设的合法值之一。
func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleUser, RoleOps:
		return true
	}
	return false
}

// User 是用户账户模型，对应数据库中的 users 表。
type User struct {
	// ID 主键，自增。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// Name 登录用户名，全局唯一。
	Name string `gorm:"size:64;not null;uniqueIndex" json:"name"`
	// PasswordHash bcrypt 哈希后的密码，永不出现在接口响应中。
	PasswordHash string `gorm:"size:255;not null" json:"-"`
	// Role 用户角色。
	Role Role `gorm:"size:16;not null" json:"role"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 最后更新时间。
	UpdatedAt time.Time `json:"updated_at"`
}
