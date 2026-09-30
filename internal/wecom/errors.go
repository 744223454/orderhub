package wecom

import "errors"

// 企业微信接入模块的错误哨兵。
//
// 调用方应使用 errors.Is 判断，不要依赖错误文案做字符串比较。
// 与其它模块一样分两类：业务错误由接口层映射为对应状态码；
// 技术错误（配置异常、网络故障等）接口层一律返回 500 加通用文案，
// 细节只写日志 —— 企微的 errmsg 是英文，且可能带出 CorpID、IP 等配置信息。
var (
	// ErrInvalidAuthCode 授权码无效：已使用过、已超时（5 分钟）或本就不是本次授权的码。
	// 对用户呈现为「授权已失效，请重新登录」。
	ErrInvalidAuthCode = errors.New("无效的授权码")
	// ErrInvalidState 回调带回的 state 与本次发起授权时种下的不一致。
	// 可能原因：授权页停留过久导致 state 过期，或攻击者伪造的登录请求（OAuth CSRF）。
	ErrInvalidState = errors.New("授权状态校验失败")
	// ErrNotCorpMember 授权者是外部联系人而非本企业成员。
	// 注意企微此时仍返回 errcode=0，只是把 userid 换成了 openid，必须靠判空识别。
	ErrNotCorpMember = errors.New("非本企业成员")
	// ErrCorpConfig 企业微信侧配置异常（CorpID / Secret / 可信域名 / 可信 IP 等）。
	// 归属技术错误：调用方无法处理，接口层返回 500 并记录日志。
	ErrCorpConfig = errors.New("企业微信配置异常")
	// ErrWecomUnavailable 调用企业微信接口失败：网络故障、响应无法解析，或未识别的错误码。
	ErrWecomUnavailable = errors.New("企业微信接口不可用")
)
