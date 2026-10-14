/**
 * 路由守卫：拦住未登录或角色不符的访问。
 *
 * 用在哪：用户端 (user)/layout.tsx 和管理端 (staff)/layout.tsx 里，
 * 把 children 包住即可。这样每个页面不用各写一遍鉴权。
 *
 * 为什么是客户端组件：token 存在 localStorage，服务端读不到，
 * 只能在浏览器里判断。也正因如此，它属于「前端的门帘」而不是「锁」——
 * 真正的权限边界在后端的 auth.Auth / auth.RequireRole，前端的守卫只负责体验。
 */

'use client';

import type { ReactNode } from 'react';
import { useEffect } from 'react';
import type { Role } from '@/lib/types';
import { useSession } from '@/lib/auth';
import { useRouter } from 'next/navigation';
import { homePathFor } from '@/lib/roles';
import { AppSkeleton } from '@/components/app-skeleton';
import { buildLoginPath, currentReturnTo } from '@/lib/redirect';
import { App } from 'antd';

export interface AuthGuardProps {
  /** 允许通过的角色；省略表示「已登录即可」。 */
  allow?: Role[];
  children: ReactNode;
}

/**
 * 渲染 children 前先校验会话与角色。
 *
 * 三个最容易踩的点：
 *
 * 1. **必须先等 hydrated**。为 false 时直接返回加载态。跳过这一步，
 *    刷新页面的瞬间「还没读到 localStorage」会被误判成「未登录」，
 *    把已登录用户踢回登录页。
 * 2. **跳转与提示（message）都是副作用，必须放进 useEffect，不能写在渲染体里**。
 *    router.replace 内部会更新 Router 组件的状态，在渲染期间调用等于「渲染中改别人的
 *    state」，React 会报 `Cannot update a component (Router) while rendering a
 *    different component`，而且每次渲染都会重复触发。渲染体只做纯计算
 *    （算出该去哪、要不要提示），副作用一律交给 effect。
 * 3. **所有 Hook（含 App.useApp）必须在组件顶层无条件调用**。放进 if 分支时
 *    eslint 会报 rules-of-hooks，运行时更会因「两次渲染的 hook 数量不一致」直接崩。
 *
 * 跳转尚未完成时同样返回加载态，**不能直接渲染 children**，
 * 否则用户会看到一闪而过的、本不该看到的内容。
 *
 * 用 `replace` 而不是 `push`：被守卫拦下的访问不该留在浏览器历史里，
 * 否则用户点「后退」会被再次弹回来，形成死循环观感。
 */
export function AuthGuard({ allow, children }: AuthGuardProps) {
  const { session, hydrated } = useSession();
  const router = useRouter();
  const { message } = App.useApp();

  // 渲染期只做纯计算：算出该把访问者送去哪，null 表示放行。
  // 外层先卡 hydrated，避免「存储还没读完」被当成「未登录」。
  let redirectTo: string | null = null;
  let forbidden = false;
  if (hydrated) {
    if (session === null) {
      redirectTo = buildLoginPath(currentReturnTo());
    } else if (allow !== undefined && !allow.includes(session.user.role)) {
      // 角色不符，送回它自己的首页；路径由 roles.ts 统一决定，不要硬编码。
      forbidden = true;
      redirectTo = homePathFor(session.user.role);
    }
  }

  // 跳转与提示是副作用，放到渲染提交之后再执行，保持渲染体纯净。
  useEffect(() => {
    if (forbidden) {
      // message 的返回值是 thenable；显式 void 表示有意忽略——
      // 提示与跳转并行：跳转要即时，message 挂在根布局的 <App> 上、切页后仍会显示完。
      void message.warning('无权访问该页面，已为你返回首页');
    }
    if (redirectTo !== null) {
      router.replace(redirectTo);
    }
  }, [forbidden, redirectTo, router, message]);

  // 初始化未完成，或已决定跳转但跳转尚未生效 —— 两种情况下都只渲染加载态。
  if (!hydrated || redirectTo !== null) {
    return <AppSkeleton />;
  }

  return <>{children}</>;
}
