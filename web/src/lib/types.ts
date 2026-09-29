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
