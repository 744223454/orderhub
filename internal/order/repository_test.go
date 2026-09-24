package order

import (
	"context"
	"errors"
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

	got, err := repo.ListByUser(ctx, 1)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
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

	got, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) < 3 {
		t.Errorf("应至少返回 3 条订单，实际 %d 条", len(got))
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
		if _, err := repo.ListByUser(ctx, 1); err == nil {
			t.Error("数据库操作失败时 ListByUser 必须返回错误，不能返回空列表 + nil")
		}
	})

	t.Run("ListAll", func(t *testing.T) {
		if _, err := repo.ListAll(ctx); err == nil {
			t.Error("数据库操作失败时 ListAll 必须返回错误，不能返回空列表 + nil")
		}
	})

	t.Run("UpdateStatus", func(t *testing.T) {
		if err := repo.UpdateStatus(ctx, 1, StatusPending, StatusPaid); err == nil {
			t.Error("数据库操作失败时 UpdateStatus 必须返回错误")
		}
	})
}
