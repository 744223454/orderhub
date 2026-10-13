'use client';

import type { ReactNode } from 'react';

import { AuthGuard } from '@/components/auth-guard';
import type { Role } from '@/lib/types';
import { AppShell } from '@/components/app-shell';

/**
 * 允许进入管理端的角色，与后端 auth.RequireRole(user.RoleAdmin, user.RoleOps) 一致。
 *
 * 特意提到组件外面：如果在 JSX 里直接写 `allow={['admin', 'ops']}`，
 * 每次渲染都会新建一个数组，守卫内部若把 allow 放进 useEffect 的依赖数组，
 * 就会每渲染一次触发一次跳转判断。这类"字面量引用每次都是新的"是
 * React 里很常见的坑，能提到外面就提到外面。
 */
const STAFF_ROLES: Role[] = ['admin', 'ops'];

/**
 * 管理端布局。
 *
 * 地址规则同用户端：路由组 (staff) 不参与 URL，
 * (staff)/admin/orders/page.tsx 的地址就是 /admin/orders。
 */
export default function StaffLayout({ children }: { children: ReactNode }) {
  return (
    <AuthGuard allow={STAFF_ROLES}>
      <AppShell>{children}</AppShell>
    </AuthGuard>
  );
}
