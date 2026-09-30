/**
 * 顶部导航项。
 *
 * 与裸 <Link> 的唯一区别：**正处于目标路径时不渲染成链接**。两个理由：
 *
 * 1. 点向当前页的 <Link> 等价于 `router.push(同一个路径)`，Next 会重新拉一遍 RSC payload
 *    （Next 15 起客户端 Router Cache 的 staleTime 默认是 0）；而本项目整棵页面树都是
 *    'use client' 组件，不会因此重新挂载，`useEffect` 也不会重跑
 *    ⇒ 表现是「网络面板一直在刷、界面纹丝不动」，纯浪费，还让人以为点了没反应。
 * 2. 顺带回答了「我现在在哪一页」——原先的导航没有任何当前项状态。
 *
 * 用在哪：(user)/layout.tsx 与 (staff)/layout.tsx 的头部导航。
 */

'use client';

import type { ReactNode } from 'react';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { Typography } from 'antd';

export interface NavLinkProps {
  /** 目标路径，同时也是「是否当前页」的判断依据。 */
  href: string;
  children: ReactNode;
}

/** 导航项；正处于目标路径时渲染为不可点的加粗文本。 */
export function NavLink({ href, children }: NavLinkProps) {
  const pathname = usePathname();

  // 用精确匹配就够：本项目的页面都是平级路径（/orders、/admin/orders）。
  // 将来若出现 /orders/123 这类子路由，这里要改成 startsWith。
  if (pathname === href) {
    return <Typography.Text strong>{children}</Typography.Text>;
  }

  return <Link href={href}>{children}</Link>;
}
