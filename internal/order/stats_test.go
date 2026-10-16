package order

// 本文件是「订单统计」接口（GET /api/admin/orders/stats）的验收靶。
//
// 跑法：
//
//	go test ./internal/order/ -run 'Stats' -v -count=1
//
// 三条贯穿全文件的约定：
//   - 一律用 **baseline + 增量** 的相对断言：开发库里已有上百条已提交订单，
//     用例事务只能回滚自己写的行，任何绝对总数断言都会随环境漂移；
//   - 造数据只 INSERT，不 DELETE / UPDATE 任何既有行（本文件不含破坏性 SQL）；
//   - 日期全部运行时推算（北京时间今天 / 昨天 / 今天-13），不写死任何日期。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/744223454/orderhub/internal/auth"
	"github.com/744223454/orderhub/internal/user"
	"github.com/gin-gonic/gin"
)

// statsWindowDays 是 daily 的契约长度，刻意写死 14 而**不复用**实现里的 statsDailyDays：
// 复用同一个常量等于用实现校验自己 —— 有人把窗口改成 12 天时，用例会跟着一起变绿。
// 同理，状态名与字段名在下游也都用字面量断言。
const statsWindowDays = 14

// statsProbeUserID 是本文件造数据时统一使用的下单用户 ID。
// 取一个远离真实账号区间的值：orders.user_id 上没有外键约束，不需要真实用户存在，
// 失败输出里也能一眼认出哪些行是探针造成的。
const statsProbeUserID = 4242

// statsBeijing 返回北京时间时区。
// 中国不使用夏令时，固定 +8 偏移与 Asia/Shanghai 完全等价；
// 与实现口径一致（service.Stats 里的 beijing，DailyStats 里的 AT TIME ZONE 'Asia/Shanghai'）。
func statsBeijing() *time.Location {
	return time.FixedZone("CST", 8*3600)
}

// statsDay 把绝对时刻换算成北京时间自然日字符串（YYYY-MM-DD）。
func statsDay(tm time.Time) string {
	return tm.In(statsBeijing()).Format("2006-01-02")
}

// statsDayStart 返回「给定时刻所在的北京时间自然日」零点，再回推 daysAgo 天。
func statsDayStart(tm time.Time, daysAgo int) time.Time {
	b := tm.In(statsBeijing())
	start := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, statsBeijing())
	return start.AddDate(0, 0, -daysAgo)
}

// statsProbeSuffix 返回本次调用唯一的后缀，给探针商品名打上运行时唯一标识，
// 避免与库里既有数据混淆（也便于失败时人工排查）。
func statsProbeSuffix() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

// insertStatsProbe 插入一条探针订单：状态 / 金额 / 创建时刻全部由用例指定。
// 创建时刻走 GORM 的「非零值不覆盖」路径，因此落库的就是用例给的绝对时刻。
func insertStatsProbe(t *testing.T, repo *Repo, status Status, amount int64, createdAt time.Time) {
	t.Helper()
	o := &Order{
		UserID:      statsProbeUserID,
		ProductName: "统计探针-" + statsProbeSuffix(),
		Amount:      amount,
		Status:      status,
		CreatedAt:   createdAt,
	}
	if err := repo.Create(context.Background(), o); err != nil {
		t.Fatalf("预置探针订单失败: %v", err)
	}
	if o.ID == 0 {
		t.Fatal("探针订单主键未被回填，数据没真正落库")
	}
}

// fetchStats 调一次服务层统计；失败即终止用例（统计是纯读接口，失败没有可继续的理由）。
func fetchStats(t *testing.T, s *Service) *Stats {
	t.Helper()
	st, err := s.Stats(context.Background())
	if err != nil {
		t.Fatalf("调用 Stats 失败: %v", err)
	}
	return st
}

// statusStatOf 取 by_status 中指定状态的统计行；缺失即终止用例（定长契约已被破坏）。
func statusStatOf(t *testing.T, st *Stats, status Status) StatusStat {
	t.Helper()
	for _, s := range st.ByStatus {
		if s.Status == status {
			return s
		}
	}
	t.Fatalf("by_status 缺少状态 %q（应恒为 5 条全量返回）: %+v", status, st.ByStatus)
	return StatusStat{}
}

// dailyStatOf 取 daily 中指定日期的统计行；缺失即终止用例（连续补齐契约已被破坏）。
func dailyStatOf(t *testing.T, st *Stats, day string) DailyStat {
	t.Helper()
	for _, d := range st.Daily {
		if d.Date == day {
			return d
		}
	}
	t.Fatalf("daily 缺少日期 %q（窗口内每天都应出现，无数据也要补零）: %+v", day, st.Daily)
	return DailyStat{}
}

// assertByStatusShape 锁定 by_status 的定长与顺序契约：
// 恒 5 条，顺序固定 pending → paid → shipped → completed → refunded。
func assertByStatusShape(t *testing.T, st *Stats) {
	t.Helper()
	want := []Status{StatusPending, StatusPaid, StatusShipped, StatusCompleted, StatusRefunded}
	if len(st.ByStatus) != len(want) {
		t.Fatalf("by_status 应恒为 %d 条，实际 %d 条: %+v", len(want), len(st.ByStatus), st.ByStatus)
	}
	for i, w := range want {
		if st.ByStatus[i].Status != w {
			t.Errorf("by_status[%d] 应为 %q，实际 %q（顺序固定，前端图表按下标取值）", i, w, st.ByStatus[i].Status)
		}
	}
}

// assertDailyWindowShape 锁定 daily 的定长与连续性契约：
// 恰好 14 条、日期为 today-13 … today 的连续升序序列、无订单的天以 {count:0, amount:0} 占位。
// 「count 为 0 却带金额」是补零写错的典型形态（金额串到别的天去了），单独钉一条。
func assertDailyWindowShape(t *testing.T, st *Stats, today string) {
	t.Helper()
	if len(st.Daily) != statsWindowDays {
		t.Fatalf("daily 应恒为 %d 条，实际 %d 条: %+v", statsWindowDays, len(st.Daily), st.Daily)
	}
	start, err := time.ParseInLocation("2006-01-02", today, statsBeijing())
	if err != nil {
		t.Fatalf("用例内部错误：%q 不是合法日期: %v", today, err)
	}
	for i, d := range st.Daily {
		want := start.AddDate(0, 0, i-(statsWindowDays-1)).Format("2006-01-02")
		if d.Date != want {
			t.Errorf("daily[%d] 应为 %q，实际 %q（必须是 today-13 … today 的连续升序序列）", i, want, d.Date)
		}
		if d.Count < 0 || d.Amount < 0 {
			t.Errorf("daily[%d]=%s 出现负值: count=%d amount=%d", i, d.Date, d.Count, d.Amount)
		}
		if d.Count == 0 && d.Amount != 0 {
			t.Errorf("daily[%d]=%s 没有订单却带金额 %d，补零时金额必须归零", i, d.Date, d.Amount)
		}
	}
}

// skipIfDayBoundaryCrossed 在用例期间跨越了北京时间自然日边界时跳过整个用例。
// Stats 的 14 天窗口取自「调用时刻」：两次调用若横跨午夜，前后窗口会整体错位一天，
// 那是环境噪声而非实现缺陷。跳过比误报红更有价值，且 t.Skip 不会掩盖已记录的失败。
func skipIfDayBoundaryCrossed(t *testing.T, startDay string) {
	t.Helper()
	if endDay := statsDay(time.Now()); endDay != startDay {
		t.Skipf("用例期间跨越了北京时间自然日（%s → %s），本次结论不作数，请重跑", startDay, endDay)
	}
}

// TestStatsDailySlicesByBeijingDay 是本验收靶的核心：daily 必须按**北京时间**切自然日。
//
// 构造的两条订单刻意落在「UTC 的同一天」内：
//   - 北京时间 今天 00:30 → UTC 昨天 16:30
//   - 北京时间 昨天 23:30 → UTC 昨天 15:30
//
// 按 UTC 切天的实现会把两条并进同一行，按北京时间切天则分别落在今天与昨天 ——
// 这条用例的增量断言就是为这个差别立的靶子（断言用 baseline + 增量，不含任何绝对值）。
func TestStatsDailySlicesByBeijingDay(t *testing.T) {
	repo := setupTestRepo(t)
	service := NewService(repo)

	now := time.Now()
	today := statsDay(now)
	yesterday := statsDay(now.AddDate(0, 0, -1))
	todayStart := statsDayStart(now, 0)
	yesterdayStart := statsDayStart(now, 1)

	todayEarly := todayStart.Add(30 * time.Minute)                     // 北京时间今天 00:30
	yesterdayLate := yesterdayStart.Add(23*time.Hour + 30*time.Minute) // 北京时间昨天 23:30

	// 用例前提自检①：两者必须同属 UTC 的同一天，否则这条用例考不出「按北京时间切天」。
	if u1, u2 := todayEarly.UTC().Format("2006-01-02"), yesterdayLate.UTC().Format("2006-01-02"); u1 != u2 {
		t.Fatalf("用例前提不成立：两个时刻本应同属 UTC 的 %s，实际 %s / %s", u1, u1, u2)
	}
	// 前提自检②：按北京时间它们必须是相邻的两天。
	if got := statsDay(todayEarly); got != today {
		t.Fatalf("用例内部错误：%s 的北京时间为 %s，期望 %s", todayEarly, got, today)
	}
	if got := statsDay(yesterdayLate); got != yesterday {
		t.Fatalf("用例内部错误：%s 的北京时间为 %s，期望 %s", yesterdayLate, got, yesterday)
	}

	baseline := fetchStats(t, service)
	assertDailyWindowShape(t, baseline, today)

	insertStatsProbe(t, repo, StatusPaid, 11100, todayEarly)
	insertStatsProbe(t, repo, StatusShipped, 22200, yesterdayLate)

	after := fetchStats(t, service)
	assertDailyWindowShape(t, after, today)

	// 今天行：只应被 00:30 那条影响。
	gotToday, baseToday := dailyStatOf(t, after, today), dailyStatOf(t, baseline, today)
	if diff := gotToday.Count - baseToday.Count; diff != 1 {
		t.Errorf("北京时间今天 00:30 的订单应计入 daily 的 %s 行（计数 +1），实际增量 %d", today, diff)
	}
	if diff := gotToday.Amount - baseToday.Amount; diff != 11100 {
		t.Errorf("daily 的 %s 行金额增量应为 11100（分），实际 %d", today, diff)
	}

	// 昨天行：只应被 23:30 那条影响。
	gotYesterday, baseYesterday := dailyStatOf(t, after, yesterday), dailyStatOf(t, baseline, yesterday)
	if diff := gotYesterday.Count - baseYesterday.Count; diff != 1 {
		t.Errorf("北京时间昨天 23:30 的订单应计入 daily 的 %s 行（计数 +1），实际增量 %d", yesterday, diff)
	}
	if diff := gotYesterday.Amount - baseYesterday.Amount; diff != 22200 {
		t.Errorf("daily 的 %s 行金额增量应为 22200（分），实际 %d", yesterday, diff)
	}

	// 两条订单必须**分别**落在两个日期行：受影响的天数应恰好为 2 天。
	// 若实现按 UTC 切天，两条会被并进同一行 —— 那时「增量合计」仍是 2、单看总数查不出来，
	// 只有「受影响的天数变成 1」这个口径才抓得住，所以这里断言的是天数而不是增量之和。
	affected := 0
	for i := range after.Daily {
		if after.Daily[i] != baseline.Daily[i] {
			affected++
		}
	}
	if affected != 2 {
		t.Errorf("两条订单应分别落在两个日期行（受影响 2 天），实际受影响 %d 天（并成一天说明切天口径用了 UTC）", affected)
	}

	skipIfDayBoundaryCrossed(t, today)
}

// TestStatsIncrementAndFixedShape 锁定「增量精确」与「定长契约」两条主线：
// total 增量精确、by_status 恒 5 条且顺序固定且逐状态增量正确、
// daily 恒 14 条且首尾日期正确且只有受影响的天发生变化。
//
// 探针刻意只覆盖 4 种状态，留 completed 一个「没被碰到」的状态：
// 它必须纹丝不动，用来证明各状态之间没有串味。
func TestStatsIncrementAndFixedShape(t *testing.T) {
	repo := setupTestRepo(t)
	service := NewService(repo)

	now := time.Now()
	today := statsDay(now)
	yesterday := statsDay(now.AddDate(0, 0, -1))
	threeDaysAgo := statsDay(now.AddDate(0, 0, -3))
	todayStart := statsDayStart(now, 0)
	yesterdayStart := statsDayStart(now, 1)
	threeDaysAgoStart := statsDayStart(now, 3)

	baseline := fetchStats(t, service)
	assertByStatusShape(t, baseline)
	assertDailyWindowShape(t, baseline, today)

	// 5 条探针，分布在 3 个自然日：今天 3 条 / 昨天 1 条 / 今天-3 1 条。
	probes := []struct {
		status    Status
		amount    int64
		createdAt time.Time
	}{
		{StatusPending, 1000, todayStart.Add(10 * time.Hour)},
		{StatusPending, 2000, todayStart.Add(10*time.Hour + 30*time.Minute)},
		{StatusPaid, 3000, todayStart.Add(11 * time.Hour)},
		{StatusRefunded, 4000, yesterdayStart.Add(9 * time.Hour)},
		{StatusShipped, 6000, threeDaysAgoStart.Add(9 * time.Hour)},
	}
	var wantCount, wantAmount int64
	for _, p := range probes {
		insertStatsProbe(t, repo, p.status, p.amount, p.createdAt)
		wantCount++
		wantAmount += p.amount
	}

	after := fetchStats(t, service)
	assertByStatusShape(t, after)
	assertDailyWindowShape(t, after, today)

	// ① 全量增量：只认差值，不猜库里原本有多少条。
	if diff := after.TotalOrders - baseline.TotalOrders; diff != wantCount {
		t.Errorf("total_orders 增量应为 %d，实际 %d", wantCount, diff)
	}
	if diff := after.TotalAmount - baseline.TotalAmount; diff != wantAmount {
		t.Errorf("total_amount 增量应为 %d（分），实际 %d", wantAmount, diff)
	}

	// ② by_status：五个状态逐一核对增量，含「没被碰到的状态必须为 0」。
	wantByStatus := map[Status]struct{ count, amount int64 }{
		StatusPending:   {2, 3000},
		StatusPaid:      {1, 3000},
		StatusShipped:   {1, 6000},
		StatusCompleted: {0, 0}, // 没有 completed 探针：必须原样不动
		StatusRefunded:  {1, 4000},
	}
	for status, want := range wantByStatus {
		got, base := statusStatOf(t, after, status), statusStatOf(t, baseline, status)
		if diff := got.Count - base.Count; diff != want.count {
			t.Errorf("by_status 的 %s 计数增量应为 %d，实际 %d", status, want.count, diff)
		}
		if diff := got.Amount - base.Amount; diff != want.amount {
			t.Errorf("by_status 的 %s 金额增量应为 %d（分），实际 %d", status, want.amount, diff)
		}
	}

	// ③ daily：受影响的三天增量精确。
	wantDaily := map[string]struct{ count, amount int64 }{
		today:        {3, 6000},
		yesterday:    {1, 4000},
		threeDaysAgo: {1, 6000},
	}
	for day, want := range wantDaily {
		got, base := dailyStatOf(t, after, day), dailyStatOf(t, baseline, day)
		if diff := got.Count - base.Count; diff != want.count {
			t.Errorf("daily 的 %s 行计数增量应为 %d，实际 %d", day, want.count, diff)
		}
		if diff := got.Amount - base.Amount; diff != want.amount {
			t.Errorf("daily 的 %s 行金额增量应为 %d（分），实际 %d", day, want.amount, diff)
		}
	}

	// ④ 其余 11 天必须与 baseline 逐条完全一致：既是「无泄漏」，也证明补零逻辑没被破坏
	// （14 条依旧条条在位，没有被压缩或错位）。
	affected := 0
	for i := range after.Daily {
		d, b := after.Daily[i], baseline.Daily[i]
		if _, ok := wantDaily[d.Date]; ok {
			affected++
			continue
		}
		if d != b {
			t.Errorf("daily 的 %s 行不该受探针影响: baseline=%+v after=%+v", d.Date, b, d)
		}
	}
	if affected != len(wantDaily) {
		t.Errorf("受探针影响的日期应恰好 %d 天，实际 %d 天", len(wantDaily), affected)
	}

	skipIfDayBoundaryCrossed(t, today)
}

// TestStatsCountsOutOfWindowOrdersInTotalsOnly 锁定「窗口外只进全量、不进 daily」。
//
// 2 个月前的订单必须计入 total_orders / total_amount（这是全量口径），
// 但绝不能出现在近 14 天的 daily 里 —— 看板若把久远订单折进来，
// 表现为「某根柱子莫名暴涨」，比少算更难排查。
func TestStatsCountsOutOfWindowOrdersInTotalsOnly(t *testing.T) {
	repo := setupTestRepo(t)
	service := NewService(repo)

	now := time.Now()
	today := statsDay(now)
	outsideDay := statsDay(now.AddDate(0, 0, -60))
	outsideStart := statsDayStart(now, 60)

	const amount = 777777

	baseline := fetchStats(t, service)
	assertDailyWindowShape(t, baseline, today)

	insertStatsProbe(t, repo, StatusCompleted, amount, outsideStart.Add(12*time.Hour))

	after := fetchStats(t, service)
	assertDailyWindowShape(t, after, today)

	if diff := after.TotalOrders - baseline.TotalOrders; diff != 1 {
		t.Errorf("窗口外的订单仍应计入 total_orders（增量 1），实际 %d", diff)
	}
	if diff := after.TotalAmount - baseline.TotalAmount; diff != amount {
		t.Errorf("窗口外的订单仍应计入 total_amount（增量 %d），实际 %d", amount, diff)
	}

	gotCompleted, baseCompleted := statusStatOf(t, after, StatusCompleted), statusStatOf(t, baseline, StatusCompleted)
	if diff := gotCompleted.Count - baseCompleted.Count; diff != 1 {
		t.Errorf("窗口外的订单仍应计入 by_status 的 completed 行（计数 +1），实际增量 %d", diff)
	}
	if diff := gotCompleted.Amount - baseCompleted.Amount; diff != amount {
		t.Errorf("by_status 的 completed 行金额增量应为 %d，实际 %d", amount, diff)
	}

	// daily 逐条不变：窗口外数据既不新增行，也不挤掉任何一天（补零完整）。
	for i := range after.Daily {
		if after.Daily[i] != baseline.Daily[i] {
			t.Errorf("daily[%d] 不应受窗口外订单影响: baseline=%+v after=%+v",
				i, baseline.Daily[i], after.Daily[i])
		}
		if after.Daily[i].Date == outsideDay {
			t.Errorf("窗口外的日期 %s 不应出现在 daily 中", outsideDay)
		}
	}

	skipIfDayBoundaryCrossed(t, today)
}

// TestStatsDailyWindowFirstDayInclusive 锁定窗口首日是**闭区间**（北京时间今天-13）。
//
// 探针刻意压在首日的零点整：仓库层注释写明口径是「since（含）以来」，
// 所以边界那一刻的订单必须被计入。三种实现偏差都会在这里现形：
//   - since 多回推一天（今天-14）：整个窗口前移，daily 末条不再是今天（形状断言先炸）；
//   - since 的时区错（用 UTC 零点 ⇒ 北京时间 08:00）：首日 00:00 的订单被丢掉；
//   - 比较写成 `>`（丢了边界）：同样捞不回这条订单。
//
// 这类偏差在看板上只表现为「最早那根柱子略矮」，是最难被发现的一类。
func TestStatsDailyWindowFirstDayInclusive(t *testing.T) {
	repo := setupTestRepo(t)
	service := NewService(repo)

	now := time.Now()
	today := statsDay(now)
	firstDay := statsDay(now.AddDate(0, 0, -(statsWindowDays - 1)))
	firstDayStart := statsDayStart(now, statsWindowDays-1)

	const amount = 12345

	baseline := fetchStats(t, service)
	assertDailyWindowShape(t, baseline, today)

	// 首日零点整：压在边界上，正面验证「since 含」这个口径。
	insertStatsProbe(t, repo, StatusPaid, amount, firstDayStart)

	after := fetchStats(t, service)
	assertDailyWindowShape(t, after, today)

	gotFirst, baseFirst := dailyStatOf(t, after, firstDay), dailyStatOf(t, baseline, firstDay)
	if diff := gotFirst.Count - baseFirst.Count; diff != 1 {
		t.Errorf("压在窗口首日 %s 零点整的订单必须计入 daily 首行（计数 +1），实际增量 %d", firstDay, diff)
	}
	if diff := gotFirst.Amount - baseFirst.Amount; diff != amount {
		t.Errorf("daily 首行（%s）金额增量应为 %d，实际 %d", firstDay, amount, diff)
	}
	if after.Daily[0].Date != firstDay {
		t.Errorf("daily 首条应为窗口首日 %s，实际 %s", firstDay, after.Daily[0].Date)
	}

	skipIfDayBoundaryCrossed(t, today)
}

// ── HTTP 矩阵：走真实的中间件链 ─────────────────────────────────────────────

// statsTestSecret 是 HTTP 矩阵用例使用的 JWT 密钥。令牌由用例自行签发，
// 与运行环境的 JWT_SECRET 无关，因此本组用例不依赖任何部署配置。
const statsTestSecret = "stats-acceptance-test-secret"

// statsPath 是订单统计接口挂上完整路由链后的路径。
const statsPath = "/api/admin/orders/stats"

// statsHTTPProbeAmount 是 HTTP 用例探针的金额（分），取一个不易与既有数据撞车的值。
const statsHTTPProbeAmount int64 = 654321

// newStatsHTTPRouter 按 main.go 的同构方式装配路由：
// /api → auth.Auth（JWT 认证）→ auth.RequireRole(admin, ops) → StaffRoutes。
//
// 刻意调用真实的 StaffRoutes：它既是权限链的一环，也是「路由有没有被注册」的被测对象。
// order 包既有的 newTestRouter 只注入 userID / 角色桩、不含中间件链，也未挂 stats 路由，
// 所以 403 这一格必须在这里自行组装。
func newStatsHTTPRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	authed := api.Group("", auth.Auth(statsTestSecret))
	staff := authed.Group("", auth.RequireRole(user.RoleAdmin, user.RoleOps))
	StaffRoutes(staff, h)
	return r
}

// statsToken 用指定角色签发一枚有效期足够长的访问令牌。
func statsToken(t *testing.T, role user.Role) string {
	t.Helper()
	token, err := auth.GenerateToken(&user.User{ID: statsProbeUserID, Role: role}, statsTestSecret, time.Hour)
	if err != nil {
		t.Fatalf("签发测试令牌失败: %v", err)
	}
	return token
}

// doStatsJSON 发起一次 GET 请求，token 非空时以 Bearer 形式带上。
// 不复用 doJSON：后者不设 Authorization 头，而本组用例整条链都靠它。
func doStatsJSON(t *testing.T, r *gin.Engine, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// statsWireStatus / statsWireDaily / statsWire 描述响应体的**线上形状**：
// 字段名（snake_case）与类型都写在这里，解码失败即视为契约被改坏。
// 用指针字段是为了把「字段缺失」与「值恰好为 0」区分开。
type statsWireStatus struct {
	Status *string `json:"status"`
	Count  *int64  `json:"count"`
	Amount *int64  `json:"amount"`
}

type statsWireDaily struct {
	Date   *string `json:"date"`
	Count  *int64  `json:"count"`
	Amount *int64  `json:"amount"`
}

type statsWire struct {
	TotalOrders *int64            `json:"total_orders"`
	TotalAmount *int64            `json:"total_amount"`
	ByStatus    []statsWireStatus `json:"by_status"`
	Daily       []statsWireDaily  `json:"daily"`
}

// decodeStatsWire 解析统计响应，并把「字段名 / 类型 / 定长」三类契约一并验掉。
// 解析失败本身就说明线上形状变了（改成 camelCase、数字被序列化成字符串都会在此现形）。
func decodeStatsWire(t *testing.T, body string) statsWire {
	t.Helper()
	var wire statsWire
	if err := json.Unmarshal([]byte(body), &wire); err != nil {
		t.Fatalf("响应体不符合统计接口契约（字段名 / 类型不符或不是合法 JSON）: %v（原始: %s）", err, body)
	}
	if wire.TotalOrders == nil || wire.TotalAmount == nil {
		t.Fatalf("响应缺少 total_orders / total_amount: %s", body)
	}
	if len(wire.ByStatus) == 0 || len(wire.Daily) == 0 {
		t.Fatalf("响应缺少 by_status / daily: %s", body)
	}
	for i, s := range wire.ByStatus {
		if s.Status == nil || s.Count == nil || s.Amount == nil {
			t.Fatalf("by_status[%d] 缺少 status / count / amount: %s", i, body)
		}
	}
	for i, d := range wire.Daily {
		if d.Date == nil || d.Count == nil || d.Amount == nil {
			t.Fatalf("daily[%d] 缺少 date / count / amount: %s", i, body)
		}
	}

	// 字段名必须是 snake_case：前端 TS 类型与 swagger 都按这个名字对齐。
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("响应不是合法的 JSON 对象: %v（原始: %s）", err, body)
	}
	for _, key := range []string{"total_orders", "total_amount", "by_status", "daily"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("响应缺少字段 %q（字段名应为 snake_case）: %s", key, body)
		}
	}
	for _, key := range []string{"totalOrders", "totalAmount", "byStatus"} {
		if _, ok := raw[key]; ok {
			t.Errorf("响应出现了非契约字段 %q（字段名应为 snake_case）: %s", key, body)
		}
	}
	return wire
}

// wireStatusOf 取线上响应中指定状态的行；缺失即终止用例。
func wireStatusOf(t *testing.T, wire statsWire, status Status) statsWireStatus {
	t.Helper()
	for _, s := range wire.ByStatus {
		if Status(*s.Status) == status {
			return s
		}
	}
	t.Fatalf("by_status 缺少状态 %q", status)
	return statsWireStatus{}
}

// assertStatsWireInvariants 锁定与具体数据无关的四条契约：
//  1. by_status 顺序固定（pending → paid → shipped → completed → refunded）；
//  2. total_orders / total_amount 恒等于 by_status 的汇总（总量由分组结果汇总而来）；
//  3. daily 是 today-13 … today 的连续升序序列，末条为今天；
//  4. daily 是近 14 天窗口的子集，窗口内合计不可能超过全量合计。
func assertStatsWireInvariants(t *testing.T, wire statsWire, today string) {
	t.Helper()
	if len(wire.ByStatus) != 5 {
		t.Fatalf("by_status 应恒为 5 条，实际 %d 条", len(wire.ByStatus))
	}
	if len(wire.Daily) != statsWindowDays {
		t.Fatalf("daily 应恒为 %d 条，实际 %d 条", statsWindowDays, len(wire.Daily))
	}

	// 用字面量而不是包内常量：接口契约是给前端的五个固定状态名与固定顺序，
	// 拿实现自己的常量来比对，等于把「契约是否被改」的判断权交回给实现。
	wantOrder := []string{"pending", "paid", "shipped", "completed", "refunded"}
	var sumCount, sumAmount int64
	for i, s := range wire.ByStatus {
		if *s.Status != wantOrder[i] {
			t.Errorf("by_status[%d] 应为 %q，实际 %q（顺序固定）", i, wantOrder[i], *s.Status)
		}
		sumCount += *s.Count
		sumAmount += *s.Amount
	}
	if *wire.TotalOrders != sumCount {
		t.Errorf("total_orders 应由 by_status 汇总得出（期望 %d），实际 %d", sumCount, *wire.TotalOrders)
	}
	if *wire.TotalAmount != sumAmount {
		t.Errorf("total_amount 应由 by_status 汇总得出（期望 %d），实际 %d", sumAmount, *wire.TotalAmount)
	}

	start, err := time.ParseInLocation("2006-01-02", today, statsBeijing())
	if err != nil {
		t.Fatalf("用例内部错误：%q 不是合法日期: %v", today, err)
	}
	var dailyCount, dailyAmount int64
	for i, d := range wire.Daily {
		want := start.AddDate(0, 0, i-(statsWindowDays-1)).Format("2006-01-02")
		if *d.Date != want {
			t.Errorf("daily[%d] 应为 %q，实际 %q（须为 today-13 … today 连续升序）", i, want, *d.Date)
		}
		dailyCount += *d.Count
		dailyAmount += *d.Amount
	}
	if got := *wire.Daily[len(wire.Daily)-1].Date; got != today {
		t.Errorf("daily 末条应为北京时间今天 %s，实际 %s", today, got)
	}
	if dailyCount > *wire.TotalOrders {
		t.Errorf("近 14 天计数合计（%d）不应超过全量订单数（%d）", dailyCount, *wire.TotalOrders)
	}
	if dailyAmount > *wire.TotalAmount {
		t.Errorf("近 14 天金额合计（%d）不应超过全量金额（%d）", dailyAmount, *wire.TotalAmount)
	}
}

// TestStatsHTTPAccessMatrix 锁定统计接口的权限矩阵与响应形状。
//
// 认证 / 授权由 auth 中间件负责，这里把四种情形都走一遍真实链路：
// 无 token → 401；令牌非法 → 401；user 角色 → 403；ops / admin → 200 且形状正确。
// 路由 404（忘了挂 StaffRoutes）会让 200 那两格直接失败，顺带把「挂载」也验了。
func TestStatsHTTPAccessMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := setupTestRepo(t)
	router := newStatsHTTPRouter(NewHandler(NewService(repo)))

	now := time.Now()
	today := statsDay(now)
	todayStart := statsDayStart(now, 0)

	t.Run("无 token 返回 401", func(t *testing.T) {
		rec := doStatsJSON(t, router, statsPath, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("未携带令牌应返回 401，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}
		assertSingleJSONObject(t, rec.Body.String())
	})

	t.Run("令牌非法返回 401", func(t *testing.T) {
		rec := doStatsJSON(t, router, statsPath, "not.a.real.token")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("伪造令牌应返回 401，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}
		assertSingleJSONObject(t, rec.Body.String())
	})

	t.Run("user 角色返回 403", func(t *testing.T) {
		rec := doStatsJSON(t, router, statsPath, statsToken(t, user.RoleUser))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("普通用户访问统计应返回 403，实际 %d（响应体: %s）", rec.Code, rec.Body.String())
		}
		assertSingleJSONObject(t, rec.Body.String())
	})

	for _, role := range []user.Role{user.RoleOps, user.RoleAdmin} {
		t.Run(string(role)+" 角色返回 200 且形状正确", func(t *testing.T) {
			token := statsToken(t, role)

			// 先取 baseline：开发库既有数据不影响结论，断言全部落在增量上。
			baselineRec := doStatsJSON(t, router, statsPath, token)
			if baselineRec.Code != http.StatusOK {
				t.Fatalf("角色 %q 访问统计应返回 200，实际 %d（响应体: %s）",
					role, baselineRec.Code, baselineRec.Body.String())
			}
			baseline := decodeStatsWire(t, baselineRec.Body.String())
			assertStatsWireInvariants(t, baseline, today)

			// 探针落在北京时间今天正午：验证 HTTP 链路确实读到了事务内的新数据。
			insertStatsProbe(t, repo, StatusPending, statsHTTPProbeAmount, todayStart.Add(12*time.Hour))

			rec := doStatsJSON(t, router, statsPath, token)
			if rec.Code != http.StatusOK {
				t.Fatalf("角色 %q 访问统计应返回 200，实际 %d（响应体: %s）",
					role, rec.Code, rec.Body.String())
			}
			after := decodeStatsWire(t, rec.Body.String())
			assertStatsWireInvariants(t, after, today)

			if diff := *after.TotalOrders - *baseline.TotalOrders; diff != 1 {
				t.Errorf("total_orders 增量应为 1，实际 %d", diff)
			}
			if diff := *after.TotalAmount - *baseline.TotalAmount; diff != statsHTTPProbeAmount {
				t.Errorf("total_amount 增量应为 %d（分），实际 %d", statsHTTPProbeAmount, diff)
			}

			gotPending, basePending := wireStatusOf(t, after, StatusPending), wireStatusOf(t, baseline, StatusPending)
			if diff := *gotPending.Count - *basePending.Count; diff != 1 {
				t.Errorf("by_status 的 pending 计数增量应为 1，实际 %d", diff)
			}
			if diff := *gotPending.Amount - *basePending.Amount; diff != statsHTTPProbeAmount {
				t.Errorf("by_status 的 pending 金额增量应为 %d，实际 %d", statsHTTPProbeAmount, diff)
			}

			last, baseLast := after.Daily[len(after.Daily)-1], baseline.Daily[len(baseline.Daily)-1]
			if *last.Date != today {
				t.Fatalf("daily 末条应为今天 %s，实际 %s", today, *last.Date)
			}
			if diff := *last.Count - *baseLast.Count; diff != 1 {
				t.Errorf("daily 的今天行计数增量应为 1，实际 %d", diff)
			}
			if diff := *last.Amount - *baseLast.Amount; diff != statsHTTPProbeAmount {
				t.Errorf("daily 的今天行金额增量应为 %d，实际 %d", statsHTTPProbeAmount, diff)
			}
		})
	}

	skipIfDayBoundaryCrossed(t, today)
}
