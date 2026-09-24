package user

import (
	"context"
	"errors"
	"fmt"
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
	if err := bcrypt.CompareHashAndPassword([]byte(found.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return found, nil
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
