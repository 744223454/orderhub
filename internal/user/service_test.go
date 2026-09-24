package user

import (
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
