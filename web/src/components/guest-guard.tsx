/**
 * 反向守卫：把「已登录的人」挡在登录页之外。
 *
 * 与 AuthGuard 恰好互补——AuthGuard 拦未登录 / 角色不符的人，
 * 本组件拦已登录的人：登录页对已登录用户没有意义，放行只会让人困惑。
 *
 * 用在 /login：包住登录卡片。已登录时送去角色首页（homePathFor），
 * 未登录时正常渲染 children（登录表单）。
 *
 * 已登录时去哪：与「登录成功后的回跳」共用同一算法（resolvePostLoginPath）——
 * 合法 from 优先，否则角色首页。⚠️ 必须共用（实测教训）：登录成功的那一刻，
 * 本组件与登录页会同时观察到「已登录」并各自发起跳转，算法不一致时
 * 本组件的「角色首页」会把登录页的「回跳目标」挤掉，from 回跳静默失效。
 *
 * 实现备注（防回退）：
 * 1. `hydrated` 之前渲染加载态（@/components/app-skeleton 的 AppSkeleton），
 *    不直接渲染 children——否则刷新瞬间「还没读到 localStorage」会被当成未登录，
 *    登录表单会一闪而过；
 * 2. 已登录时的跳转放在 useEffect 里，渲染体只做纯计算，且跳转未完成时
 *    继续渲染加载态——与 AuthGuard 相同的两条纪律，原因见 auth-guard.tsx 顶部注释；
 * 3. 用 `router.replace` 而不是 `push`：登录页不该留在浏览器历史里。
 *
 * 本组件只用 window.location 直读 from（见 readLoginFrom），不用 useSearchParams，
 * 因此调用方不必为它包 Suspense。
 */

'use client';
import type { ReactNode } from 'react';
import { useEffect } from 'react';
import { useSession } from '@/lib/auth';
import { AppSkeleton } from '@/components/app-skeleton';
import { useRouter } from 'next/navigation';
import { readLoginFrom, resolvePostLoginPath } from '@/lib/redirect';

export interface GuestGuardProps {
  children: ReactNode;
}

/**
 * 渲染 children 前先确认「当前是未登录状态」。
 */
export function GuestGuard({ children }: GuestGuardProps) {
  const { session, hydrated } = useSession();
  const router = useRouter();

  let redirectTo: string | null = null;
  if (hydrated && session) {
    redirectTo = resolvePostLoginPath(readLoginFrom(), session.user.role);
  }

  useEffect(() => {
    if (redirectTo !== null) {
      router.replace(redirectTo);
    }
  }, [redirectTo, router]);

  if (!hydrated || redirectTo !== null) {
    return <AppSkeleton />;
  }

  return <>{children}</>;
}
