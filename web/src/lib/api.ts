/**
 * 后端 API 客户端。
 *
 * 所有请求都发往同源的 `/api/*`（相对路径，不要写死 http://localhost:8888）。
 * 开发期由 `next.config.ts` 里的 rewrites 把 `/api/*` 代理到 Go 后端，
 * 因此浏览器看到的是同源请求，后端不需要开 CORS——等价于 Vite 的 server.proxy。
 */

import { readToken } from './storage';
import type {
  AdminUser,
  AdminUserPage,
  ApiErrorBody,
  CreateOrderRequest,
  IdentityProvider,
  LoginRequest,
  LoginResponse,
  MeResponse,
  Order,
  OrgDirectory,
  UpdateUserRoleRequest,
  WecomAuthorizeResponse,
  WecomCallbackRequest,
  WecomIntent,
} from './types';

/**
 * 后端返回非 2xx 时抛出的错误。
 *
 * 携带 status 便于调用方分支处理（401 去登录、409 提示状态冲突）。
 * message 直接取自后端响应体里的 error 字段，可直接展示给用户。
 */
export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

/**
 * 调用后端接口并解析 JSON 响应，失败时抛出 ApiError。
 *
 * TODO: 由你实现。验收标准（逐条对照，缺一条都会在上线后咬人）：
 *
 * 1. 请求地址用传入的 `path`（同源相对路径），开发期会被 rewrites 代理到 Go 后端。
 * 2. 请求头：默认带 `Content-Type: application/json`；
 *    若 `readToken()` 有值，追加 `Authorization: Bearer <token>`。
 *    注意读取 token 要在这层统一做，页面里不要各自拼 header。
 * 3. 响应状态不是 2xx 时：尝试解析响应体里的 `{ error }` 并
 *    `throw new ApiError(status, error)`；解析不出来就
 *    `throw new ApiError(status, '请求失败')`。绝不把空的 message 抛出去。
 * 4. 成功时返回 `await response.json()`。若某个接口将来返回 204（无响应体），
 *    直接 `json()` 会抛 SyntaxError，届时需要单独处理。
 * 5. 401 的处理放哪里要想清楚：放在这里"自动跳登录"很危险——
 *    登录接口本身返回 401（账号密码错误）也会触发跳转，造成刷新循环。
 *    推荐由调用方（守卫、页面）判断 `err.status === 401` 后再决定。
 *
 * 另外留意：Next.js 在服务端组件里会对 fetch 做缓存，而本函数是给浏览器用的。
 * 如果以后要在服务端组件里取数，用 `API_BASE_URL` 拼绝对地址另写一份，
 * 相对路径在 Node 环境里会直接报 "Failed to parse URL"。
 */
export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  // TODO: 由你实现（第 2 条要用到上面 import 的 readToken）。
  // 下面这行只是先把 token 取出来，方便你确认登录状态是否读写正常；
  // 真正的请求构造与错误映射请照着上方注释补全。
  const headers = new Headers(init?.headers);
  headers.set('Content-Type', 'application/json');
  const token = readToken();
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }
  const response = await fetch(path, { ...init, headers });

  if (!response.ok) {
    let message = '请求失败';
    try {
      const body = (await response.json()) as ApiErrorBody;
      if (typeof body?.error === 'string' && body?.error) {
        message = body.error;
      }
    } catch {}
    throw new ApiError(response.status, message);
  }

  return (await response.json()) as T;
}

/**
 * 后端接口表。
 *
 * 这层只做「路径 + 方法」的映射，不含任何业务判断，
 * 所以只要 `request` 实现好了，下面这些方法就全部可用。
 */
export const api = {
  /** 登录，成功返回 token 与用户信息。 */
  login(body: LoginRequest): Promise<LoginResponse> {
    return request<LoginResponse>('/api/login', { method: 'POST', body: JSON.stringify(body) });
  },

  /** 注册。公开接口，只能注册为普通用户。 */
  register(body: LoginRequest): Promise<void> {
    return request<void>('/api/register', { method: 'POST', body: JSON.stringify(body) });
  },

  /** 获取当前登录用户，可用来校验本地 token 是否仍然有效。 */
  me(): Promise<MeResponse> {
    return request<MeResponse>('/api/me');
  },

  /** 下单。 */
  createOrder(body: CreateOrderRequest): Promise<Order> {
    return request<Order>('/api/orders', { method: 'POST', body: JSON.stringify(body) });
  },

  /** 查询自己的订单列表。 */
  listMyOrders(): Promise<Order[]> {
    return request<Order[]>('/api/orders');
  },

  /** 支付自己的订单（pending → paid）。 */
  payOrder(id: number): Promise<Order> {
    return request<Order>(`/api/orders/${id}/pay`, { method: 'POST' });
  },

  /** 查询全部订单（仅运营 / 管理员）。 */
  listAllOrders(): Promise<Order[]> {
    return request<Order[]>('/api/admin/orders');
  },

  /** 发货（paid → shipped，仅运营 / 管理员）。 */
  shipOrder(id: number): Promise<Order> {
    return request<Order>(`/api/admin/orders/${id}/ship`, { method: 'POST' });
  },

  /** 完成订单（shipped → completed，仅运营 / 管理员）。 */
  completeOrder(id: number): Promise<Order> {
    return request<Order>(`/api/admin/orders/${id}/complete`, { method: 'POST' });
  },

  /** 退款（paid / shipped → refunded，仅运营 / 管理员）。 */
  refundOrder(id: number): Promise<Order> {
    return request<Order>(`/api/admin/orders/${id}/refund`, { method: 'POST' });
  },

  /**
   * 获取企业微信扫码登录链接。
   *
   * 后端在此接口里同时种下 HttpOnly 的 state Cookie，页面读不到也不需要读——
   * 回调换票时浏览器会自动带上它。
   */
  wecomAuthorize(intent: WecomIntent): Promise<WecomAuthorizeResponse> {
    return request<WecomAuthorizeResponse>(`/api/auth/wecom/authorize?intent=${intent}`);
  },

  /**
   * 企业微信扫码登录：用回调页地址栏里的授权码换访问令牌。
   * 返回形状与密码登录一致，可直接交给 signIn。
   */
  wecomLogin(body: WecomCallbackRequest): Promise<LoginResponse> {
    return request<LoginResponse>('/api/auth/wecom/login', {
      method: 'POST',
      body: JSON.stringify(body),
    });
  },

  /** 把企业微信身份绑定到当前登录账号（需已登录，token 由 request 自动带上）。 */
  wecomBind(body: WecomCallbackRequest): Promise<void> {
    return request<void>('/api/auth/wecom/bind', {
      method: 'POST',
      body: JSON.stringify(body),
    });
  },

  /**
   * 查询用户列表（仅管理员）。
   *
   * 分页与关键字都由后端处理：前端不做本地切片，否则总数、页码都会与后端对不上。
   * 只传有值的参数，避免拼出 `?page=undefined` 这种被后端当非法值忽略掉的查询串。
   */
  listUsers(
    params: { page?: number; pageSize?: number; keyword?: string } = {},
  ): Promise<AdminUserPage> {
    const query = new URLSearchParams();
    if (params.page !== undefined) {
      query.set('page', String(params.page));
    }
    if (params.pageSize !== undefined) {
      query.set('page_size', String(params.pageSize));
    }
    if (params.keyword) {
      query.set('keyword', params.keyword);
    }
    const suffix = query.toString();
    return request<AdminUserPage>(`/api/admin/users${suffix === '' ? '' : `?${suffix}`}`);
  },

  /** 修改用户角色（仅管理员），返回更新后的用户。 */
  updateUserRole(id: number, body: UpdateUserRoleRequest): Promise<AdminUser> {
    return request<AdminUser>(`/api/admin/users/${id}/role`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    });
  },

  /** 解绑用户的外部身份（仅管理员）。解绑后该账号无法再用这种方式登录。 */
  unbindUserIdentity(id: number, provider: IdentityProvider): Promise<void> {
    return request<void>(`/api/admin/users/${id}/identities/${provider}`, { method: 'DELETE' });
  },

  /**
   * 获取企业通讯录（仅管理员）。
   *
   * 返回的是**扁平**的部门与成员：树由前端组装。
   * refresh 传 true 时带 `?refresh=1` 绕过服务端 5 分钟缓存 —— 只在用户主动点刷新时用，
   * 首屏/切页一律走缓存，否则每打开一次页面就是几十次企微调用。
   */
  getOrgDirectory(refresh = false): Promise<OrgDirectory> {
    return request<OrgDirectory>(`/api/admin/org/directory${refresh ? '?refresh=1' : ''}`);
  },
};
