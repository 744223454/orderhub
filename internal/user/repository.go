package user

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// Repo 提供用户数据的持久化访问能力。
type Repo struct {
	db *gorm.DB
}

// NewRepository 创建用户仓库，db 为已初始化并完成迁移的数据库连接。
func NewRepository(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// Create 写入一条用户记录。
// 用户名冲突返回 ErrUsernameExists；gorm 的错误在本层翻译，不再向上泄露。
func (r *Repo) Create(ctx context.Context, user *User) error {
	if err := gorm.G[User](r.db).Create(ctx, user); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrUsernameExists
		}
		return fmt.Errorf("创建用户失败: %w", err)
	}
	return nil
}

// FindByName 按用户名查询用户。
// 用户不存在时返回 ErrUserNotFound，便于调用方与数据库故障区分处理。
func (r *Repo) FindByName(ctx context.Context, name string) (*User, error) {
	user, err := gorm.G[User](r.db).
		Where("name = ?", name).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("按用户名查询用户失败: %w", err)
	}
	return &user, nil
}
