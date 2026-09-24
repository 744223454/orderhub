package order

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// Repo 提供订单数据的持久化访问能力。
type Repo struct {
	db *gorm.DB
}

// NewRepository 创建订单仓库，db 为已初始化并完成迁移的数据库连接。
func NewRepository(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// Create 写入一条订单记录。
func (r *Repo) Create(ctx context.Context, order *Order) error {
	if err := gorm.G[Order](r.db).Create(ctx, order); err != nil {
		return fmt.Errorf("创建订单失败: %w", err)
	}
	return nil
}

// FindByID 按主键查询单条订单。
// 订单不存在时返回 ErrOrderNotFound；gorm 的错误在本层翻译，不再向上泄露。
func (r *Repo) FindByID(ctx context.Context, id uint) (*Order, error) {
	order, err := gorm.G[Order](r.db).
		Where("id = ?", id).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrOrderNotFound
		}
		return nil, fmt.Errorf("查询订单失败: %w", err)
	}
	return &order, nil
}

// ListByUser 查询指定用户的全部订单，按创建时间倒序。
// 该用户没有订单时返回空列表且不报错。
func (r *Repo) ListByUser(ctx context.Context, userID uint) ([]Order, error) {
	orders, err := gorm.G[Order](r.db).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询用户订单列表失败: %w", err)
	}
	return orders, nil
}

// ListAll 查询全部订单（管理端），按创建时间倒序。
func (r *Repo) ListAll(ctx context.Context) ([]Order, error) {
	orders, err := gorm.G[Order](r.db).
		Order("created_at DESC").
		Find(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询订单列表失败: %w", err)
	}
	return orders, nil
}

// UpdateStatus 将订单状态从 from 流转为 to。
// 采用条件更新（CAS）：状态校验与状态改写由同一条 SQL 完成，中间不存在竞态窗口。
// 影响行数为 0 时返回 ErrStatusConflict——订单不存在，或状态已被其他请求流转。
func (r *Repo) UpdateStatus(ctx context.Context, id uint, from, to Status) error {
	rows, err := gorm.G[Order](r.db).
		Where("id = ? AND status = ?", id, from).
		Update(ctx, "status", to)
	if err != nil {
		return fmt.Errorf("更新订单状态失败: %w", err)
	}
	if rows == 0 {
		return ErrStatusConflict
	}
	return nil
}
