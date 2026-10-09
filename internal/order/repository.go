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

// ListByUser 分页查询指定用户的订单（按创建时间倒序），并返回满足条件的总数。
func (r *Repo) ListByUser(ctx context.Context, userID uint, offset, limit int) ([]Order, int64, error) {
	countChain := gorm.G[Order](r.db).Where("user_id = ?", userID)
	total, err := countChain.Count(ctx, "*")
	if err != nil {
		return nil, 0, fmt.Errorf("统计用户订单总数失败: %w", err)
	}

	listChain := gorm.G[Order](r.db).Where("user_id = ?", userID)
	orders, err := listChain.Order("created_at DESC").Offset(offset).Limit(limit).Find(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("查询用户订单列表失败: %w", err)
	}

	return orders, total, nil
}

// ListAll 分页查询全部订单（管理端），并返回总数。其余约定同 ListByUser。
func (r *Repo) ListAll(ctx context.Context, offset, limit int) ([]Order, int64, error) {
	countChain := gorm.G[Order](r.db)
	total, err := countChain.Count(ctx, "*")
	if err != nil {
		return nil, 0, fmt.Errorf("统计全部订单总数失败: %w", err)
	}

	listChain := gorm.G[Order](r.db)
	orders, err := listChain.Order("created_at DESC").Offset(offset).Limit(limit).Find(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("查询全部订单列表失败: %w", err)
	}

	return orders, total, nil
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
