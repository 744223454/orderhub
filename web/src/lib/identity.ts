/**
 * 外部身份提供方的展示文案。
 *
 * 单独拆一个文件而不是写在页面里，理由与 lib/order-status.ts 相同：
 * 展示文案集中一处，新增提供方时只改这里；而且页面导出的是带 'use client' 的组件，
 * 这些纯函数将来若要在服务端组件里用，从这个文件导入才不会踩边界。
 */

import type { IdentityProvider } from './types';

/** 各提供方的中文名。新增提供方时补在这里即可，页面不用动。 */
const PROVIDER_LABELS: Record<IdentityProvider, string> = {
  wecom: '企业微信',
};

/**
 * 取提供方的展示名。
 * 未知取值原样返回，避免后端先上线了新提供方、前端还在读旧代码时显示成空白。
 */
export function identityProviderLabel(provider: string): string {
  return PROVIDER_LABELS[provider as IdentityProvider] ?? provider;
}
