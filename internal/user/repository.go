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
func (r *Repo) Create(ctx context.Context, user *User) error {
	err := gorm.G[User](r.db).Create(ctx, user)
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return errors.New("用户名已存在")
		}
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

// FindByName 按用户名查询用户。
func (r *Repo) FindByName(ctx context.Context, name string) (*User, error) {
	user, err := gorm.G[User](r.db).
		Where("name = ?", name).
		First(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to find user by name: %w", err)
	}
	return &user, nil
}
