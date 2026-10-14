/**
 * 访问令牌的无感刷新（浏览器侧）。
 *
 * 背景：后端把登录凭据拆成「短寿命 access + 长寿命 refresh」一对（练习六批次 1）。
 * 本模块负责前端的那一半：请求因 access 过期收到 401 时，用 refresh 静默换回
 * 新的一对令牌，再由调用方把原请求重发——用户全程无感知。
 *
 * 为什么单独成模块，而不是写进 api.ts：
 * 1. 刷新必须**合流**（见下），合流状态需要一个模块级的变量来承载；
 * 2. 依赖保持单向：api.ts → session-refresh.ts → storage.ts，不形成循环；
 * 3. 不依赖 React 与 DOM（storage 层已做 `typeof window` 防护），
 *    验收靶（session-refresh.test.ts）可以在 Node 环境直接跑。
 *
 * ⚠️ 合流（singleflight）不是优化项，是正确性要求：
 * 页面同时发出多个请求、同时 401 时，若每个都各自拿着**同一个旧 refresh**
 * 去刷新——刷新是轮转式的（用一次即作废），第一个成功后旧串立即失效，
 * 第二个再递上来会被后端判定为**重放**，触发整个令牌家族连坐撤销，
 * 所有设备一起被登出。所以进行中的刷新只能有一个，后来的调用等同一个 Promise。
 *
 * 已知边界（本期不做，留作面试谈资）：跨标签页无法合流，两个标签同时刷新
 * 仍会重放连坐；要彻底解决需用 BroadcastChannel 或 storage 事件做跨页协调。
 */
import { clearSession, notifySessionExpired, readRefreshToken, saveTokens } from './storage';
import type { RefreshResponse } from './types';

let pending: Promise<string> | null = null;

/**
 * 刷新访问令牌，返回新的 access 令牌。并发调用合流为同一次刷新。
 *
 * 失败语义（与验收靶 session-refresh.test.ts 的契约一致）：
 * - 本地没有 refresh 令牌（未登录 / 已登出）→ 直接抛错，不发请求；
 * - 被拒绝（401：过期 / 已撤销 / 重放）→ 清空会话 + 广播失效事件，再抛错；
 * - 网络故障等 → 只抛错、不动会话（网络抖一下不该把用户登出）；
 * - 无论成败都会清空「进行中」登记（finally）——失败绝不能被缓存，否则永远无法自愈。
 */
export function refreshAccessToken(): Promise<string> {
  if (pending) {
    return pending;
  }

  pending = doRefresh().finally(() => {
    pending = null;
  });
  return pending;
}

/** 执行一次完整的刷新交互：裸 fetch 直连后端（不经 api.request，防「刷新触发刷新」的环）。 */
async function doRefresh(): Promise<string> {
  const refreshToken = readRefreshToken();
  if (!refreshToken) {
    throw new Error('未登录，无法刷新访问令牌');
  }

  const res = await fetch('/api/refresh', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token: refreshToken }),
  });

  if (res.status === 401) {
    clearSession();
    notifySessionExpired();
    throw new Error('登录已过期，请重新登录');
  }

  if (!res.ok) {
    throw new Error('刷新访问令牌失败');
  }

  const data = (await res.json()) as RefreshResponse;
  saveTokens(data.token, data.refresh_token);
  return data.token;
}
