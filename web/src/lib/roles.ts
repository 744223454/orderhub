/**
 * 角色相关的纯函数。
 *
 * 放在独立文件里而不是塞进 auth.tsx，是因为 auth.tsx 带 'use client'：
 * 从客户端模块导入的函数在服务端组件里调用会报错，而这里的判断
 * 服务端和客户端都可能要用。
 */

import type { Role } from './types';

/** 判断角色是否属于运营 / 管理员（即后端 RequireRole 放行的那两个）。 */
export function isStaff(role: Role): boolean {
  return role === 'admin' || role === 'ops';
}

/**
 * 按角色决定登录后该去的首页。
 *
 * 与后端的分组保持一致：staff 组挂的是 /api/admin/*，对应前端 /admin/orders；
 * 普通用户落在 /orders。守卫在「角色不符」时也用这个函数把人送回自己的首页。
 */
export function homePathFor(role: Role): string {
  return isStaff(role) ? '/admin/orders' : '/orders';
}
