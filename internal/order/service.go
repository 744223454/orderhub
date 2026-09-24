package order

import (
	"context"
	"fmt"
	"unicode/utf8"
)

// 商品名的长度限制，按字符数计。
// PostgreSQL 的 varchar(n) 同样按字符计数，因此与 gorm size 的口径一致；
// 上限必须与 Order.ProductName 的 gorm:"size:128" 保持同步，否则会出现
// 「服务层放行、数据库拒绝」的割裂。
const (
	productNameMinLen = 1
	productNameMaxLen = 128
)

// Service 承载订单模块的业务逻辑。
type Service struct {
	repo *Repo
}

// NewService 创建订单服务，repo 为订单数据仓库。
func NewService(repo *Repo) *Service {
	return &Service{repo: repo}
}

// CreateOrder 创建订单，初始状态固定为待支付。
func (s *Service) CreateOrder(ctx context.Context, userID uint, productName string, amount int64) (*Order, error) {
	if err := validateOrderInput(productName, amount); err != nil {
		return nil, err
	}

	order := &Order{
		UserID:      userID,
		ProductName: productName,
		Amount:      amount,
		// 初始状态由服务层决定，不接受调用方传入，
		// 否则用户可以造出一张「已完成」的订单绕过支付流程。
		Status: StatusPending,
	}

	if err := s.repo.Create(ctx, order); err != nil {
		return nil, err
	}

	return order, nil
}

// ListUserOrders 查询指定用户的全部订单。
func (s *Service) ListUserOrders(ctx context.Context, userID uint) ([]Order, error) {
	return s.repo.ListByUser(ctx, userID)
}

// ListAllOrders 查询全部订单（管理端使用）。
func (s *Service) ListAllOrders(ctx context.Context) ([]Order, error) {
	return s.repo.ListAll(ctx)
}

// PayOrder 用户支付自己的订单（pending → paid）。
func (s *Service) PayOrder(ctx context.Context, userID, orderID uint) (*Order, error) {
	o, err := s.findForUser(ctx, userID, orderID)
	if err != nil {
		return nil, err
	}
	return s.transit(ctx, o, StatusPaid)
}

// ShipOrder 运营/管理员发货（paid → shipped）。管理端操作，无需属主校验。
func (s *Service) ShipOrder(ctx context.Context, orderID uint) (*Order, error) {
	return s.transitByID(ctx, orderID, StatusShipped)
}

// CompleteOrder 完成订单（shipped → completed）。
func (s *Service) CompleteOrder(ctx context.Context, orderID uint) (*Order, error) {
	return s.transitByID(ctx, orderID, StatusCompleted)
}

// RefundOrder 退款（paid 或 shipped → refunded）。
func (s *Service) RefundOrder(ctx context.Context, orderID uint) (*Order, error) {
	return s.transitByID(ctx, orderID, StatusRefunded)
}

// findForUser 查出订单并校验属主，防止越权操作他人订单。
func (s *Service) findForUser(ctx context.Context, userID, orderID uint) (*Order, error) {
	o, err := s.repo.FindByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	// 属主不符时故意返回 ErrOrderNotFound：若返回「无权访问」，
	// 响应上的差异就等于向调用方确认了该订单 ID 真实存在。
	if o.UserID != userID {
		return nil, ErrOrderNotFound
	}
	return o, nil
}

// transitByID 查出订单并流转到目标状态（管理端操作，不校验属主）。
func (s *Service) transitByID(ctx context.Context, orderID uint, to Status) (*Order, error) {
	o, err := s.repo.FindByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	return s.transit(ctx, o, to)
}

// transit 把订单流转到目标状态。
//
// 目标状态是否合法一律交给状态机判定（CanTransitTo），不在各方法里手写状态条件——
// 手写条件会让「哪个状态能到哪个状态」这条规则散落多处，改一处漏一处。
// 传入的 from 取自刚查出的订单，若期间状态已被并发请求改写，
// 仓库层的条件更新会返回 ErrStatusConflict，由调用方原样上抛。
func (s *Service) transit(ctx context.Context, o *Order, to Status) (*Order, error) {
	if !o.Status.CanTransitTo(to) {
		return nil, ErrInvalidTransition
	}

	if err := s.repo.UpdateStatus(ctx, o.ID, o.Status, to); err != nil {
		return nil, err
	}

	// 同步内存中的状态：UpdateStatus 只写库，
	// 不同步的话返回给调用方的仍是流转前的数据，响应内容与实际不符。
	o.Status = to
	return o, nil
}

// validateOrderInput 校验下单参数。
// 返回的错误包装了对应哨兵（ErrInvalidProductName / ErrInvalidAmount），
// 接口层据此映射为 400 而非 500。
func validateOrderInput(productName string, amount int64) error {
	if n := utf8.RuneCountInString(productName); n < productNameMinLen || n > productNameMaxLen {
		return fmt.Errorf("%w: 长度需在 %d 到 %d 个字符之间", ErrInvalidProductName, productNameMinLen, productNameMaxLen)
	}
	if amount <= 0 {
		return fmt.Errorf("%w: 金额必须为正数", ErrInvalidAmount)
	}
	return nil
}
