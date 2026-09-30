package user

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestExternalUsernameShape 锁定外部账号用户名的生成规则。
// 这条规则很容易写歪：套用用户自设用户名的 16 字符上限，长 userid 的账号就建不出来；
// 而完全不截断又可能顶到数据库列宽。因此用一组边界值把它钉住。
func TestExternalUsernameShape(t *testing.T) {
	cases := []struct {
		name       string
		externalID string
		attempt    int
		want       string
	}{
		{"普通标识", "ZengMaiKuan", 0, "wecom_ZengMaiKuan"},
		{"特殊字符被清洗", "zeng.mai_kuan@corp", 0, "wecom_zeng-mai-kuan-corp"},
		{"冲突重试换候选名", "ZengMaiKuan", 1, "wecom_ZengMaiKuan-2"},
		{"超长标识被截断", strings.Repeat("a", 80), 0, "wecom_" + strings.Repeat("a", 42)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := externalUsername(ProviderWecom, tc.externalID, tc.attempt)
			if got != tc.want {
				t.Errorf("期望 %q，实际 %q", tc.want, got)
			}
			// 上限 48 字符，加最短后缀后仍应落在 users.name 的列宽（64）之内。
			if n := len([]rune(got)); n > externalUsernameMaxLen {
				t.Errorf("用户名长度 %d 超出上限 %d", n, externalUsernameMaxLen)
			}
		})
	}
}

// TestServiceLoginByExternalValidatesInput 覆盖入参校验。
// 校验发生在触库之前，因此这里用空仓库即可，无需数据库也能跑。
func TestServiceLoginByExternalValidatesInput(t *testing.T) {
	service := NewService(nil)
	ctx := context.Background()

	if _, err := service.LoginByExternal(ctx, "github", "u1"); !errors.Is(err, ErrInvalidProvider) {
		t.Errorf("未支持的提供方应返回 ErrInvalidProvider，实际 %v", err)
	}
	if _, err := service.LoginByExternal(ctx, ProviderWecom, ""); !errors.Is(err, ErrInvalidExternalID) {
		t.Errorf("空外部标识应返回 ErrInvalidExternalID，实际 %v", err)
	}
	if err := service.BindExternal(ctx, 1, "github", "u1"); !errors.Is(err, ErrInvalidProvider) {
		t.Errorf("绑定：未支持的提供方应返回 ErrInvalidProvider，实际 %v", err)
	}
	if err := service.BindExternal(ctx, 1, ProviderWecom, ""); !errors.Is(err, ErrInvalidExternalID) {
		t.Errorf("绑定：空外部标识应返回 ErrInvalidExternalID，实际 %v", err)
	}
}

// TestIdentityProviderValid 锁定提供方取值集合。
func TestIdentityProviderValid(t *testing.T) {
	if !ProviderWecom.Valid() {
		t.Error("wecom 应是合法的提供方")
	}
	for _, p := range []IdentityProvider{"", "wechat", "feishu", "WECOM"} {
		if p.Valid() {
			t.Errorf("%q 不应被判为合法提供方", p)
		}
	}
}

// uniqueExternalID 生成一个本次运行独有的外部标识，供会写入身份的用例使用。
//
// 为什么不能用固定的 "ZengMaiKuan"：用例跑在事务里，回滚只能撤掉自己写的行，
// 撤不掉**已经提交进开发库的数据** —— 唯一约束是按已提交的数据判定的。开发库里存在
// 手工扫码登录留下的真实绑定时，固定标识会让「自动建号」「绑定」这两类用例直接撞唯一冲突，
// 且只在跑过真实登录的机器上复现。用例不该依赖开发库的既有状态。
func uniqueExternalID(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
}

// TestServiceLoginByExternalProvisionsUnusableAccount 覆盖首次登录自动建号，
// 并锁定最要紧的一条安全性质：自动建出的账号不能成为绕过密码的入口。
func TestServiceLoginByExternalProvisionsUnusableAccount(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()
	externalID := uniqueExternalID(t)

	created, err := service.LoginByExternal(ctx, ProviderWecom, externalID)
	if err != nil {
		t.Fatalf("首次外部登录应自动建号，实际报错: %v", err)
	}
	if created.Role != RoleUser {
		t.Errorf("自动建号的角色应为普通用户，实际 %q", created.Role)
	}
	if want := externalUsername(ProviderWecom, externalID, 0); created.Name != want {
		t.Errorf("用户名期望 %q，实际 %q", want, created.Name)
	}
	if created.PasswordHash != unusablePasswordHash {
		t.Errorf("外部账号应写入不可用的密码占位值，实际 %q", created.PasswordHash)
	}

	for _, password := range []string{"", "!", created.Name, "123456"} {
		if _, err := service.Login(ctx, created.Name, password); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("外部账号不应能用密码 %q 登录，实际错误 %v", password, err)
		}
	}
}

// TestServiceLoginByExternalIsIdempotent 覆盖二次登录：不得重复建号，且必须回到同一账号。
func TestServiceLoginByExternalIsIdempotent(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()
	externalID := uniqueExternalID(t)

	first, err := service.LoginByExternal(ctx, ProviderWecom, externalID)
	if err != nil {
		t.Fatalf("首次外部登录失败: %v", err)
	}
	second, err := service.LoginByExternal(ctx, ProviderWecom, externalID)
	if err != nil {
		t.Fatalf("二次外部登录失败: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("二次登录应返回同一账号：首次 %d，二次 %d", first.ID, second.ID)
	}

	identity, err := service.repo.FindIdentity(ctx, ProviderWecom, externalID)
	if err != nil {
		t.Fatalf("查询绑定关系失败: %v", err)
	}
	if identity.UserID != first.ID {
		t.Errorf("绑定关系应指向首次建出的账号，实际 %d", identity.UserID)
	}
}

// TestServiceLoginByExternalRetriesOnNameConflict 覆盖用户名冲突时换候选名重试。
//
// 自然触发很难（企微 userid 在单个企业内唯一），但公开注册接口完全可能先把同名账号占掉。
// 注意这里用仓库层直接占位，而不是走 Register —— 系统生成的用户名不受「用户自设用户名
// 1~16 字符」那条规则约束，正好也验证了自动建号绕开该校验的必要性。
func TestServiceLoginByExternalRetriesOnNameConflict(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()
	externalID := uniqueExternalID(t)

	occupied := &User{
		Name:         externalUsername(ProviderWecom, externalID, 0),
		PasswordHash: "x",
		Role:         RoleUser,
	}
	if err := service.repo.Create(ctx, occupied); err != nil {
		t.Fatalf("预占用户名失败: %v", err)
	}

	created, err := service.LoginByExternal(ctx, ProviderWecom, externalID)
	if err != nil {
		t.Fatalf("用户名冲突时应换候选名重试，实际报错: %v", err)
	}
	if want := externalUsername(ProviderWecom, externalID, 1); created.Name != want {
		t.Errorf("冲突后应使用第二个候选名 %q，实际 %q", want, created.Name)
	}
	if created.ID == occupied.ID {
		t.Error("重试后应新建账号，而不是复用占位的那个")
	}
}

// TestServiceBindExternal 覆盖绑定的正常、幂等与冲突三条路径，
// 并验证绑定后外部登录确实落到被绑定的账号（而不是新建一个）。
func TestServiceBindExternal(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()
	externalID := uniqueExternalID(t)

	alice, err := service.Register(ctx, "alice", "pass1234", RoleUser)
	if err != nil {
		t.Fatalf("注册 alice 失败: %v", err)
	}
	bob, err := service.Register(ctx, "bob", "pass1234", RoleUser)
	if err != nil {
		t.Fatalf("注册 bob 失败: %v", err)
	}

	if err := service.BindExternal(ctx, alice.ID, ProviderWecom, externalID); err != nil {
		t.Fatalf("绑定应成功，实际报错: %v", err)
	}
	// 幂等：重复绑定同一身份视为成功，避免用户重复点击时拿到无意义的冲突错误。
	if err := service.BindExternal(ctx, alice.ID, ProviderWecom, externalID); err != nil {
		t.Errorf("重复绑定同一身份应幂等成功，实际报错: %v", err)
	}
	// 冲突：同一个企微身份不能再绑到别人身上。
	if err := service.BindExternal(ctx, bob.ID, ProviderWecom, externalID); !errors.Is(err, ErrExternalIdentityExists) {
		t.Errorf("身份已被占用应返回 ErrExternalIdentityExists，实际 %v", err)
	}
	// 绑定后再登录，应进入 alice 而不是新建账号。
	loggedIn, err := service.LoginByExternal(ctx, ProviderWecom, externalID)
	if err != nil {
		t.Fatalf("绑定后外部登录失败: %v", err)
	}
	if loggedIn.ID != alice.ID {
		t.Errorf("绑定后应登录到账号 %d，实际 %d", alice.ID, loggedIn.ID)
	}
	// 账号不存在时不得留下指向不存在用户的绑定记录。
	if err := service.BindExternal(ctx, 999999, ProviderWecom, "ghost"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("绑定到不存在的账号应返回 ErrUserNotFound，实际 %v", err)
	}
}

// setupIsolatedDB 打开一个独立于测试事务的连接池，供并发用例使用。
//
// 为什么不复用 setupTestService：那个仓库跑在单个事务里，而并发用例需要多条真实连接，
// 才能让「两个请求同时插入同一外部身份」真的发生；多条 goroutine 挤在同一条连接上
// 互相等锁，反而会让竞争窗口消失，测出来的绿是假的。
//
// 代价是数据不受事务回滚保护，因此用例必须使用唯一的外部标识，并在结束时按标识精确清理。
func setupIsolatedDB(t *testing.T) *gorm.DB {
	t.Helper()

	if err := godotenv.Load("../../.env"); err != nil && !os.IsNotExist(err) {
		t.Fatalf("加载 .env 失败: %v", err)
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("未配置 DATABASE_URL，跳过并发用例")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		TranslateError: true,
		Logger:         logger.Default.LogMode(logger.Error),
	})
	if err != nil {
		t.Fatalf("连接数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&User{}, &UserIdentity{}); err != nil {
		t.Fatalf("迁移表结构失败: %v", err)
	}
	return db
}

// cleanupExternalAccount 按外部标识清理并发用例留下的数据。
// 自动建号的用户名由外部标识推导（见 externalUsername），因此可以精确删除。
func cleanupExternalAccount(t *testing.T, db *gorm.DB, externalID string) {
	t.Helper()
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM user_identities WHERE external_id = ?", externalID).Error
		_ = db.Exec("DELETE FROM users WHERE name LIKE ?", string(ProviderWecom)+"_"+externalID+"%").Error
	})
}

// TestServiceLoginByExternalIsConcurrentSafe 锁定并发下的建号唯一性。
//
// 这是真实存在的场景：用户在扫码页连点、或前端在 StrictMode 下重复发请求，
// 同一瞬间会有多个请求带着同一个 externalID 进来。若没有唯一约束与冲突兜底，
// 就会建出多个账号，且该身份只绑到其中一个 —— 用户下次登录可能落到另一个空账号上。
func TestServiceLoginByExternalIsConcurrentSafe(t *testing.T) {
	db := setupIsolatedDB(t)
	service := NewService(NewRepository(db))

	// 唯一标识：既不与库中既有数据冲突，也保证本用例重跑时互不干扰。
	externalID := fmt.Sprintf("race-%d", time.Now().UnixNano())
	cleanupExternalAccount(t, db, externalID)

	const workers = 10
	users := make([]*User, workers)
	errs := make([]error, workers)

	// 用一个起跑信号让请求尽量同时到达，放大竞争窗口。
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			users[idx], errs[idx] = service.LoginByExternal(context.Background(), ProviderWecom, externalID)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("第 %d 个并发请求失败: %v", i, err)
		}
	}

	wantID := users[0].ID
	for i, u := range users {
		if u.ID != wantID {
			t.Errorf("并发建号产生了多个账号：第 0 个为 %d，第 %d 个为 %d", wantID, i, u.ID)
		}
	}

	var identityCount int64
	if err := db.Model(&UserIdentity{}).
		Where("provider = ? AND external_id = ?", ProviderWecom, externalID).
		Count(&identityCount).Error; err != nil {
		t.Fatalf("统计绑定关系失败: %v", err)
	}
	if identityCount != 1 {
		t.Errorf("同一外部身份应只产生 1 行绑定关系，实际 %d 行", identityCount)
	}

	var accountCount int64
	if err := db.Model(&User{}).
		Where("name LIKE ?", string(ProviderWecom)+"_"+externalID+"%").
		Count(&accountCount).Error; err != nil {
		t.Fatalf("统计账号失败: %v", err)
	}
	if accountCount != 1 {
		t.Errorf("并发建号应只产生 1 个账号，实际 %d 个", accountCount)
	}
}
