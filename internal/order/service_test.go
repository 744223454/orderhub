package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestValidateOrderInput 覆盖下单参数校验的边界值。
func TestValidateOrderInput(t *testing.T) {
	cases := []struct {
		name        string
		productName string
		amount      int64
		wantErr     error
	}{
		{"正常参数", "机械键盘", 29900, nil},
		{"商品名恰好 1 字符（下界）", "书", 100, nil},
		{"商品名恰好 128 字符（上界，中文按字符计）", strings.Repeat("中", 128), 100, nil},
		{"商品名为空", "", 100, ErrInvalidProductName},
		{"商品名 129 字符超限", strings.Repeat("中", 129), 100, ErrInvalidProductName},
		{"金额为零", "机械键盘", 0, ErrInvalidAmount},
		{"金额为负", "机械键盘", -1, ErrInvalidAmount},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateOrderInput(tc.productName, tc.amount)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("期望校验通过，实际报错: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("期望包装哨兵 %v，实际: %v", tc.wantErr, err)
			}
		})
	}
}

// TestServiceCreateOrder 验证订单创建：主键回填、初始状态固定为待支付。
func TestServiceCreateOrder(t *testing.T) {
	service := NewService(setupTestRepo(t))
	ctx := context.Background()

	o, err := service.CreateOrder(ctx, 7, "机械键盘", 29900)
	if err != nil {
		t.Fatalf("创建订单应成功，实际报错: %v", err)
	}
	if o.ID == 0 {
		t.Error("创建后主键未被回填，调用方无法获知新订单 ID")
	}
	if o.Status != StatusPending {
		t.Errorf("初始状态应为 %q，实际 %q", StatusPending, o.Status)
	}
	if o.UserID != 7 {
		t.Errorf("下单用户应为 7，实际 %d", o.UserID)
	}
}

// TestServiceCreateOrderRejectsInvalidInput 验证非法参数不会落库。
func TestServiceCreateOrderRejectsInvalidInput(t *testing.T) {
	service := NewService(setupTestRepo(t))
	ctx := context.Background()

	if _, err := service.CreateOrder(ctx, 7, "", 100); !errors.Is(err, ErrInvalidProductName) {
		t.Errorf("空商品名应返回 ErrInvalidProductName，实际: %v", err)
	}
	if _, err := service.CreateOrder(ctx, 7, "机械键盘", 0); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("零金额应返回 ErrInvalidAmount，实际: %v", err)
	}
}

// TestServiceListUserOrders 验证订单列表只返回本人订单。
func TestServiceListUserOrders(t *testing.T) {
	service := NewService(setupTestRepo(t))
	ctx := context.Background()

	if _, err := service.CreateOrder(ctx, 1, "我的订单", 100); err != nil {
		t.Fatalf("预置数据失败: %v", err)
	}
	if _, err := service.CreateOrder(ctx, 2, "他人订单", 200); err != nil {
		t.Fatalf("预置数据失败: %v", err)
	}

	got, err := service.ListUserOrders(ctx, 1, 1, 20)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	for _, o := range got.Orders {
		if o.UserID != 1 {
			t.Errorf("返回了其他用户的订单: %+v", o)
		}
	}
}

// TestServiceListUserOrdersPaginationFallback 验证分页参数的回落规则：
// 非法页码与越界条数一律回落成默认值，**不报错**。
//
// 这条用例是「参数不校验就等于把错误往后推」的防线：一旦有人改成返回 400，
// 它会立刻失败。
func TestServiceListUserOrdersPaginationFallback(t *testing.T) {
	service := NewService(setupTestRepo(t))
	ctx := context.Background()

	if err := service.repo.db.WithContext(ctx).Exec("DELETE FROM orders WHERE user_id = 11").Error; err != nil {
		t.Fatalf("清理预置数据失败: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := service.CreateOrder(ctx, 11, "订单", 100); err != nil {
			t.Fatalf("预置数据失败: %v", err)
		}
	}

	tests := []struct {
		name             string
		page, pageSize   int
		wantPage, wantPS int
	}{
		{"全零值应回落默认", 0, 0, 1, defaultOrderPageSize},
		{"负页码应回落", -5, -1, 1, defaultOrderPageSize},
		{"条数超上限应被截断", 1, maxOrderPageSize + 50, 1, maxOrderPageSize},
		{"正常值原样保留", 2, 10, 2, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.ListUserOrders(ctx, 11, tt.page, tt.pageSize)
			if err != nil {
				t.Fatalf("分页参数非法不应报错，实际: %v", err)
			}
			if got.Page != tt.wantPage {
				t.Errorf("page 生效值应为 %d，实际 %d", tt.wantPage, got.Page)
			}
			if got.PageSize != tt.wantPS {
				t.Errorf("page_size 生效值应为 %d，实际 %d", tt.wantPS, got.PageSize)
			}
		})
	}

	// 第 1 页 2 条、第 2 页 1 条，且两页的 total 都是 3。
	p1, err := service.ListUserOrders(ctx, 11, 1, 2)
	if err != nil {
		t.Fatalf("查询第 1 页失败: %v", err)
	}
	p2, err := service.ListUserOrders(ctx, 11, 2, 2)
	if err != nil {
		t.Fatalf("查询第 2 页失败: %v", err)
	}
	if len(p1.Orders) != 2 || len(p2.Orders) != 1 {
		t.Errorf("两页应分别为 2 条与 1 条，实际 %d 与 %d", len(p1.Orders), len(p2.Orders))
	}
	if p1.Total != 3 || p2.Total != 3 {
		t.Errorf("两页的 total 都应为 3，实际 %d 与 %d", p1.Total, p2.Total)
	}
}

// TestServiceListAllOrdersPagination 验证管理端列表翻页。
func TestServiceListAllOrdersPagination(t *testing.T) {
	service := NewService(setupTestRepo(t))
	ctx := context.Background()

	if err := service.repo.db.WithContext(ctx).Exec("DELETE FROM orders WHERE user_id BETWEEN 30 AND 39").Error; err != nil {
		t.Fatalf("清理预置数据失败: %v", err)
	}
	for u := 0; u < 3; u++ {
		if _, err := service.CreateOrder(ctx, uint(30+u), "订单", 100); err != nil {
			t.Fatalf("预置数据失败: %v", err)
		}
	}

	got, err := service.ListAllOrders(ctx, 0, 0)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if got.Total < 3 {
		t.Errorf("总数应至少为 3，实际 %d", got.Total)
	}
	if got.Page != 1 || got.PageSize != defaultOrderPageSize {
		t.Errorf("零值入参应回落到 page=1 / page_size=%d，实际 %d / %d",
			defaultOrderPageSize, got.Page, got.PageSize)
	}

	// ── 翻页验收探针：offset 是「跳过条数」，不是页码 ────────────────
	// 服务层必须把 page 换算成 (page-1)*pageSize 再传给仓储层。
	// 若把 page 直接当 offset 传：第 1 页会从第 2 条开始（静默丢掉最新一条）、
	// 第 2 页会与期望错位 —— 这段断言就是给这个换算关系立的靶子。
	probeBase := time.Now().Add(time.Hour)
	for k := 1; k <= 6; k++ {
		o := &Order{
			UserID:      uint(30 + k%3),
			ProductName: fmt.Sprintf("翻页探针-%d", k),
			Amount:      100,
			Status:      StatusPending,
			CreatedAt:   probeBase.Add(time.Duration(k) * time.Minute),
		}
		if err := service.repo.Create(ctx, o); err != nil {
			t.Fatalf("预置探针数据失败: %v", err)
		}
	}
	// 探针创建时间在未来，DESC 排序下必在表头，依次为 翻页探针-6 … 翻页探针-1。

	p1, err := service.ListAllOrders(ctx, 1, 3)
	if err != nil {
		t.Fatalf("查询第 1 页失败: %v", err)
	}
	assertProbeOrder(t, p1.Orders, []string{"翻页探针-6", "翻页探针-5", "翻页探针-4"}, "第 1 页")

	p2, err := service.ListAllOrders(ctx, 2, 3)
	if err != nil {
		t.Fatalf("查询第 2 页失败: %v", err)
	}
	assertProbeOrder(t, p2.Orders, []string{"翻页探针-3", "翻页探针-2", "翻页探针-1"}, "第 2 页")
}

// newPendingOrder 预置一张指定用户的待支付订单。
func newPendingOrder(t *testing.T, s *Service, userID uint) *Order {
	t.Helper()
	o, err := s.CreateOrder(context.Background(), userID, "测试商品", 1000)
	if err != nil {
		t.Fatalf("预置订单失败: %v", err)
	}
	return o
}

// assertStatus 回查数据库，断言订单当前状态。
func assertStatus(t *testing.T, s *Service, orderID uint, want Status) {
	t.Helper()
	stored, err := s.repo.FindByID(context.Background(), orderID)
	if err != nil {
		t.Fatalf("回查订单失败: %v", err)
	}
	if stored.Status != want {
		t.Errorf("数据库中的状态应为 %q，实际 %q", want, stored.Status)
	}
}

// TestServicePayOrder 验证支付成功：返回值与数据库的状态都要同步为 paid。
func TestServicePayOrder(t *testing.T) {
	service := NewService(setupTestRepo(t))
	ctx := context.Background()
	o := newPendingOrder(t, service, 1)

	paid, err := service.PayOrder(ctx, 1, o.ID)
	if err != nil {
		t.Fatalf("支付应成功，实际报错: %v", err)
	}
	// 返回值状态必须已同步，否则响应里仍是 pending，前端会误判为支付失败。
	if paid.Status != StatusPaid {
		t.Errorf("返回值状态应为 %q，实际 %q", StatusPaid, paid.Status)
	}
	assertStatus(t, service, o.ID, StatusPaid)
}

// TestServicePayOrderRejectsOtherUser 验证越权防护：支付他人订单必须失败且不改数据。
func TestServicePayOrderRejectsOtherUser(t *testing.T) {
	service := NewService(setupTestRepo(t))
	ctx := context.Background()
	o := newPendingOrder(t, service, 1)

	if _, err := service.PayOrder(ctx, 2, o.ID); !errors.Is(err, ErrOrderNotFound) {
		t.Errorf("越权支付应返回 ErrOrderNotFound（不暴露他人订单是否存在），实际: %v", err)
	}
	assertStatus(t, service, o.ID, StatusPending)
}

// TestServicePayOrderRejectsRepeat 验证重复支付被状态机拦下。
func TestServicePayOrderRejectsRepeat(t *testing.T) {
	service := NewService(setupTestRepo(t))
	ctx := context.Background()
	o := newPendingOrder(t, service, 1)

	if _, err := service.PayOrder(ctx, 1, o.ID); err != nil {
		t.Fatalf("首次支付应成功: %v", err)
	}
	if _, err := service.PayOrder(ctx, 1, o.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("重复支付应返回 ErrInvalidTransition，实际: %v", err)
	}
}

// TestServiceShipOrder 验证发货需要先支付。
func TestServiceShipOrder(t *testing.T) {
	service := NewService(setupTestRepo(t))
	ctx := context.Background()
	o := newPendingOrder(t, service, 1)

	// 待支付订单不能直接发货。
	if _, err := service.ShipOrder(ctx, o.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("待支付订单发货应返回 ErrInvalidTransition，实际: %v", err)
	}
	assertStatus(t, service, o.ID, StatusPending)

	if _, err := service.PayOrder(ctx, 1, o.ID); err != nil {
		t.Fatalf("支付失败: %v", err)
	}

	shipped, err := service.ShipOrder(ctx, o.ID)
	if err != nil {
		t.Fatalf("发货应成功，实际报错: %v", err)
	}
	if shipped.Status != StatusShipped {
		t.Errorf("返回值状态应为 %q，实际 %q", StatusShipped, shipped.Status)
	}
	assertStatus(t, service, o.ID, StatusShipped)
}

// TestServiceCompleteOrder 验证完成的唯一合法前驱是「已发货」，而非「已支付」。
func TestServiceCompleteOrder(t *testing.T) {
	service := NewService(setupTestRepo(t))
	ctx := context.Background()
	o := newPendingOrder(t, service, 1)

	if _, err := service.PayOrder(ctx, 1, o.ID); err != nil {
		t.Fatalf("支付失败: %v", err)
	}

	// 关键用例：已支付但未发货的订单不能直接完成，否则等于跳过发货环节。
	if _, err := service.CompleteOrder(ctx, o.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("未发货订单不能直接完成，应返回 ErrInvalidTransition，实际: %v", err)
	}
	assertStatus(t, service, o.ID, StatusPaid)

	if _, err := service.ShipOrder(ctx, o.ID); err != nil {
		t.Fatalf("发货失败: %v", err)
	}

	completed, err := service.CompleteOrder(ctx, o.ID)
	if err != nil {
		t.Fatalf("完成应成功，实际报错: %v", err)
	}
	if completed.Status != StatusCompleted {
		t.Errorf("返回值状态应为 %q，实际 %q", StatusCompleted, completed.Status)
	}
	assertStatus(t, service, o.ID, StatusCompleted)
}

// TestServiceRefundOrder 覆盖退款的合法与非法前驱状态。
func TestServiceRefundOrder(t *testing.T) {
	service := NewService(setupTestRepo(t))
	ctx := context.Background()

	t.Run("已支付可直接退款", func(t *testing.T) {
		o := newPendingOrder(t, service, 1)
		if _, err := service.PayOrder(ctx, 1, o.ID); err != nil {
			t.Fatalf("支付失败: %v", err)
		}
		refunded, err := service.RefundOrder(ctx, o.ID)
		if err != nil {
			t.Fatalf("已支付订单退款应成功，实际报错: %v", err)
		}
		if refunded.Status != StatusRefunded {
			t.Errorf("返回值状态应为 %q，实际 %q", StatusRefunded, refunded.Status)
		}
		assertStatus(t, service, o.ID, StatusRefunded)
	})

	t.Run("已发货可退款", func(t *testing.T) {
		o := newPendingOrder(t, service, 1)
		if _, err := service.PayOrder(ctx, 1, o.ID); err != nil {
			t.Fatalf("支付失败: %v", err)
		}
		if _, err := service.ShipOrder(ctx, o.ID); err != nil {
			t.Fatalf("发货失败: %v", err)
		}
		if _, err := service.RefundOrder(ctx, o.ID); err != nil {
			t.Errorf("已发货订单应可退款，实际报错: %v", err)
		}
	})

	t.Run("待支付不可退款", func(t *testing.T) {
		o := newPendingOrder(t, service, 1)
		if _, err := service.RefundOrder(ctx, o.ID); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("待支付订单退款应返回 ErrInvalidTransition，实际: %v", err)
		}
	})

	t.Run("已完成不可退款", func(t *testing.T) {
		o := newPendingOrder(t, service, 1)
		if _, err := service.PayOrder(ctx, 1, o.ID); err != nil {
			t.Fatalf("支付失败: %v", err)
		}
		if _, err := service.ShipOrder(ctx, o.ID); err != nil {
			t.Fatalf("发货失败: %v", err)
		}
		if _, err := service.CompleteOrder(ctx, o.ID); err != nil {
			t.Fatalf("完成失败: %v", err)
		}
		if _, err := service.RefundOrder(ctx, o.ID); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("已完成订单不应允许退款，实际: %v", err)
		}
	})
}

// TestServiceTransitRejectsMissingOrder 验证对不存在的订单操作返回 ErrOrderNotFound。
func TestServiceTransitRejectsMissingOrder(t *testing.T) {
	service := NewService(setupTestRepo(t))
	ctx := context.Background()

	if _, err := service.ShipOrder(ctx, 99999999); !errors.Is(err, ErrOrderNotFound) {
		t.Errorf("发货不存在的订单应返回 ErrOrderNotFound，实际: %v", err)
	}
	if _, err := service.CompleteOrder(ctx, 99999999); !errors.Is(err, ErrOrderNotFound) {
		t.Errorf("完成不存在的订单应返回 ErrOrderNotFound，实际: %v", err)
	}
	if _, err := service.RefundOrder(ctx, 99999999); !errors.Is(err, ErrOrderNotFound) {
		t.Errorf("退款不存在的订单应返回 ErrOrderNotFound，实际: %v", err)
	}
}
