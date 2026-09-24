package order

import "testing"

// allStatuses 是全部合法状态的列表，用于穷举流转组合。
var allStatuses = []Status{
	StatusPending,
	StatusPaid,
	StatusShipped,
	StatusCompleted,
	StatusRefunded,
}

// TestStatusValid 覆盖合法状态与各类非法输入。
func TestStatusValid(t *testing.T) {
	cases := []struct {
		name string
		s    Status
		want bool
	}{
		{"待支付", StatusPending, true},
		{"已支付", StatusPaid, true},
		{"已发货", StatusShipped, true},
		{"已完成", StatusCompleted, true},
		{"已退款", StatusRefunded, true},
		{"空字符串", Status(""), false},
		{"未定义状态", Status("cancelled"), false},
		{"大小写不符", Status("Pending"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.Valid(); got != tc.want {
				t.Errorf("Valid(%q) = %v, 期望 %v", tc.s, got, tc.want)
			}
		})
	}
}

// TestStatusCanTransitTo 穷举 5×5 = 25 种状态组合。
// 期望值来自状态机需求（而非实现推导）：
// pending→paid；paid→shipped/refunded；shipped→completed/refunded；终态无出边。
func TestStatusCanTransitTo(t *testing.T) {
	allowed := map[Status][]Status{
		StatusPending:   {StatusPaid},
		StatusPaid:      {StatusShipped, StatusRefunded},
		StatusShipped:   {StatusCompleted, StatusRefunded},
		StatusCompleted: {},
		StatusRefunded:  {},
	}

	for _, from := range allStatuses {
		for _, to := range allStatuses {
			want := contains(allowed[from], to)
			t.Run(string(from)+"→"+string(to), func(t *testing.T) {
				if got := from.CanTransitTo(to); got != want {
					t.Errorf("%q.CanTransitTo(%q) = %v, 期望 %v", from, to, got, want)
				}
			})
		}
	}
}

// TestStatusCanTransitToInvalid 覆盖非法状态值：无论作为起点还是终点都应返回 false。
func TestStatusCanTransitToInvalid(t *testing.T) {
	bogus := Status("bogus")
	if bogus.CanTransitTo(StatusPaid) {
		t.Error("非法起点应返回 false")
	}
	if StatusPending.CanTransitTo(bogus) {
		t.Error("非法终点应返回 false")
	}
	if bogus.CanTransitTo(bogus) {
		t.Error("两边都非法应返回 false")
	}
}

// contains 判断切片中是否包含目标值。
func contains(list []Status, target Status) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
