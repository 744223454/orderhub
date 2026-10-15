/**
 * 受保护页面的统一外壳：顶栏（品牌 + 导航 + 用户 + 退出）与内容区。
 *
 * 背景（体检记录）：(user) 与 (staff) 两个布局各有一份近乎重复的 Header，
 * 且导航构成与顺序不同——从「我的订单」跳到「订单管理」时整个外壳换掉、
 * 每一项导航的位置也随之变动。统一后两个布局只保留「守卫策略」这一处差异
 * （(user) 不限角色、(staff) 限运营与管理员），外壳本身共用本组件。
 *
 * 职责边界：
 * - 不负责鉴权：那是 AuthGuard 的职责，本组件假定调用方已在守卫内；
 * - 不负责页面标题与容器：那是 PageContainer 的职责；
 * - 只负责顶栏与内容区的布局。
 */

import type { ReactNode } from 'react';
import { Button, Layout, theme, Typography } from 'antd';

import { NavLink } from '@/components/nav-link';
import { useSession } from '@/lib/auth';
import { navItemsFor } from '@/lib/navigation';
import { ROLE_LABELS } from '@/lib/roles';

export interface AppShellProps {
  children: ReactNode;
}

export function AppShell({ children }: AppShellProps) {
  const { session, signOut } = useSession();
  const { token } = theme.useToken();

  // 本组件只在守卫放行之后渲染，session 语义上必非空；
  // 但类型上仍是 Session | null，用三元把它收敛成可直接消费的形态。
  const navItems = session === null ? [] : navItemsFor(session.user.role);

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Layout.Header
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 24,
          paddingInline: 24,
          // 取语义 token，不再写死色值：调主题时全站随 ConfigProvider 联动。
          background: token.colorBgContainer,
          borderBottom: `1px solid ${token.colorSplit}`,
        }}
      >
        <Typography.Text strong>多角色订单系统</Typography.Text>

        {navItems.map((item) => (
          <NavLink key={item.href} href={item.href}>
            {item.label}
          </NavLink>
        ))}

        <span style={{ display: 'flex', alignItems: 'center', gap: 24, marginLeft: 'auto' }}>
          {session !== null && (
            <>
              <Typography.Text>{session.user.name}</Typography.Text>
              <Typography.Text type="secondary">{ROLE_LABELS[session.user.role]}</Typography.Text>
            </>
          )}
          <Button onClick={signOut}>退出登录</Button>
        </span>
      </Layout.Header>

      {/* Content 自带 flex: auto，会撑满外层 Layout（minHeight 100vh）的剩余高度，
          不要再手设 minHeight: 100vh——那会让整页高度变成 64px + 100vh，白出滚动条。 */}
      <Layout.Content style={{ padding: 24 }}>{children}</Layout.Content>
    </Layout>
  );
}
