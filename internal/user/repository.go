package user

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// Repo 提供用户数据的持久化访问能力。
type Repo struct {
	db *gorm.DB
}

// NewRepository 创建用户仓库，db 为已初始化并完成迁移的数据库连接。
func NewRepository(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// Create 写入一条用户记录。
// 用户名冲突返回 ErrUsernameExists；gorm 的错误在本层翻译，不再向上泄露。
func (r *Repo) Create(ctx context.Context, user *User) error {
	if err := gorm.G[User](r.db).Create(ctx, user); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrUsernameExists
		}
		return fmt.Errorf("创建用户失败: %w", err)
	}
	return nil
}

// FindByName 按用户名查询用户。
// 用户不存在时返回 ErrUserNotFound，便于调用方与数据库故障区分处理。
func (r *Repo) FindByName(ctx context.Context, name string) (*User, error) {
	user, err := gorm.G[User](r.db).
		Where("name = ?", name).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("按用户名查询用户失败: %w", err)
	}
	return &user, nil
}

// FindByID 按主键查询用户。
// 用户不存在时返回 ErrUserNotFound。
func (r *Repo) FindByID(ctx context.Context, id uint) (*User, error) {
	user, err := gorm.G[User](r.db).
		Where("id = ?", id).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("按 ID 查询用户失败: %w", err)
	}
	return &user, nil
}

// FindIdentity 按提供方与外部标识查询绑定关系。
// 未绑定时返回 ErrIdentityNotFound，便于调用方与数据库故障区分处理。
func (r *Repo) FindIdentity(ctx context.Context, provider IdentityProvider, externalID string) (*UserIdentity, error) {
	identity, err := gorm.G[UserIdentity](r.db).
		Where("provider = ?", provider).
		Where("external_id = ?", externalID).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrIdentityNotFound
		}
		return nil, fmt.Errorf("按外部身份查询绑定关系失败: %w", err)
	}
	return &identity, nil
}

// CreateIdentity 写入一条绑定关系。
// 该外部身份已被占用时返回 ErrExternalIdentityExists。
func (r *Repo) CreateIdentity(ctx context.Context, identity *UserIdentity) error {
	if err := gorm.G[UserIdentity](r.db).Create(ctx, identity); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrExternalIdentityExists
		}
		return fmt.Errorf("绑定外部身份失败: %w", err)
	}
	return nil
}

// CreateWithIdentity 在同一个事务里创建用户并绑定外部身份。
//
// 两步必须同事务：否则中途失败会留下一个没有任何登录方式的孤儿账号。
// 两处写入各自的唯一约束冲突被分别翻译成 ErrUsernameExists 与 ErrExternalIdentityExists，
// 调用方无需解析数据库错误，就能区分「用户名撞了」还是「该外部身份已被占用」——
// 前者应换一个候选用户名重试，后者说明并发下已有请求建好了号。
func (r *Repo) CreateWithIdentity(ctx context.Context, user *User, identity *UserIdentity) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := gorm.G[User](tx).Create(ctx, user); err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrUsernameExists
			}
			return fmt.Errorf("创建用户失败: %w", err)
		}
		// 用户已落库并回填了自增主键，绑定关系据此关联。
		identity.UserID = user.ID
		if err := gorm.G[UserIdentity](tx).Create(ctx, identity); err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrExternalIdentityExists
			}
			return fmt.Errorf("绑定外部身份失败: %w", err)
		}
		return nil
	})
}

// WithinTx 在单个事务内执行 fn，并把绑定到该事务的仓库传给它。
//
// 供「先检查再修改」这类不能被打断的组合操作使用：两步必须处在同一个事务里，
// 否则中间会被其他请求插进来，检查就白做了 —— 见 LockAdmins 的说明。
func (r *Repo) WithinTx(ctx context.Context, fn func(tx *Repo) error) error {
	return r.db.WithContext(ctx).Transaction(func(gdb *gorm.DB) error {
		return fn(&Repo{db: gdb})
	})
}

// ListUsers 按创建时间倒序分页查询用户，并返回满足条件的总数。
//
// keyword 非空时按用户名模糊匹配；若它同时是纯数字，也匹配用户 ID —— 管理员往往是拿着一个
// ID 来找人的。注意 LIKE 的通配符必须转义：用户名允许出现 % 与 _，不转义的话搜一个 % 会把
// 整张表捞出来，搜 a_b 会顺带匹配到 axb。
func (r *Repo) ListUsers(ctx context.Context, keyword string, offset, limit int) ([]User, int64, error) {
	condition, args := userFilter(keyword)

	// 两个链分开构造：同一个链上既执行 Count 又执行 Find 会共享同一份 statement，
	// 后一次调用看得见前一次留下的痕迹。多写一行换来的是不必去推理这类耦合。
	countChain := gorm.G[User](r.db).Where(condition, args...)
	total, err := countChain.Count(ctx, "*")
	if err != nil {
		return nil, 0, fmt.Errorf("统计用户总数失败: %w", err)
	}

	listChain := gorm.G[User](r.db).Where(condition, args...)
	users, err := listChain.Order("created_at DESC").Offset(offset).Limit(limit).Find(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("查询用户列表失败: %w", err)
	}
	return users, total, nil
}

// userFilter 构造用户列表的过滤条件。
// 始终返回非空条件，调用方因此不必为「有没有关键字」写两条链式调用。
func userFilter(keyword string) (string, []any) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return "1 = 1", nil
	}

	pattern := "%" + escapeLikePattern(keyword) + "%"
	if id, err := strconv.ParseUint(keyword, 10, 64); err == nil {
		return "(id = ? OR name ILIKE ? ESCAPE '\\')", []any{id, pattern}
	}
	return "(name ILIKE ? ESCAPE '\\')", []any{pattern}
}

// escapeLikePattern 转义 LIKE 模式中的通配符。
// 反斜杠必须排在替换表最前：否则后面新插入的转义符会被再转义一遍。
func escapeLikePattern(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// UpdateRole 修改指定用户的角色。
// 用户不存在时返回 ErrUserNotFound。
func (r *Repo) UpdateRole(ctx context.Context, id uint, role Role) error {
	rows, err := gorm.G[User](r.db).
		Where("id = ?", id).
		Update(ctx, "role", role)
	if err != nil {
		return fmt.Errorf("更新用户角色失败: %w", err)
	}
	if rows == 0 {
		// PostgreSQL 即使新值与旧值相同也会报告更新成功，因此 0 行只可能是这个 id 不存在。
		return ErrUserNotFound
	}
	return nil
}

// LockAdmins 锁定全部管理员账号并返回数量。
//
// 为什么必须加锁，而不是「先 COUNT 再 UPDATE」：两条独立语句之间存在竞态窗口 ——
// 两个并发请求可能各自读到 2 个管理员、各自把一个降级，结果一个不剩。管理员一个都不剩
// 就意味着再没有人能进入用户管理把它改回来，只能手工改库。
// FOR UPDATE 让并发的角色变更在这里排队；后到的那个在锁释放后重新读，会看到已被改小的数量。
func (r *Repo) LockAdmins(ctx context.Context) (int64, error) {
	var ids []uint
	if err := gorm.G[User](r.db).
		Raw("SELECT id FROM users WHERE role = ? FOR UPDATE", RoleAdmin).
		Scan(ctx, &ids); err != nil {
		return 0, fmt.Errorf("锁定管理员账号失败: %w", err)
	}
	return int64(len(ids)), nil
}

// ListIdentitiesByUserIDs 批量查询这些用户的外部身份绑定关系，按 id 升序。
// 传入空切片时直接返回，避免生成 `IN ()` 这种语法错误的 SQL。
func (r *Repo) ListIdentitiesByUserIDs(ctx context.Context, userIDs []uint) ([]UserIdentity, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

	identities, err := gorm.G[UserIdentity](r.db).
		Where("user_id IN ?", userIDs).
		Order("id ASC").
		Find(ctx)
	if err != nil {
		return nil, fmt.Errorf("批量查询外部身份失败: %w", err)
	}
	return identities, nil
}

// DeleteIdentity 解绑指定用户在某个提供方下的外部身份。
// 该绑定关系不存在时返回 ErrIdentityNotFound。
func (r *Repo) DeleteIdentity(ctx context.Context, userID uint, provider IdentityProvider) error {
	rows, err := gorm.G[UserIdentity](r.db).
		Where("user_id = ?", userID).
		Where("provider = ?", provider).
		Delete(ctx)
	if err != nil {
		return fmt.Errorf("解绑外部身份失败: %w", err)
	}
	if rows == 0 {
		return ErrIdentityNotFound
	}
	return nil
}
