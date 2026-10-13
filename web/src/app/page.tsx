'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';

import { useSession } from '@/lib/auth';
import { LOGIN_PATH } from '@/lib/redirect';
import { homePathFor } from '@/lib/roles';
import { AppSkeleton } from '@/components/app-skeleton';

/**
 * 根路由：按会话「客户端分流」。
 *
 * 为什么不能像原来那样在服务端写死 redirect('/orders')：登录态在 localStorage，
 * 服务端读不到——staff 从根路径进来会先落 /orders、再被守卫弹去 /admin/orders，
 * 白跳一次；未登录用户同样要多绕一道。分流只能在浏览器里做：
 * 未登录去 /login，已登录去自己角色的首页（homePathFor）。
 *
 * 刻意不带 from：根路径自己当 from 没有意义（登录后回 '/' 又要再分流一次），
 * 直接去 /login，让登录成功后的落点由 resolvePostLoginPath 统一决定。
 *
 * 本页面永远只渲染加载骨架——分流完就走，没有可见内容。
 */
export default function RootPage() {
  const { session, hydrated } = useSession();
  const router = useRouter();

  // 渲染体只做纯计算；跳转是副作用，放进 effect（原因见 auth-guard.tsx 顶部注释）。
  let directTo: string | null = null;
  if (hydrated) {
    directTo = session === null ? LOGIN_PATH : homePathFor(session.user.role);
  }

  useEffect(() => {
    if (directTo !== null) {
      router.replace(directTo);
    }
  }, [directTo, router]);

  return <AppSkeleton />;
}
