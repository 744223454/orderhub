/**
 * 角色相关的纯函数。
 *
 * 放在独立文件里而不是塞进 auth.tsx，是因为 auth.tsx 带 'use client'：
 * 从客户端模块导入的函数在服务端组件里调用会报错，而这里的判断
 * 服务端和客户端都可能要用。
 */

import type { Role } from './types';

/** 角色对应的中文名，用于展示。 */
export const ROLE_LABELS: Record<Role, string> = {
  admin: '管理员',
  user: '普通用户',
  ops: '运营',
};

/**
 * 用户管理页可以赋予的角色，顺序即按钮顺序。
 * 与后端 user.Role.Valid() 放行的取值集合保持一致。
 */
export const ASSIGNABLE_ROLES: Role[] = ['user', 'ops', 'admin'];

/**
 * 仅管理员。
 *
 * 作为 AuthGuard 的 allow 白名单使用时必须提到模块级：字面量数组每次渲染都是新引用，
 * 放进组件里会让守卫内部的依赖判断每次都被判为变化。
 */
export const ADMIN_ROLES: Role[] = ['admin'];

/** 判断角色是否属于运营 / 管理员（即后端 RequireRole 放行的那两个）。 */
export function isStaff(role: Role): boolean {
  return role === 'admin' || role === 'ops';
}

/**
 * 判断角色是否仅为管理员。
 *
 * 与 isStaff 的区别在于运营不算：用户管理、改角色这类操作是权限边界本身，
 * 后端挂的是 RequireRole(user.RoleAdmin)，前端的入口显隐必须与之一致。
 */
export function isAdmin(role: Role): boolean {
  return role === 'admin';
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
