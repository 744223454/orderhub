'use client';

import type { ReactNode } from 'react';
import Link from 'next/link';
import { Button, Layout, Typography } from 'antd';

import { AuthGuard } from '@/components/auth-guard';
import { useSession } from '@/lib/auth';

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
  const { signOut } = useSession();

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
