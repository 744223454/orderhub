package order

import (
	"context"
	"fmt"
	"time"
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

// 分页相关常量。取值与 user 包的 defaultUserPageSize / maxUserPageSize 保持一致，
// 两个列表接口的口径统一，前端才能复用同一套分页逻辑。
const (
	// defaultOrderPageSize 是订单列表的默认每页条数。
	defaultOrderPageSize = 20
	// maxOrderPageSize 是每页条数上限，防止调用方一次把整张表拖走。
	maxOrderPageSize = 100
)

// statsDailyDays 是订单统计「按天趋势」的时间窗口：含今天在内的最近 N 个自然日。
// 看板固定展示 14 天，暂不开放调用方自定义。
const statsDailyDays = 14

// OrderPage 是一页订单查询结果。
type OrderPage struct {
	// Orders 当前页的订单。
	Orders []Order
	// Total 满足条件的订单总数（不受分页限制）。
	Total int64
	// Page 生效后的页码，从 1 开始。
	Page int
	// PageSize 生效后的每页条数。
	PageSize int
}

// StatusStat 是单个状态的聚合统计（统计接口输出）。
type StatusStat struct {
	// Status 订单状态。
	Status Status `json:"status"`
	// Count 该状态的订单数。
	Count int64 `json:"count"`
	// Amount 该状态的金额合计（分）。
	Amount int64 `json:"amount"`
}

// DailyStat 是单日的聚合统计（统计接口输出）。
type DailyStat struct {
	// Date 日期，北京时间口径，格式 YYYY-MM-DD。
	Date string `json:"date"`
	// Count 该天的订单数。
	Count int64 `json:"count"`
	// Amount 该天的金额合计（分）。
	Amount int64 `json:"amount"`
}

// Stats 是订单统计接口的完整结果，直接作为响应体使用。
type Stats struct {
	// TotalOrders 全量订单数。
	TotalOrders int64 `json:"total_orders"`
	// TotalAmount 全量金额合计（分）。
	TotalAmount int64 `json:"total_amount"`
	// ByStatus 按状态分组的统计：五种状态定长全量返回，顺序固定为
	// pending → paid → shipped → completed → refunded（与常量的声明顺序一致），
	// 无订单的状态计数与金额为 0。
	ByStatus []StatusStat `json:"by_status"`
	// Daily 近 statsDailyDays 天的逐日统计：北京时间自然日（含今天），
	// 连续定长返回、按日期升序（首条最早、末条为今天），无数据的日期补零。
	Daily []DailyStat `json:"daily"`
}

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

// ListUserOrders 分页查询指定用户的订单。
// 非法分页参数一律回落到默认值，不报错——列表接口不值得让调用方因为「页码写错」失败一次。
func (s *Service) ListUserOrders(ctx context.Context, userID uint, page, pageSize int) (*OrderPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultOrderPageSize
	}
	if pageSize > maxOrderPageSize {
		pageSize = maxOrderPageSize
	}

	orders, total, err := s.repo.ListByUser(ctx, userID, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, err
	}

	return &OrderPage{
		Orders:   orders,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// ListAllOrders 分页查询全部订单（管理端使用）。参数回落规则与 ListUserOrders 相同。
func (s *Service) ListAllOrders(ctx context.Context, page, pageSize int) (*OrderPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultOrderPageSize
	}
	if pageSize > maxOrderPageSize {
		pageSize = maxOrderPageSize
	}

	orders, total, err := s.repo.ListAll(ctx, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, err
	}

	return &OrderPage{
		Orders:   orders,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// Stats 汇总订单统计：全量总数与金额、按状态分组、近 statsDailyDays 天逐日趋势。
//
// 补齐职责在本层：by_status 五种状态定长返回（缺的补 0）、daily 补齐连续日期
// （缺的补零），让前端图表拿到的是可直接渲染的定长序列。
// 总量可由按状态分组的结果汇总得到，无需额外查询。
func (s *Service) Stats(ctx context.Context) (*Stats, error) {
	// beijing 是统计口径的北京时间时区；仓库层 SQL 里的 'Asia/Shanghai'
	// 是同一口径的另一份表达（中国无夏令时，两者恒等）——改动任一侧须同步。
	beijing := time.FixedZone("CST", 8*3600)
	now := time.Now().In(beijing)
	var (
		totalOrders int64
		totalAmount int64
		byStatus    []StatusStat
		dailyStats  []DailyStat
	)

	aggregates, err := s.repo.CountByStatus(ctx)
	if err != nil {
		return nil, err
	}

	aggByStatus := make(map[Status]StatusAggregate, len(aggregates))
	for _, a := range aggregates {
		aggByStatus[a.Status] = a
	}

	for _, st := range []Status{StatusPending, StatusPaid, StatusShipped, StatusCompleted, StatusRefunded} {
		a := aggByStatus[st]
		totalOrders += a.Count
		totalAmount += a.Amount
		byStatus = append(byStatus, StatusStat{Status: st, Count: a.Count, Amount: a.Amount})
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, beijing)
	since := today.AddDate(0, 0, -(statsDailyDays - 1))
	dailyAggr, err := s.repo.DailyStats(ctx, since)
	if err != nil {
		return nil, err
	}

	dailyMap := make(map[string]DailyAggregate, len(dailyAggr))
	for _, d := range dailyAggr {
		dailyMap[d.Day] = d
	}
	for i := range statsDailyDays {
		day := since.AddDate(0, 0, i).Format("2006-01-02")
		d := dailyMap[day]
		dailyStats = append(dailyStats, DailyStat{Date: day, Count: d.Count, Amount: d.Amount})
	}

	return &Stats{
		TotalOrders: totalOrders,
		TotalAmount: totalAmount,
		ByStatus:    byStatus,
		Daily:       dailyStats,
	}, nil
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
