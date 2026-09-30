/**
 * 后端接口的 TypeScript 类型定义。
 *
 * 字段与 Go 侧的 model JSON tag 一一对应（internal/user/model.go、internal/order/model.go），
 * 与 docs/swagger.json 也应保持一致。后端字段若变更，这里必须同步——
 * 否则前端只会在运行期拿到 undefined，编译期不会有任何提示。
 */

/** 用户角色，取值与后端 user.Role 一致。 */
export type Role = 'admin' | 'user' | 'ops';

/** 用户信息，对应后端 user.User（不含密码哈希）。 */
export interface User {
  id: number;
  name: string;
  role: Role;
  created_at: string;
  updated_at: string;
}

/** 订单状态，取值与后端 order.Status 一致。 */
export type OrderStatus = 'pending' | 'paid' | 'shipped' | 'completed' | 'refunded';

/** 订单，对应后端 order.Order。 */
export interface Order {
  id: number;
  user_id: number;
  product_name: string;
  /** 金额，单位为分。展示时需自行除以 100，不要用浮点运算累加。 */
  amount: number;
  status: OrderStatus;
  created_at: string;
  updated_at: string;
}

/** 登录请求体，对应 POST /api/login。 */
export interface LoginRequest {
  username: string;
  password: string;
}

/** 登录成功响应，对应 POST /api/login 的返回。 */
export interface LoginResponse {
  token: string;
  user: User;
}

/** 当前登录用户，对应 GET /api/me 的返回。 */
export interface MeResponse {
  user_id: number;
  role: Role;
}

/** 下单请求体，对应 POST /api/orders。 */
export interface CreateOrderRequest {
  product_name: string;
  amount: number;
}

/** 后端统一的错误响应体。所有非 2xx 响应都是这个形状。 */
export interface ApiErrorBody {
  error: string;
}

/**
 * 企业微信授权的用途。
 *
 * login：扫码后登录，该企业微信身份没绑过账号就自动建一个普通账号；
 * bind：扫码后把该身份绑定到当前登录的账号（管理员、运营这样把自己的账号挂上去）。
 */
export type WecomIntent = 'login' | 'bind';

/** 获取企业微信扫码登录链接的返回，对应 GET /api/auth/wecom/authorize。 */
export interface WecomAuthorizeResponse {
  /** 企业微信扫码登录页地址。必须整页跳转到它，不能 fetch。 */
  url: string;
}

/** 企业微信回调参数，对应 POST /api/auth/wecom/login 与 POST /api/auth/wecom/bind。 */
export interface WecomCallbackRequest {
  /** 企微回调带回的授权码，只能用一次、5 分钟过期。 */
  code: string;
  /** 发起授权时下发的状态串，后端用它比对 Cookie 以防范 CSRF。 */
  state: string;
}
