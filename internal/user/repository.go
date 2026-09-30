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

// FindByID 按主键查询用户。
// 用户不存在时返回 ErrUserNotFound。
func (r *Repo) FindByID(ctx context.Context, id uint) (*User, error) {
	user, err := gorm.G[User](r.db).
		Where("id = ?", id).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("按 ID 查询用户失败: %w", err)
	}
	return &user, nil
}

// FindIdentity 按提供方与外部标识查询绑定关系。
// 未绑定时返回 ErrIdentityNotFound，便于调用方与数据库故障区分处理。
func (r *Repo) FindIdentity(ctx context.Context, provider IdentityProvider, externalID string) (*UserIdentity, error) {
	identity, err := gorm.G[UserIdentity](r.db).
		Where("provider = ?", provider).
		Where("external_id = ?", externalID).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrIdentityNotFound
		}
		return nil, fmt.Errorf("按外部身份查询绑定关系失败: %w", err)
	}
	return &identity, nil
}

// CreateIdentity 写入一条绑定关系。
// 该外部身份已被占用时返回 ErrExternalIdentityExists。
func (r *Repo) CreateIdentity(ctx context.Context, identity *UserIdentity) error {
	if err := gorm.G[UserIdentity](r.db).Create(ctx, identity); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrExternalIdentityExists
		}
		return fmt.Errorf("绑定外部身份失败: %w", err)
	}
	return nil
}

// CreateWithIdentity 在同一个事务里创建用户并绑定外部身份。
//
// 两步必须同事务：否则中途失败会留下一个没有任何登录方式的孤儿账号。
// 两处写入各自的唯一约束冲突被分别翻译成 ErrUsernameExists 与 ErrExternalIdentityExists，
// 调用方无需解析数据库错误，就能区分「用户名撞了」还是「该外部身份已被占用」——
// 前者应换一个候选用户名重试，后者说明并发下已有请求建好了号。
func (r *Repo) CreateWithIdentity(ctx context.Context, user *User, identity *UserIdentity) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := gorm.G[User](tx).Create(ctx, user); err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrUsernameExists
			}
			return fmt.Errorf("创建用户失败: %w", err)
		}
		// 用户已落库并回填了自增主键，绑定关系据此关联。
		identity.UserID = user.ID
		if err := gorm.G[UserIdentity](tx).Create(ctx, identity); err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrExternalIdentityExists
			}
			return fmt.Errorf("绑定外部身份失败: %w", err)
		}
		return nil
	})
}
