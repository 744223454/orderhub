// Package order 订单模块：下单、支付、发货、完成与退款。
// 依赖方向：order 可依赖 user（读取当前登录用户），user 不得反向依赖 order。
package order

import "time"

// Status 表示订单状态。
type Status string

const (
	// StatusPending 待支付：订单刚创建，尚未支付。
	StatusPending Status = "pending"
	// StatusPaid 已支付：买家已完成付款，等待发货。
	StatusPaid Status = "paid"
	// StatusShipped 已发货：卖家已发出货物，等待确认收货。
	StatusShipped Status = "shipped"
	// StatusCompleted 已完成：交易正常结束。
	StatusCompleted Status = "completed"
	// StatusRefunded 已退款：交易异常结束，款项已退回。
	StatusRefunded Status = "refunded"
)

// Valid 判断状态是否为预设的合法值之一。
func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusPaid, StatusShipped, StatusCompleted, StatusRefunded:
		return true
	}
	return false
}

// CanTransitTo 判断当前状态能否流转到目标状态。
// 预期规则：pending→paid→shipped→completed；paid 与 shipped 可流转到 refunded；其余流转一律非法。
func (s Status) CanTransitTo(target Status) bool {
	switch s {
	case StatusPending:
		return target == StatusPaid
	case StatusPaid:
		return target == StatusShipped || target == StatusRefunded
	case StatusShipped:
		return target == StatusCompleted || target == StatusRefunded
	case StatusCompleted, StatusRefunded:
		return false
	default:
		return false
	}
}

// Order 是订单模型，对应数据库中的 orders 表。
type Order struct {
	// ID 主键，自增。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// UserID 下单用户 ID。
	UserID uint `gorm:"not null;index" json:"user_id"`
	// ProductName 商品名称。
	ProductName string `gorm:"size:128;not null" json:"product_name"`
	// Amount 订单金额，单位为分（用整数避免浮点精度问题，前端展示时再除以 100）。
	Amount int64 `gorm:"not null" json:"amount"`
	// Status 订单状态。
	Status Status `gorm:"size:16;not null;index" json:"status"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 最后更新时间。
	UpdatedAt time.Time `json:"updated_at"`
}
