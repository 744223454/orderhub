import type { Metadata } from 'next';
import type { ReactNode } from 'react';
import { AntdRegistry } from '@ant-design/nextjs-registry';

import './globals.css';

export const metadata: Metadata = {
  title: '多角色订单系统',
  description: '普通用户下单，运营与管理员管理订单',
};

/**
 * 根布局。
 *
 * 必须用 AntdRegistry 包裹子节点：antd 采用 CSS-in-JS，
 * 缺少它服务端渲染出的首屏将不带样式，页面会闪烁。
 */
export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="zh-CN">
      <body>
        <AntdRegistry>{children}</AntdRegistry>
      </body>
    </html>
  );
}
