package user

import (
	"context"
	"errors"
	"os"
	"testing"

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
	if err := db.AutoMigrate(&User{}); err != nil {
		t.Fatalf("迁移 users 表失败: %v", err)
	}

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("开启事务失败: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	return NewRepository(tx)
}

// setupTestService 在仓库的基础上构造服务层，供跨层用例使用。
func setupTestService(t *testing.T) *Service {
	t.Helper()
	return NewService(setupTestRepo(t))
}

// canceledCtx 返回一个已取消的 context，用于让数据库操作必然失败，
// 借此验证错误是否被如实向上传递、有没有被伪装成业务错误。
func canceledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// TestRepoCreateDuplicateUsername 验证用户名冲突被翻译为包内哨兵错误。
func TestRepoCreateDuplicateUsername(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := context.Background()

	if err := repo.Create(ctx, &User{Name: "dup", PasswordHash: "x", Role: RoleUser}); err != nil {
		t.Fatalf("首次创建应成功，实际报错: %v", err)
	}

	err := repo.Create(ctx, &User{Name: "dup", PasswordHash: "y", Role: RoleUser})
	if err == nil {
		t.Fatal("重复用户名应返回错误")
	}
	if !errors.Is(err, ErrUsernameExists) {
		t.Errorf("应返回哨兵错误 ErrUsernameExists，实际: %v", err)
	}
}

// TestRepoFindByNameNotFound 验证「查无此人」返回包内哨兵而非 gorm 错误。
func TestRepoFindByNameNotFound(t *testing.T) {
	repo := setupTestRepo(t)

	_, err := repo.FindByName(context.Background(), "no-such-user")
	if err == nil {
		t.Fatal("查询不存在的用户应返回错误")
	}
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("应返回哨兵错误 ErrUserNotFound，实际: %v", err)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		t.Error("不应把 gorm 的错误透出到仓库层之外，否则 service 层被迫依赖 gorm")
	}
}

// TestRepoPropagatesDatabaseError 验证数据库故障如实上抛，且不被误判为业务错误。
func TestRepoPropagatesDatabaseError(t *testing.T) {
	repo := setupTestRepo(t)
	ctx := canceledCtx()

	_, err := repo.FindByName(ctx, "alice")
	if err == nil {
		t.Error("数据库操作失败时 FindByName 必须返回错误")
	} else if errors.Is(err, ErrUserNotFound) {
		t.Error("数据库故障不应被伪装成「用户不存在」")
	}

	if err := repo.Create(ctx, &User{Name: "alice", PasswordHash: "x", Role: RoleUser}); err == nil {
		t.Error("数据库操作失败时 Create 必须返回错误")
	}
}
