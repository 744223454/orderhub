package order

import "errors"

// 订单模块的业务错误。
//
// 这些错误是「可预期」的失败，调用方（service / handler）需要据此改变行为，
// 因此统一用 errors.Is 判断，**不要依赖错误文案做字符串比较**。
//
// 与之相对的技术错误（数据库连接失败、SQL 报错等）不进这里，
// 由仓库层用 fmt.Errorf("描述: %w", err) 包装后向上抛。
var (
	// ErrOrderNotFound 订单不存在。
	ErrOrderNotFound = errors.New("订单不存在")
	// ErrStatusConflict 订单不存在，或当前状态已被其他请求流转。
	// 条件更新影响行数为 0 时返回，用于防止并发下的重复流转。
	ErrStatusConflict = errors.New("订单不存在或状态已变更")
	// ErrInvalidTransition 订单当前状态不允许执行该操作（如待支付订单直接完成）。
	// 与 ErrStatusConflict 的区别：前者重试也无用，后者是并发竞争导致、重试可能成功。
	ErrInvalidTransition = errors.New("订单当前状态不允许该操作")
	// ErrInvalidProductName 商品名不合法（为空或超出长度限制），错误文案中会附带要求。
	ErrInvalidProductName = errors.New("商品名不合法")
	// ErrInvalidAmount 订单金额不合法（必须为正数）。
	ErrInvalidAmount = errors.New("订单金额不合法")
)
