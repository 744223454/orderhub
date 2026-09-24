package user

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestValidateCredentialsCountsRunes 锁定「按字符数而非字节数校验长度」这一修复。
// 中文用户名在旧实现下会因按字节计算而被误判超长。
func TestValidateCredentialsCountsRunes(t *testing.T) {
	cases := []struct {
		name     string
		username string
		password string
		wantErr  bool
	}{
		{"ASCII 正常", "alice", "pass1234", false},
		{"中文 11 字符合法（33 字节）", "怀民测试中文用户名长度", "pass1234", false},
		{"中文 16 字符合法（上界）", strings.Repeat("中", 16), "pass1234", false},
		{"中文 17 字符超限", strings.Repeat("中", 17), "pass1234", true},
		{"用户名为空", "", "pass1234", true},
		{"用户名 17 字符超限", strings.Repeat("a", 17), "pass1234", true},
		{"密码 7 字符过短", "alice", "pass123", true},
		{"密码 17 字符过长", "alice", strings.Repeat("a", 17), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCredentials(tc.username, tc.password)
			if tc.wantErr && err == nil {
				t.Error("期望校验失败，实际通过")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("期望校验通过，实际报错: %v", err)
			}
		})
	}
}

// TestValidateCredentialsReturnsSentinel 验证长度校验错误包装了哨兵错误，
// 使 handler 层能将其映射为 400，而不是落进「未识别错误 → 500」的分支。
func TestValidateCredentialsReturnsSentinel(t *testing.T) {
	if err := validateCredentials("", "pass1234"); !errors.Is(err, ErrInvalidUsername) {
		t.Errorf("用户名长度非法应包装 ErrInvalidUsername，实际: %v", err)
	}
	if err := validateCredentials("alice", "short"); !errors.Is(err, ErrInvalidPassword) {
		t.Errorf("密码长度非法应包装 ErrInvalidPassword，实际: %v", err)
	}
}

// TestServiceRegisterAndLogin 覆盖注册与登录的正常路径及凭据错误路径。
func TestServiceRegisterAndLogin(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()

	if _, err := service.Register(ctx, "alice", "pass1234", RoleUser); err != nil {
		t.Fatalf("注册应成功，实际报错: %v", err)
	}

	if _, err := service.Login(ctx, "alice", "pass1234"); err != nil {
		t.Fatalf("正确凭据应登录成功，实际报错: %v", err)
	}

	// 密码错误与用户不存在对外必须呈现同一个错误，避免账号枚举。
	if _, err := service.Login(ctx, "alice", "wrongpass"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("密码错误应返回 ErrInvalidCredentials，实际: %v", err)
	}
	if _, err := service.Login(ctx, "nobody", "pass1234"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("用户不存在对外也应返回 ErrInvalidCredentials，实际: %v", err)
	}
}

// TestServiceRegisterDuplicate 验证重复注册返回 ErrUsernameExists（handler 据此返回 409）。
func TestServiceRegisterDuplicate(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()

	if _, err := service.Register(ctx, "bob", "pass1234", RoleUser); err != nil {
		t.Fatalf("首次注册应成功，实际报错: %v", err)
	}
	if _, err := service.Register(ctx, "bob", "pass1234", RoleUser); !errors.Is(err, ErrUsernameExists) {
		t.Errorf("重复注册应返回 ErrUsernameExists，实际: %v", err)
	}
}

// TestServiceLoginDoesNotDisguiseDatabaseError 锁定本次修复：
// 数据库故障必须如实上抛，不能被伪装成「用户名或密码错误」，
// 否则系统异常会被淹没在正常的认证失败里，排查时无从下手。
func TestServiceLoginDoesNotDisguiseDatabaseError(t *testing.T) {
	service := setupTestService(t)

	_, err := service.Login(canceledCtx(), "alice", "pass1234")
	if err == nil {
		t.Fatal("数据库不可用时登录应返回错误")
	}
	if errors.Is(err, ErrInvalidCredentials) {
		t.Error("数据库故障被伪装成了认证失败")
	}
}
