'use client';

import type { ReactNode } from 'react';
import { Button, Layout, Typography } from 'antd';

import { AuthGuard } from '@/components/auth-guard';
import { NavLink } from '@/components/nav-link';
import { useSession } from '@/lib/auth';
import { isStaff } from '@/lib/roles';

/**
 * 用户端布局。
 *
 * 路由组 (user) 的括号表示它**不参与 URL**——有了它，
 * (user)/orders/page.tsx 的地址仍是 /orders，但这一层的 layout 能共享。
 * 需要新页面时直接往这个目录下加文件夹即可，守卫和导航自动生效。
 *
 * 这一层只做「登录与否」的判断（AuthGuard 不传 allow）；
 * 「是不是运营 / 管理员」交给 (staff)/layout.tsx。
 */
export default function UserLayout({ children }: { children: ReactNode }) {
  const { session, signOut } = useSession();

  return (
    <AuthGuard>
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
          <Typography.Text strong>多角色订单系统</Typography.Text>
          <NavLink href="/orders">我的订单</NavLink>
          {/* 运营 / 管理员从管理端点「我的订单」过来后，这里必须留一条回去的路：
              /orders 落在 (user) 路由组，渲染的是这一层布局，而它的导航里原本没有
              管理端入口 —— 于是就成了单向门，只能靠浏览器回退。
              判断依据直接复用 roles.ts 的 isStaff，与后端 RequireRole(admin, ops) 一致。 */}
          {session !== null && isStaff(session.user.role) && (
            <NavLink href="/admin/orders">订单管理</NavLink>
          )}
          {/* 账号设置（绑定企业微信）挂在用户端导航里；管理端布局也放了同一个入口，
              免得管理员在两套导航间来回时又变成「单向门」。 */}
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
