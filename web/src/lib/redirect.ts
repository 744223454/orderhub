/**
 * 跳转模型：全站「该把人送去哪」的决策单一来源。
 *
 * 背景（1b 设计）：跳转规则原先散落在各个组件里，各自为政——
 * AuthGuard 拦下未登录访问时不记来源（登录后只能去角色首页，回不到用户本来想去的页面）、
 * /login 没有反向守卫（已登录还能继续看登录表单）、根路径写死 redirect('/orders')、
 * 企微绑定完成后写死回 /orders（明明是从账号设置页发起的）。
 * 本模块把规则收敛成一组纯函数，组件只负责调用。
 *
 * 规则表（1b 定稿）：
 * - 未登录访问受保护页 → `/login?from=<当前地址>`，登录成功后优先回跳；
 * - 登录成功（密码 / 企微扫码）→ 合法 from 优先，否则角色首页；
 * - 已登录访问 /login → 角色首页（GuestGuard 消费）；
 * - 已登录但角色不符 → 提示后回角色首页（AuthGuard 消费）；
 * - 根路径 / → 已登录去角色首页，未登录去 /login。
 *
 * 为什么放 lib 而不是组件文件：纯函数，不依赖 React 与 antd，可直接 vitest 单测
 * （见 redirect.test.ts）；而且「from 的安全校验」是安全边界，逻辑必须能被穷举验证。
 *
 * ⚠️ from 是 URL 参数、用户可控：sanitizeReturnTo 是防开放重定向的唯一防线，
 * 任何消费 from 的路径都必须先过它，不允许绕过。
 */

import { homePathFor } from './roles';
import type { Role } from './types';

/** 登录页的标准路径（不含查询串）。集中定义，避免各处硬编码拼写分叉。 */
export const LOGIN_PATH = '/login';

/** 登录来源在 sessionStorage 中的键名（saveLoginFrom / takeLoginFrom 共用）。 */
const LOGIN_FROM_KEY = 'orderhub.loginFrom';

/**
 * 校验 URL 参数里的回跳目标（from），防止开放重定向。
 *
 * 只有同时满足以下条件的值才放行，其余一律返回 null：
 * 1. 非空字符串，且以单个 `/` 开头——排除绝对地址（`https://…`）、
 *    裸相对路径（`orders`）与 `javascript:` 这类伪协议；
 * 2. 第二个字符不是 `/` 或 `\`——`//evil.com` 是协议相对地址，
 *    `/\evil.com` 会被浏览器按前者处理，两者都会把用户带出站外；
 * 3. 不指向登录相关页面（`/login` 及 `/login/**`）——否则登录成功后回跳
 *    又落回登录页，形成循环；前缀相似的路径（如 `/login-help`）不在此列。
 *
 * 放行时原样返回（不做规范化——原值就是要用的回跳地址）。
 * 验收标准见 redirect.test.ts「开放重定向防护」一组。
 */
export function sanitizeReturnTo(raw: string | null): string | null {
  if (!raw || !raw.startsWith('/') || raw.startsWith('/', 1) || raw.startsWith('\\', 1))
    return null;
  // 判定基于路径部分：先截断 ? 与 #（/login#x 同样指向登录页，也要拦）
  const path = raw.split(/[?#]/)[0];
  if (path === LOGIN_PATH || path.startsWith(`${LOGIN_PATH}/`)) return null;
  return raw;
}

/**
 * 构造登录页地址，把回跳目标编码进 from 参数。
 *
 * 编码用 encodeURIComponent（`/`、`?`、`&` 等都会转义），保证 from 的值
 * 在地址栏里始终是一个完整参数，不会被目标路径自带的查询串截断。
 * 读取端（useSearchParams 的 get / URLSearchParams）会自动解码。
 *
 * 信任边界：本函数只负责编码，不负责校验——调用方要么传入站内生成的地址
 * （如 currentReturnTo 的结果），要么让消费端先过 sanitizeReturnTo。
 */
export function buildLoginPath(returnTo: string): string {
  return `${LOGIN_PATH}?from=${encodeURIComponent(returnTo)}`;
}

/**
 * 登录成功后该去哪：合法的 from 优先，否则回落角色首页。
 *
 * 「合法」由 sanitizeReturnTo 判定——from 的用户可控入口只有 URL，
 * 校验必须发生在消费它的这一刻（也就是这里）。
 *
 * 密码登录、企微扫码回调与 GuestGuard（已登录访问 /login）共用本函数，
 * 保证「登录后的落点」只有一处定义——不共用就会互相覆盖（教训见 guest-guard.tsx）。
 */
export function resolvePostLoginPath(rawFrom: string | null, role: Role): string {
  return sanitizeReturnTo(rawFrom) ?? homePathFor(role);
}

/**
 * 当前页面地址，用作 returnUrl 的来源（pathname + search，不含 hash）。
 *
 * 客户端专用：AuthGuard 在浏览器里判断未登录、要拼 `/login?from=` 时调用。
 * 服务端（无 window）回落 '/'，保证任何调用时机都不会抛异常。
 */
export function currentReturnTo(): string {
  if (typeof window === 'undefined') {
    return '/';
  }
  return `${window.location.pathname}${window.location.search}`;
}

/**
 * 从当前地址读出 from 参数（登录来源）；无该参数或处于服务端时返回 null。
 *
 * 与 saveLoginFrom / takeLoginFrom 是同一份数据的两种载体：
 * URL 参数用于页面间直达（AuthGuard 弹到 `/login?from=…`），
 * sessionStorage 用于跨企微授权往返。返回值未经校验，
 * 消费端（resolvePostLoginPath）会统一过 sanitizeReturnTo。
 *
 * 不用 useSearchParams 读：它是 hook，会强制调用方包 Suspense；
 * 而这里的消费时机都是一次性读取（表单提交、守卫跳转计算），用不上响应式。
 */
export function readLoginFrom(): string | null {
  if (typeof window === 'undefined') {
    return null;
  }
  return new URLSearchParams(window.location.search).get('from');
}

/**
 * 暂存登录来源（企微登录往返用）。
 *
 * 场景：用户在 `/login?from=/admin/orders` 选择「企业微信登录」——
 * 浏览器会离开本站、经企微授权页再回来，from 参数在往返中丢失。
 * 跳走前把它暂存，回调页登录成功后取出。
 *
 * 用 sessionStorage 而不是 localStorage：这份数据只服务于当前这次登录动作，
 * 每个标签页一份，登出后不该留着污染下一次登录。
 * 键名沿用 storage.ts 的 `orderhub.*` 命名（如 `orderhub.loginFrom`）。
 *
 * 存的是原始值（不做校验）；安全校验统一在使用端由 resolvePostLoginPath 执行。
 * 服务端（无 window）静默忽略——与 storage.ts 同一防御风格。
 */
export function saveLoginFrom(returnTo: string): void {
  if (typeof window === 'undefined') {
    return;
  }
  window.sessionStorage.setItem(LOGIN_FROM_KEY, returnTo);
}

/**
 * 取出并清除暂存的登录来源；没有暂存时返回 null。
 *
 * 「取出即消费」：读走之后立刻清掉，避免下一次登录误用旧的来源。
 * ⚠️ 消费时机必须是「登录成功之后」，不能在页面加载时——登录失败重试时
 * 来源已被清掉的话，用户重试成功后就回不到原页面了。
 * 服务端（无 window）返回 null。
 */
export function takeLoginFrom(): string | null {
  if (typeof window === 'undefined') {
    return null;
  }
  const value = window.sessionStorage.getItem(LOGIN_FROM_KEY);
  if (value === null) return null;
  window.sessionStorage.removeItem(LOGIN_FROM_KEY);
  return value;
}
