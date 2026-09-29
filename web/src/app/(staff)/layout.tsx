'use client';

import type { ReactNode } from 'react';
import Link from 'next/link';
import { Button, Layout, Typography } from 'antd';

import { AuthGuard } from '@/components/auth-guard';
import { useSession } from '@/lib/auth';
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
  const { signOut } = useSession();

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
          <Link href="/admin/orders">订单管理</Link>
          <Link href="/orders">我的订单</Link>
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
