package user

import "errors"

// 用户模块的业务错误。
//
// 调用方应使用 errors.Is 判断，不要依赖错误文案做字符串比较。
// 技术错误（数据库故障等）不进这里，由仓库层用 fmt.Errorf("描述: %w", err) 包装后向上抛。
var (
	// ErrUserNotFound 用户不存在。
	// 仅供模块内部区分「查无此人」与数据库故障——登录流程对外一律以
	// ErrInvalidCredentials 呈现，避免攻击者用响应差异枚举已存在的账号。
	ErrUserNotFound = errors.New("用户不存在")
	// ErrUsernameExists 用户名已被占用。
	ErrUsernameExists = errors.New("用户名已存在")
	// ErrInvalidRole 角色不是预设的合法值。
	ErrInvalidRole = errors.New("无效的角色")
	// ErrInvalidCredentials 用户名或密码错误（不区分二者，避免账号枚举）。
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	// ErrInvalidUsername 用户名长度不合法，错误文案中会附带具体要求。
	ErrInvalidUsername = errors.New("用户名长度不合法")
	// ErrInvalidPassword 密码长度不合法，错误文案中会附带具体要求。
	ErrInvalidPassword = errors.New("密码长度不合法")
	// ErrIdentityNotFound 外部身份尚未绑定任何账号。
	// 仅供模块内部区分「未绑定」与数据库故障，不直接对外暴露。
	ErrIdentityNotFound = errors.New("外部身份未绑定账号")
	// ErrExternalIdentityExists 该外部身份已被其他账号绑定（接口层据此返回 409）。
	ErrExternalIdentityExists = errors.New("该外部身份已被占用")
	// ErrInvalidProvider 外部身份提供方不是预设的合法值。
	ErrInvalidProvider = errors.New("无效的身份提供方")
	// ErrInvalidExternalID 外部身份标识为空。
	ErrInvalidExternalID = errors.New("无效的外部身份标识")
	// ErrLastAdmin 不允许把系统中最后一个管理员降级。
	// 降级之后没有人能再进入用户管理把它改回来，系统只能靠手工改库恢复。
	ErrLastAdmin = errors.New("不能降级最后一个管理员")
)
