'use client';

import type { ReactNode } from 'react';
import { Button, Layout, Typography } from 'antd';

import { AuthGuard } from '@/components/auth-guard';
import { NavLink } from '@/components/nav-link';
import { useSession } from '@/lib/auth';
import { isAdmin } from '@/lib/roles';
import type { Role } from '@/lib/types';

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
  const { session, signOut } = useSession();

  return (
    <AuthGuard allow={STAFF_ROLES}>
      <Layout style={{ minHeight: '100vh' }}>
        <Layout.Header
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 24,
            paddingInline: 24,
            background: '#fff',
            borderBottom: '1px solid rgba(5, 5, 5, 0.06)',
          }}
        >
          <Typography.Text strong>多角色订单系统 · 管理端</Typography.Text>
          <NavLink href="/admin/orders">订单管理</NavLink>
          <NavLink href="/orders">我的订单</NavLink>
          {/* 用户管理与组织架构只对管理员显示，用 isAdmin 而不是 isStaff ——
              运营能进这个页面的后端接口会直接 403，入口就不该出现。 */}
          {session !== null && isAdmin(session.user.role) && (
            <>
              <NavLink href="/admin/users">用户管理</NavLink>
              <NavLink href="/admin/org">组织架构</NavLink>
            </>
          )}
          <NavLink href="/settings">账号设置</NavLink>
          <span style={{ marginLeft: 'auto' }}>
            <Button type="link" style={{ padding: 0 }} onClick={signOut}>
              退出登录
            </Button>
          </span>
        </Layout.Header>
        <Layout.Content style={{ padding: 24 }}>{children}</Layout.Content>
      </Layout>
    </AuthGuard>
  );
}
