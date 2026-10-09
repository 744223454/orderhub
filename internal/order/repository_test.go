package order

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestRepo 连接真实 PostgreSQL 并返回仓库实例。
// 每个用例在独立事务中运行、结束时回滚，用例之间互不污染，也不会残留测试数据。
// 未配置 DATABASE_URL 时跳过，保证在无数据库环境下不会误报失败。
func setupTestRepo(t *testing.T) *Repo {
	t.Helper()

	if err := godotenv.Load("../../.env"); err != nil && !os.IsNotExist(err) {
		t.Fatalf("加载 .env 失败: %v", err)
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("未配置 DATABASE_URL，跳过仓库集成测试")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		TranslateError: true,
		Logger:         logger.Default.LogMode(logger.Error),
	})
	if err != nil {
		t.Fatalf("连接数据库失败: %v", err)
	}
	// 表结构迁移是幂等的，且与服务启动时的行为一致。
	if err := db.AutoMigrate(&Order{}); err != nil {
		t.Fatalf("迁移 orders 表失败: %v", err)
	}

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("开启事务失败: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	return NewRepository(tx)
}

// canceledCtx 返回一个已取消的 context，用于让数据库操作必然失败，
// 借此验证仓库层是否把底层错误如实向上传递。
func canceledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// TestRepoCreateAndFindByID 验证正常写入与查询，并确认主键被回填。
func TestRepoCreateAndFindByID(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	created := &Order{UserID: 7, ProductName: "机械键盘", Amount: 29900, Status: StatusPending}
	if err := repo.Create(ctx, created); err != nil {
		t.Fatalf("创建订单失败: %v", err)
	}
	if created.ID == 0 {
		t.Error("创建后主键未被回填，调用方无法获知新订单 ID")
	}

	found, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	if found.UserID != 7 || found.ProductName != "机械键盘" || found.Amount != 29900 || found.Status != StatusPending {
		t.Errorf("查询结果与写入不一致: %+v", found)
	}
}

// TestRepoFindByIDNotFound 验证「订单不存在」可被调用方识别。
func TestRepoFindByIDNotFound(t *testing.T) {
	repo := setupTestRepo(t)

	_, err := repo.FindByID(context.Background(), 99999999)
	if err == nil {
		t.Fatal("查询不存在的订单应返回错误")
	}
	if !errors.Is(err, ErrOrderNotFound) {
		t.Errorf("应返回包内哨兵错误 ErrOrderNotFound，实际: %v", err)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		t.Error("不应把 gorm 的错误透出到仓库层之外，否则 service 层被迫依赖 gorm")
	}
}

// TestRepoListByUser 验证只返回本人订单且按创建时间倒序。
func TestRepoListByUser(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()
	base := time.Now()

	// 先清掉这两个用户在库里已有的订单，把断言的前提「恰好 2 条」坐实。
	//
	// 为什么必须清：用例跑在事务里，这里的删除会随回滚一起还原，动不到真实数据；
	// 但不清的话，起点就取决于开发库里攒了什么 —— 手工测试留下的订单（user_id = 1）
	// 会让这条用例突然变成 3 条而失败，且只在「本地跑过业务」的机器上复现。
	// 用例不该依赖全局数据状态。
	if err := repo.db.WithContext(ctx).Exec("DELETE FROM orders WHERE user_id IN (1, 2)").Error; err != nil {
		t.Fatalf("清理预置数据失败: %v", err)
	}

	seed := []Order{
		{UserID: 1, ProductName: "较早的订单", Amount: 100, Status: StatusPaid, CreatedAt: base.Add(-2 * time.Hour)},
		{UserID: 1, ProductName: "较晚的订单", Amount: 200, Status: StatusPending, CreatedAt: base},
		{UserID: 2, ProductName: "他人订单", Amount: 300, Status: StatusPending, CreatedAt: base},
	}
	for i := range seed {
		if err := repo.Create(ctx, &seed[i]); err != nil {
			t.Fatalf("预置数据失败: %v", err)
		}
	}

	// 分页查询取全部两条（offset 0、limit 10），total 也应为 2。
	got, total, err := repo.ListByUser(ctx, 1, 0, 10)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 2 {
		t.Errorf("总数应为 2，实际 %d", total)
	}
	if len(got) != 2 {
		t.Fatalf("应只返回 user 1 的 2 条订单，实际 %d 条: %+v", len(got), got)
	}
	for _, o := range got {
		if o.UserID != 1 {
			t.Errorf("返回了其他用户的订单: %+v", o)
		}
	}
	if got[0].ProductName != "较晚的订单" {
		t.Errorf("应按创建时间倒序，首条应为「较晚的订单」，实际首条: %s", got[0].ProductName)
	}
}

// TestRepoListByUserPagination 验证分页的offset 与 limit 真的生效，
// 且翻页时 total 始终是「满足条件的总数」而不是「当页条数」。
func TestRepoListByUserPagination(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	if err := repo.db.WithContext(ctx).Exec("DELETE FROM orders WHERE user_id = 8").Error; err != nil {
		t.Fatalf("清理预置数据失败: %v", err)
	}

	base := time.Now().Truncate(time.Second)
	// 5 条订单，越早创建的越靠后（列表按 created_at DESC）。
	names := []string{"第1条", "第2条", "第3条", "第4条", "第5条"}
	for i, name := range names {
		o := Order{
			UserID:      8,
			ProductName: name,
			Amount:      100,
			Status:      StatusPending,
			CreatedAt:   base.Add(time.Duration(i-len(names)) * time.Hour),
		}
		if err := repo.Create(ctx, &o); err != nil {
			t.Fatalf("预置数据失败: %v", err)
		}
	}

	// 第1 页：offset 0、limit 2 → 应得「第5条」「第4条」，total 恒为 5。
	page1, total, err := repo.ListByUser(ctx, 8, 0, 2)
	if err != nil {
		t.Fatalf("查询第 1 页失败: %v", err)
	}
	if total != 5 {
		t.Errorf("total 应为 5，实际 %d", total)
	}
	if len(page1) != 2 {
		t.Fatalf("第 1 页应有 2 条，实际 %d 条", len(page1))
	}
	if page1[0].ProductName != "第5条" || page1[1].ProductName != "第4条" {
		t.Errorf("第 1 页内容应按创建时间倒序为「第5条」「第4条」，实际: %s、%s",
			page1[0].ProductName, page1[1].ProductName)
	}

	// 第 2 页：offset 2、limit 2 → 应得「第3条」「第2条」。
	page2, total2, err := repo.ListByUser(ctx, 8, 2, 2)
	if err != nil {
		t.Fatalf("查询第 2 页失败: %v", err)
	}
	if total2 != 5 {
		t.Errorf("翻页时 total 仍应为 5，实际 %d", total2)
	}
	if len(page2) != 2 {
		t.Fatalf("第 2 页应有 2 条，实际 %d 条", len(page2))
	}
	if page2[0].ProductName != "第3条" || page2[1].ProductName != "第2条" {
		t.Errorf("第 2 页内容应为「第3条」「第2条」，实际: %s、%s",
			page2[0].ProductName, page2[1].ProductName)
	}

	// 第 3 页：offset 4、limit 2 → 只剩 1 条，且不能因为 len==0 而报错。
	page3, _, err := repo.ListByUser(ctx, 8, 4, 2)
	if err != nil {
		t.Fatalf("查询第 3 页失败: %v", err)
	}
	if len(page3) != 1 || page3[0].ProductName != "第1条" {
		t.Errorf("第 3 页应只剩「第1条」，实际: %+v", page3)
	}

	// 越界页：offset 远超总数 → 返回空切片且不报错。
	page4, total4, err := repo.ListByUser(ctx, 8, 100, 2)
	if err != nil {
		t.Fatalf("越界页不应报错，实际: %v", err)
	}
	if len(page4) != 0 {
		t.Errorf("越界页应返回 0 条，实际 %d 条", len(page4))
	}
	if total4 != 5 {
		t.Errorf("越界页的 total 仍应为 5，实际 %d", total4)
	}
}

// TestRepoListAllPagination 验证管理端分页的 offset / limit 生效，且能跨用户取到数据。
//
// ⚠️ ListAll 查的是**全表**，开发库里天然带着别人提交的订单 ——
// 「总数恰好 6」「第一页恰好是这 6 条」这类绝对值断言在共享开发库上必然失败
// （用例跑在事务里，只能回滚自己写的行，撤不掉已提交的真实数据）。
// 所以这里换成「探针 + 相对断言」：
//   - 6 条探针的 created_at 取**未来时间**，DESC 排序下必在表头；
//   - total 用「插入前基线 + 6」做差，不猜库里原有的数据量。
func TestRepoListAllPagination(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	// 基线：插入探针前，全表已有多少条（含开发库里的既有数据）。
	_, baseTotal, err := repo.ListAll(ctx, 0, 1)
	if err != nil {
		t.Fatalf("读取基线总数失败: %v", err)
	}

	// 6 条探针分属 3 个用户（顺带覆盖「跨用户」），序号越大创建时间越晚。
	probeBase := time.Now().Add(time.Hour)
	for k := 1; k <= 6; k++ {
		o := Order{
			UserID:      uint(90 + k%3),
			ProductName: fmt.Sprintf("分页探针-%d", k),
			Amount:      100,
			Status:      StatusPending,
			CreatedAt:   probeBase.Add(time.Duration(k) * time.Minute),
		}
		if err := repo.Create(ctx, &o); err != nil {
			t.Fatalf("预置探针数据失败: %v", err)
		}
	}
	// DESC 排序下 6 条探针依次为：分页探针-6、-5、-4、-3、-2、-1。

	// 第 1 页（offset 0、limit 3）→ 应为最新的三条探针。
	page1, total, err := repo.ListAll(ctx, 0, 3)
	if err != nil {
		t.Fatalf("查询第 1 页失败: %v", err)
	}
	if total != baseTotal+6 {
		t.Errorf("total 应为基线 %d + 6 = %d，实际 %d", baseTotal, baseTotal+6, total)
	}
	assertProbeOrder(t, page1, []string{"分页探针-6", "分页探针-5", "分页探针-4"}, "第 1 页")

	// 第 2 页（offset 3、limit 3）→ 应为次新的三条探针，与第 1 页不重叠。
	page2, _, err := repo.ListAll(ctx, 3, 3)
	if err != nil {
		t.Fatalf("查询第 2 页失败: %v", err)
	}
	assertProbeOrder(t, page2, []string{"分页探针-3", "分页探针-2", "分页探针-1"}, "第 2 页")
}

// assertProbeOrder 断言一页订单的**开头**与给定的探针序列一一对应。
// 只比较前缀：ListAll 是全表查询，将来若有别的数据混进页尾也不能算失败。
func assertProbeOrder(t *testing.T, got []Order, want []string, label string) {
	t.Helper()
	if len(got) < len(want) {
		t.Fatalf("%s 应至少返回 %d 条（探针），实际 %d 条", label, len(want), len(got))
	}
	for i, name := range want {
		if got[i].ProductName != name {
			t.Errorf("%s 第 %d 条应为 %q，实际 %q（顺序或分页错位）", label, i+1, name, got[i].ProductName)
		}
	}
}

// TestRepoListAll 验证管理端能取到全部订单。
func TestRepoListAll(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		o := &Order{UserID: uint(i + 1), ProductName: "订单", Amount: 100, Status: StatusPending}
		if err := repo.Create(ctx, o); err != nil {
			t.Fatalf("预置数据失败: %v", err)
		}
	}

	// 分页查询取全部（limit 给足），并校验总数。
	got, total, err := repo.ListAll(ctx, 0, 100)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) < 3 {
		t.Errorf("应至少返回 3 条订单，实际 %d 条", len(got))
	}
	if total < 3 {
		t.Errorf("总数应至少为 3，实际 %d", total)
	}
}

// TestRepoUpdateStatus 验证合法流转能落库。
func TestRepoUpdateStatus(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	o := &Order{UserID: 3, ProductName: "待支付订单", Amount: 500, Status: StatusPending}
	if err := repo.Create(ctx, o); err != nil {
		t.Fatalf("预置数据失败: %v", err)
	}

	if err := repo.UpdateStatus(ctx, o.ID, StatusPending, StatusPaid); err != nil {
		t.Fatalf("合法流转应成功，实际报错: %v", err)
	}

	found, err := repo.FindByID(ctx, o.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if found.Status != StatusPaid {
		t.Errorf("状态应变为 %q，实际 %q", StatusPaid, found.Status)
	}
}

// TestRepoUpdateStatusStaleFrom 验证 CAS 语义：from 与实际状态不符时必须失败且不改数据。
// 这是防止并发重复流转（如两次支付）的关键。
func TestRepoUpdateStatusStaleFrom(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	o := &Order{UserID: 3, ProductName: "已支付订单", Amount: 500, Status: StatusPaid}
	if err := repo.Create(ctx, o); err != nil {
		t.Fatalf("预置数据失败: %v", err)
	}

	// 实际状态是 paid，却声称从 pending 流转，属于过期请求，必须拒绝。
	err := repo.UpdateStatus(ctx, o.ID, StatusPending, StatusPaid)
	if err == nil {
		t.Fatal("from 与实际状态不符时应返回错误（CAS 语义），实际返回 nil")
	}
	if !errors.Is(err, ErrStatusConflict) {
		t.Errorf("应返回哨兵错误 ErrStatusConflict，实际: %v", err)
	}

	found, err := repo.FindByID(ctx, o.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if found.Status != StatusPaid {
		t.Errorf("CAS 失败时不应改动状态，实际 %q", found.Status)
	}
}

// TestRepoUpdateStatusNotFound 验证订单不存在时流转失败。
func TestRepoUpdateStatusNotFound(t *testing.T) {
	repo := setupTestRepo(t)

	err := repo.UpdateStatus(context.Background(), 99999999, StatusPending, StatusPaid)
	if err == nil {
		t.Fatal("订单不存在时应返回错误，实际返回 nil")
	}
	if !errors.Is(err, ErrStatusConflict) {
		t.Errorf("应返回哨兵错误 ErrStatusConflict，实际: %v", err)
	}
}

// TestRepoPropagatesDatabaseError 验证底层数据库错误不会被吞掉。
// 用已取消的 context 让每个操作必然失败：只要返回 nil 即为漏报错误。
func TestRepoPropagatesDatabaseError(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := canceledCtx()

	t.Run("Create", func(t *testing.T) {
		err := repo.Create(ctx, &Order{UserID: 1, ProductName: "x", Amount: 1, Status: StatusPending})
		if err == nil {
			t.Error("数据库操作失败时 Create 必须返回错误，不能吞掉后返回 nil")
		}
	})

	t.Run("FindByID", func(t *testing.T) {
		if _, err := repo.FindByID(ctx, 1); err == nil {
			t.Error("数据库操作失败时 FindByID 必须返回错误，不能返回零值订单 + nil")
		}
	})

	t.Run("ListByUser", func(t *testing.T) {
		if _, _, err := repo.ListByUser(ctx, 1, 0, 10); err == nil {
			t.Error("数据库操作失败时 ListByUser 必须返回错误，不能返回空列表 + nil")
		}
	})

	t.Run("ListAll", func(t *testing.T) {
		if _, _, err := repo.ListAll(ctx, 0, 10); err == nil {
			t.Error("数据库操作失败时 ListAll 必须返回错误，不能返回空列表 + nil")
		}
	})

	t.Run("UpdateStatus", func(t *testing.T) {
		if err := repo.UpdateStatus(ctx, 1, StatusPending, StatusPaid); err == nil {
			t.Error("数据库操作失败时 UpdateStatus 必须返回错误")
		}
	})
}
