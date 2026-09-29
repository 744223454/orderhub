/**
 * 会话（token + 用户信息）的浏览器本地存取。
 *
 * 单独拆成一个模块的原因是依赖方向：
 * `api.ts` 要读 token 塞进 Authorization 头，`auth.tsx` 要写 token，
 * 两者都依赖本模块，从而避免 `api.ts` ↔ `auth.tsx` 互相 import 形成循环。
 *
 * 取舍说明：这里用 localStorage，读写简单、调试直观，代价是 token 可被
 * 页面内任意脚本读取（XSS 风险）。更安全的做法是把 token 换成 httpOnly Cookie，
 * 但那需要后端改成下发 Cookie，或在前端加一层 Route Handler 做代理转发。
 * 先把功能跑通，这条升级路径记在心上即可。
 *
 * 注意：localStorage 只存在于浏览器。SSR / 预渲染阶段访问会直接抛异常，
 * 因此每次读写都先判断 `window` 是否存在。
 */

import type { User } from './types';

/** token 在 localStorage 中的键名。 */
const TOKEN_KEY = 'orderhub.token';

/** 用户信息在 localStorage 中的键名。 */
const USER_KEY = 'orderhub.user';

/**
 * 读取访问令牌。未登录或处于服务端时返回 null。
 */
export function readToken(): string | null {
  if (typeof window === 'undefined') {
    return null;
  }
  return window.localStorage.getItem(TOKEN_KEY);
}

/**
 * 读取已缓存的用户信息。未登录、数据损坏或处于服务端时返回 null。
 *
 * 之所以把用户信息也缓存下来，是为了刷新页面后能立刻拿到 role 做路由分流，
 * 不必先打一次接口。代价是用户改名 / 改角色后需要重新登录才会更新。
 */
export function readUser(): User | null {
  if (typeof window === 'undefined') {
    return null;
  }
  const raw = window.localStorage.getItem(USER_KEY);
  if (raw === null) {
    return null;
  }
  try {
    return JSON.parse(raw) as User;
  } catch {
    // 存储被外部写坏时当作未登录处理，而不是让整个页面崩掉。
    return null;
  }
}

/**
 * 写入会话。登录成功后调用。
 */
export function saveSession(token: string, user: User): void {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.setItem(TOKEN_KEY, token);
  window.localStorage.setItem(USER_KEY, JSON.stringify(user));
}

/**
 * 清除会话。退出登录、或收到 401 时调用。
 */
export function clearSession(): void {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.removeItem(TOKEN_KEY);
  window.localStorage.removeItem(USER_KEY);
}
