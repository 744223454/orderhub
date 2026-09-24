package auth

import "errors"

// 认证模块的业务错误。
//
// 调用方应使用 errors.Is 判断，不要依赖错误文案做字符串比较。
var (
	// ErrInvalidToken 令牌未通过校验（格式错误、签名无效、签发者不符或已过期等）。
	// 具体失败原因一并包装在下层，因此 errors.Is(err, jwt.ErrTokenExpired)、
	// errors.Is(err, jwt.ErrTokenSignatureInvalid) 等判断依然有效。
	ErrInvalidToken = errors.New("无效的令牌")
)
