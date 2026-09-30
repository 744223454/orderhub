package user

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

// uniqueSuffix 生成用例内唯一的标识后缀，用于把本用例造的数据与库中既有数据区分开。
//
// 仓库层用例跑在事务里、随回滚还原，但**事务内的查询仍看得见库里已提交的数据**——
// 开发库里攒着十来个账号，因此断言「总数等于几」这类写法必然会随环境而变化。
// 统一给名字带上唯一后缀、按后缀过滤，断言才能稳定。
func uniqueSuffix() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

// TestHasUsablePassword 锁定「有没有密码」的判定。
// 企业微信自动建号写入的是占位哈希，必须被判为「没有密码」，否则用户管理页会误显示成有密码账号。
func TestHasUsablePassword(t *testing.T) {
	cases := []struct {
		name string
		hash string
		want bool
	}{
		{"空哈希", "", false},
		{"外部建号的占位哈希", unusablePasswordHash, false},
		{"正常的 bcrypt 哈希", "$2a$10$abcdefghijklmnopqrstuv", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasUsablePassword(&User{PasswordHash: tc.hash}); got != tc.want {
				t.Errorf("hash=%q 期望 %v，实际 %v", tc.hash, tc.want, got)
			}
		})
	}
}

// TestServiceListUsersPagination 覆盖分页：总数是过滤后的总数，页大小按参数生效。
func TestServiceListUsersPagination(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()
	keyword := "page" + uniqueSuffix()

	for i := 0; i < 3; i++ {
		name := keyword + "-" + strconv.Itoa(i)
		if err := service.repo.Create(ctx, &User{Name: name, PasswordHash: "x", Role: RoleUser}); err != nil {
			t.Fatalf("预置用户 %s 失败: %v", name, err)
		}
	}

	first, err := service.ListUsers(ctx, keyword, 1, 2)
	if err != nil {
		t.Fatalf("查询第一页失败: %v", err)
	}
	if first.Total != 3 {
		t.Errorf("总数应为 3，实际 %d", first.Total)
	}
	if len(first.Users) != 2 {
		t.Errorf("第一页应有 2 条，实际 %d", len(first.Users))
	}
	if first.Page != 1 || first.PageSize != 2 {
		t.Errorf("生效后的分页参数应为 page=1 pageSize=2，实际 page=%d pageSize=%d", first.Page, first.PageSize)
	}

	second, err := service.ListUsers(ctx, keyword, 2, 2)
	if err != nil {
		t.Fatalf("查询第二页失败: %v", err)
	}
	if len(second.Users) != 1 {
		t.Errorf("第二页应有 1 条，实际 %d", len(second.Users))
	}
}

// TestServiceListUsersNormalizesPaging 锁定「非法分页参数回落到默认值而不是报错」。
func TestServiceListUsersNormalizesPaging(t *testing.T) {
	service := setupTestService(t)

	cases := []struct {
		name         string
		page, size   int
		wantPage     int
		wantPageSize int
	}{
		{"页码为 0", 0, 20, 1, 20},
		{"页码为负", -3, 20, 1, 20},
		{"页大小超上限", 1, 9999, 1, maxUserPageSize},
		{"页大小为零", 1, 0, 1, defaultUserPageSize},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := service.ListUsers(context.Background(), "", tc.page, tc.size)
			if err != nil {
				t.Fatalf("查询失败: %v", err)
			}
			if page.Page != tc.wantPage || page.PageSize != tc.wantPageSize {
				t.Errorf("期望 page=%d pageSize=%d，实际 page=%d pageSize=%d",
					tc.wantPage, tc.wantPageSize, page.Page, page.PageSize)
			}
		})
	}
}

// TestServiceListUsersEscapesLikeWildcards 锁定 LIKE 通配符转义。
//
// 用户名允许出现 % 与 _（注册接口只校验长度），因此关键字里的这两个字符必须当字面量处理。
// 不转义的话，搜一个 % 会把整张表捞出来 —— 而且这种错误在数据量小的时候完全看不出来。
func TestServiceListUsersEscapesLikeWildcards(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()
	suffix := uniqueSuffix()

	// 一对结构相似的名字：一个真的含通配符字符，一个只含形近的普通字符。
	cases := []struct {
		name     string
		literal  string
		decoy    string
		wildcard string
	}{
		{"百分号", "esc" + suffix + "%-a", "esc" + suffix + "b-a", "%"},
		{"下划线", "esc" + suffix + "_-a", "esc" + suffix + "b-a2", "_"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{tc.literal, tc.decoy} {
				if err := service.repo.Create(ctx, &User{Name: name, PasswordHash: "x", Role: RoleUser}); err != nil {
					t.Fatalf("预置用户 %s 失败: %v", name, err)
				}
			}

			page, err := service.ListUsers(ctx, "esc"+suffix+tc.wildcard, 1, 50)
			if err != nil {
				t.Fatalf("查询失败: %v", err)
			}
			if page.Total != 1 {
				t.Errorf("通配符 %q 应被当作字面量，只命中 1 条，实际命中 %d 条", tc.wildcard, page.Total)
			}
			if len(page.Users) == 1 && page.Users[0].Name != tc.literal {
				t.Errorf("应命中 %q，实际命中 %q", tc.literal, page.Users[0].Name)
			}
		})
	}
}

// TestServiceListUsersMatchesNumericKeywordByID 锁定「纯数字关键字同时按 ID 匹配」。
func TestServiceListUsersMatchesNumericKeywordByID(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()

	created := &User{Name: "num" + uniqueSuffix(), PasswordHash: "x", Role: RoleUser}
	if err := service.repo.Create(ctx, created); err != nil {
		t.Fatalf("预置用户失败: %v", err)
	}

	page, err := service.ListUsers(ctx, strconv.FormatUint(uint64(created.ID), 10), 1, 50)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total < 1 {
		t.Fatal("按 ID 搜索应至少命中刚创建的那一条")
	}
	found := false
	for _, u := range page.Users {
		if u.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("结果中应包含 ID=%d 的用户", created.ID)
	}
}

// TestServiceUpdateRolePromotesAndIsIdempotent 覆盖改角色的正常路径与幂等。
func TestServiceUpdateRolePromotesAndIsIdempotent(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()

	created := &User{Name: "role" + uniqueSuffix(), PasswordHash: "x", Role: RoleUser}
	if err := service.repo.Create(ctx, created); err != nil {
		t.Fatalf("预置用户失败: %v", err)
	}

	if err := service.UpdateRole(ctx, created.ID, RoleOps); err != nil {
		t.Fatalf("提升为运营应成功，实际报错: %v", err)
	}
	after, err := service.FindUserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("回读用户失败: %v", err)
	}
	if after.Role != RoleOps {
		t.Errorf("角色应为 %q，实际 %q", RoleOps, after.Role)
	}

	// 幂等：把同一个人再设成同一个角色不该报错。
	if err := service.UpdateRole(ctx, created.ID, RoleOps); err != nil {
		t.Errorf("重复设置同一角色应幂等成功，实际报错: %v", err)
	}
}

// TestServiceUpdateRoleValidatesInput 覆盖入参校验与用户不存在。
func TestServiceUpdateRoleValidatesInput(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()

	if err := service.UpdateRole(ctx, 1, "superuser"); !errors.Is(err, ErrInvalidRole) {
		t.Errorf("非法角色应返回 ErrInvalidRole，实际 %v", err)
	}
	if err := service.UpdateRole(ctx, 999999, RoleUser); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("用户不存在应返回 ErrUserNotFound，实际 %v", err)
	}
}

// TestServiceUpdateRoleRejectsLastAdmin 锁定「不能降级最后一个管理员」。
//
// 构造方式：用独立连接开一个外层事务，在其中把除目标外的管理员全部降级，
// 使本事务内只剩它一个管理员，再调用服务层。
// 这样既造出了「最后一个」的局面，又完全不动真实数据 —— 事务结束时回滚。
// 之所以需要独立连接：共享事务的用例里，库中既有的那几个管理员是看不见也改不动的。
func TestServiceUpdateRoleRejectsLastAdmin(t *testing.T) {
	db := setupIsolatedDB(t)
	ctx := context.Background()

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("开启事务失败: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	// 服务层绑定到这个事务上，它内部再开的事务会退化成 savepoint，仍处在同一事务内。
	service := NewService(NewRepository(tx))

	only := &User{Name: "lastadmin" + uniqueSuffix(), PasswordHash: "x", Role: RoleAdmin}
	if err := service.repo.Create(ctx, only); err != nil {
		t.Fatalf("预置管理员失败: %v", err)
	}

	// 把其他管理员在本事务内降级，制造「只剩一个管理员」的局面。
	if err := tx.Exec("UPDATE users SET role = ? WHERE role = ? AND id <> ?", RoleUser, RoleAdmin, only.ID).Error; err != nil {
		t.Fatalf("构造前置状态失败: %v", err)
	}

	err := service.UpdateRole(ctx, only.ID, RoleUser)
	if !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("降级最后一个管理员应返回 ErrLastAdmin，实际 %v", err)
	}

	after, err := service.FindUserByID(ctx, only.ID)
	if err != nil {
		t.Fatalf("回读用户失败: %v", err)
	}
	if after.Role != RoleAdmin {
		t.Errorf("被拒绝后角色不应发生变化，实际 %q", after.Role)
	}

	// 反向确认：此时把它提成同一个角色（无变化）是幂等成功的，
	// 说明拒绝只发生在「真的要把管理员降下去」这一种情况。
	if err := service.UpdateRole(ctx, only.ID, RoleAdmin); err != nil {
		t.Errorf("重复设置为管理员应幂等成功，实际 %v", err)
	}
}

// TestServiceUnbindIdentity 覆盖解绑的正常路径与三种失败情形。
func TestServiceUnbindIdentity(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()

	externalID := "unbind" + uniqueSuffix()
	u, err := service.LoginByExternal(ctx, ProviderWecom, externalID)
	if err != nil {
		t.Fatalf("外部登录建号失败: %v", err)
	}

	if err := service.UnbindIdentity(ctx, u.ID, ProviderWecom); err != nil {
		t.Fatalf("解绑应成功，实际报错: %v", err)
	}
	// 已经解掉了再解一次，要说清楚是「没绑过」而不是静默成功。
	if err := service.UnbindIdentity(ctx, u.ID, ProviderWecom); !errors.Is(err, ErrIdentityNotFound) {
		t.Errorf("重复解绑应返回 ErrIdentityNotFound，实际 %v", err)
	}
	// 解绑之后该外部身份不再绑定任何账号，再次外部登录会新建一个账号。
	rebound, err := service.LoginByExternal(ctx, ProviderWecom, externalID)
	if err != nil {
		t.Fatalf("解绑后再登录应重新建号，实际报错: %v", err)
	}
	if rebound.ID == u.ID {
		t.Error("解绑后重新登录应建出新账号，而不是回到原账号")
	}

	if err := service.UnbindIdentity(ctx, 999999, ProviderWecom); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("用户不存在应返回 ErrUserNotFound，实际 %v", err)
	}
	if err := service.UnbindIdentity(ctx, u.ID, "github"); !errors.Is(err, ErrInvalidProvider) {
		t.Errorf("未支持的提供方应返回 ErrInvalidProvider，实际 %v", err)
	}
}

// TestServiceListIdentitiesGroupsByUser 覆盖批量查询按用户分组，以及空输入的安全处理。
func TestServiceListIdentitiesGroupsByUser(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()

	externalID := "group" + uniqueSuffix()
	u, err := service.LoginByExternal(ctx, ProviderWecom, externalID)
	if err != nil {
		t.Fatalf("外部登录建号失败: %v", err)
	}

	grouped, err := service.ListIdentities(ctx, []uint{u.ID})
	if err != nil {
		t.Fatalf("批量查询失败: %v", err)
	}
	bound := grouped[u.ID]
	if len(bound) != 1 || bound[0].Provider != ProviderWecom || bound[0].ExternalID != externalID {
		t.Errorf("应查到 1 条 wecom 绑定，实际 %+v", bound)
	}

	// 空输入不能生成 `IN ()` —— 那是一条语法错误的 SQL。
	empty, err := service.ListIdentities(ctx, nil)
	if err != nil {
		t.Fatalf("空输入不应报错，实际 %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("空输入应返回空结果，实际 %+v", empty)
	}
}
