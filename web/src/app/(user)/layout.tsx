'use client';

import type { ReactNode } from 'react';

import { AuthGuard } from '@/components/auth-guard';
import { AppShell } from '@/components/app-shell';

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
  return (
    <AuthGuard>
      <AppShell>{children}</AppShell>
    </AuthGuard>
  );
}
