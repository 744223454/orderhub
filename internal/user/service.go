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
func (s *Service) Register(ctx context.Context, name, password string, role Role) (*User, error) {
	if !role.Valid() {
		return nil, errors.New("无效的角色")
	}
	if err := validateCredentials(name, password); err != nil {
		return nil, err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	newUser := &User{
		Name:         name,
		PasswordHash: string(passwordHash),
		Role:         role,
	}
	if err := s.repo.Create(ctx, newUser); err != nil {
		return nil, err
	}

	return newUser, nil
}

// Login 校验用户名与密码，通过后返回对应用户。
func (s *Service) Login(ctx context.Context, username, password string) (*User, error) {
	if err := validateCredentials(username, password); err != nil {
		return nil, err
	}

	found, err := s.repo.FindByName(ctx, username)
	if err != nil {
		// 不区分「用户不存在」与「密码错误」，避免账号枚举。
		return nil, errors.New("用户名或密码错误")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(found.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("用户名或密码错误")
	}

	return found, nil
}

// validateCredentials 校验用户名与密码长度，按字符数计算以正确处理多字节字符。
func validateCredentials(username, password string) error {
	if n := utf8.RuneCountInString(username); n < usernameMinLen || n > usernameMaxLen {
		return fmt.Errorf("用户名长度需在 %d 到 %d 个字符之间", usernameMinLen, usernameMaxLen)
	}
	if n := utf8.RuneCountInString(password); n < passwordMinLen || n > passwordMaxLen {
		return fmt.Errorf("密码长度需在 %d 到 %d 个字符之间", passwordMinLen, passwordMaxLen)
	}
	return nil
}
