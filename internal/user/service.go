package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// 账号与密码的长度限制，按字符数计（非字节数，避免中文用户名长度被误判）。
const (
	usernameMinLen = 1
	usernameMaxLen = 16
	passwordMinLen = 8
	passwordMaxLen = 16
)

// unusablePasswordHash 是外部身份自动建号时写入 users.password_hash 的占位值。
//
// users.password_hash 是 not null，必须有个值；但外部账号本就没有密码，于是写入这个
// 不是合法 bcrypt 哈希的字符串——比对必然失败，外部账号因而无法用密码登录。
// 刻意不用空串：让库表里一眼能看出「此账号没有密码」，而不是看着像数据缺失。
const unusablePasswordHash = "!"

// externalUsernameMaxLen 是外部账号自动生成用户名的长度上限。
//
// 与用户自设用户名的 usernameMaxLen(16) 是两回事：后者约束的是「人取的名字」，
// 而外部账号的用户名是系统生成的标签、不参与登录。若套用 16 字符上限，
// 形如 wecom_ZengMaiKuan 的账号会被直接挡在门外。这里只给数据库列宽（64）留出余量，
// 以便用户名冲突时还能追加 -2、-3 这样的后缀。
const externalUsernameMaxLen = 48

// externalUsernameAttempts 是自动建号时用户名冲突的最大重试次数。
const externalUsernameAttempts = 5

// Service 承载用户模块的业务逻辑。
type Service struct {
	repo *Repo
}

// NewService 创建用户服务，repo 为用户数据仓库。
func NewService(repo *Repo) *Service {
	return &Service{repo: repo}
}

// Register 创建新用户，密码经 bcrypt 哈希后落库，返回创建成功的用户。
// 用户名冲突返回 ErrUsernameExists；其余技术错误如实上抛。
func (s *Service) Register(ctx context.Context, name, password string, role Role) (*User, error) {
	if !role.Valid() {
		return nil, ErrInvalidRole
	}
	if err := validateCredentials(name, password); err != nil {
		return nil, err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("生成密码哈希失败: %w", err)
	}

	newUser := &User{
		Name:         name,
		PasswordHash: string(passwordHash),
		Role:         role,
	}
	// 仓库层已把用户名冲突翻译为 ErrUsernameExists，此处直接上抛。
	if err := s.repo.Create(ctx, newUser); err != nil {
		return nil, err
	}

	return newUser, nil
}

// Login 校验用户名与密码，通过后返回对应用户。
func (s *Service) Login(ctx context.Context, username, password string) (*User, error) {
	// 登录不做密码格式校验：长度规则是「设置密码时」的约束（见 Register），
	// 存量账号的密码未必满足当前规则（如种子账号），一律放行到 bcrypt 比对，
	// 否则合法账号会被自己的校验规则锁在门外。仅拦截空值，省掉一次无意义的查库。
	if username == "" || password == "" {
		return nil, ErrInvalidCredentials
	}

	found, err := s.repo.FindByName(ctx, username)
	if err != nil {
		// 「用户不存在」与「密码错误」对外合并为同一文案，避免账号枚举。
		// 但数据库故障必须如实上抛，否则系统异常会被伪装成「密码错误」，
		// 既误导使用者，也让真实故障淹没在正常的认证失败里。
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	// 外部身份自动建出的账号没有密码，一律拒绝密码登录。
	// 这层判断是显式的，而不是依赖「bcrypt 恰好会失败」——占位值将来若被改动，
	// 隐式依赖会静默失效，显式判断则会立刻暴露问题。
	if found.PasswordHash == unusablePasswordHash {
		return nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(found.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return found, nil
}

// LoginByExternal 按外部身份登录：已绑定则返回既有账号，未绑定则自动建号。
//
// provider 与 externalID 由适配器（如 internal/wecom）提供，本包不感知任何具体提供方。
// 自动建出的账号写入不可用的密码哈希（见 unusablePasswordHash），因此不能用密码登录；
// 角色固定为普通用户，与公开注册接口保持一致——外部身份不得成为提权通道。
func (s *Service) LoginByExternal(ctx context.Context, provider IdentityProvider, externalID string) (*User, error) {
	if !provider.Valid() {
		return nil, ErrInvalidProvider
	}
	if externalID == "" {
		return nil, ErrInvalidExternalID
	}

	// 快路径：该身份已绑定过账号，直接返回。
	bound, err := s.repo.FindIdentity(ctx, provider, externalID)
	switch {
	case err == nil:
		return s.repo.FindByID(ctx, bound.UserID)
	case !errors.Is(err, ErrIdentityNotFound):
		return nil, err
	}

	// 慢路径：首次登录，自动建号。
	// 用户名冲突就换一个候选名重试；若该身份已被并发请求抢先建好，就直接取用那一个。
	var lastErr error
	for attempt := 0; attempt < externalUsernameAttempts; attempt++ {
		newUser := &User{
			Name:         externalUsername(provider, externalID, attempt),
			PasswordHash: unusablePasswordHash,
			Role:         RoleUser,
		}
		identity := &UserIdentity{Provider: provider, ExternalID: externalID}

		err := s.repo.CreateWithIdentity(ctx, newUser, identity)
		if err == nil {
			return newUser, nil
		}
		if !errors.Is(err, ErrUsernameExists) && !errors.Is(err, ErrExternalIdentityExists) {
			return nil, err
		}

		// 两类冲突共用这段兜底：先看该身份是否已经存在（并发场景）——存在就直接返回它；
		// 不存在则说明只是用户名撞了，换下一个候选名重试。
		if existing, findErr := s.repo.FindIdentity(ctx, provider, externalID); findErr == nil {
			return s.repo.FindByID(ctx, existing.UserID)
		} else if !errors.Is(findErr, ErrIdentityNotFound) {
			return nil, findErr
		}
		lastErr = err
	}

	return nil, fmt.Errorf("自动建号失败: 连续 %d 次用户名冲突: %w", externalUsernameAttempts, lastErr)
}

// BindExternal 把外部身份绑定到指定账号。
//
// 用途是让已经用密码登录的用户（例如管理员）绑定企业微信，此后可直接扫码进入同一账号，
// 而不必受「扫码一律新建普通账号」的限制。
// 该外部身份已被其他账号占用时返回 ErrExternalIdentityExists；重复绑定同一身份视为成功。
func (s *Service) BindExternal(ctx context.Context, userID uint, provider IdentityProvider, externalID string) error {
	if !provider.Valid() {
		return ErrInvalidProvider
	}
	if externalID == "" {
		return ErrInvalidExternalID
	}

	// 先确认账号存在，否则会绑出一条指向不存在用户的记录。
	if _, err := s.repo.FindByID(ctx, userID); err != nil {
		return err
	}

	// 幂等：重复绑定同一身份直接返回成功，用户重复点击时不该拿到一个无意义的冲突错误。
	existing, err := s.repo.FindIdentity(ctx, provider, externalID)
	switch {
	case err == nil:
		if existing.UserID == userID {
			return nil
		}
		return ErrExternalIdentityExists
	case !errors.Is(err, ErrIdentityNotFound):
		return err
	}

	return s.repo.CreateIdentity(ctx, &UserIdentity{
		UserID:     userID,
		Provider:   provider,
		ExternalID: externalID,
	})
}

// externalUsername 为外部身份生成候选用户名，供首次登录自动建号使用。
//
// 命名规则为 `<provider>_<清洗后的 externalID>`；attempt 是冲突重试序号，
// 0 返回基础名，之后依次追加 `-2`、`-3`……保证每次重试都换一个候选名。
//
// 这里刻意不走 validateCredentials：那套规则约束的是「用户自己取的用户名」，
// 拿它卡系统生成的标签，会把长 userid 的账号直接挡在门外（见 externalUsernameMaxLen）。
func externalUsername(provider IdentityProvider, externalID string, attempt int) string {
	base := string(provider) + "_" + sanitizeExternalID(externalID)
	if runes := []rune(base); len(runes) > externalUsernameMaxLen {
		base = string(runes[:externalUsernameMaxLen])
	}
	if attempt == 0 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, attempt+1)
}

// sanitizeExternalID 把外部标识清洗成用户名可用的字符集：非 ASCII 字母数字一律换成 '-'。
// 外部标识常含点、下划线、@ 等字符，直接拼进用户名会让账号看起来像被写坏了。
func sanitizeExternalID(externalID string) string {
	var b strings.Builder
	b.Grow(len(externalID))
	for _, r := range externalID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// validateCredentials 校验用户名与密码长度，按字符数计算以正确处理多字节字符。
// 返回的错误包装了对应哨兵（ErrInvalidUsername / ErrInvalidPassword），
// 因此既可被 errors.Is 识别，又能携带具体的长度要求。
func validateCredentials(username, password string) error {
	if n := utf8.RuneCountInString(username); n < usernameMinLen || n > usernameMaxLen {
		return fmt.Errorf("%w: 需在 %d 到 %d 个字符之间", ErrInvalidUsername, usernameMinLen, usernameMaxLen)
	}
	if n := utf8.RuneCountInString(password); n < passwordMinLen || n > passwordMaxLen {
		return fmt.Errorf("%w: 需在 %d 到 %d 个字符之间", ErrInvalidPassword, passwordMinLen, passwordMaxLen)
	}
	return nil
}
