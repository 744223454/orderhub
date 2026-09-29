/**
 * 全局会话状态。
 *
 * 为什么需要它：登录状态被页面（导航栏显示用户名 / 退出按钮）和守卫
 * （判断能不能进这个路由）同时用到，属于典型的"跨越组件层级共享状态"。
 * React 的标准做法是 Context——概念上对应 Vue 3 的 provide / inject，
 * 或 Pinia 里那个全局 store，但 React 不会自动追踪依赖，改动必须显式调 setState。
 *
 * storage.ts 只负责"存到浏览器"，本文件负责"让组件读到并响应变化"，两者不要混。
 */

'use client';

import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';

import { clearSession, readToken, readUser, saveSession } from './storage';
import type { User } from './types';

/** 一份已登录的会话。 */
export interface Session {
  token: string;
  user: User;
}

/** 会话 Context 暴露给组件的能力。 */
export interface SessionValue {
  /** 当前会话；未登录为 null。 */
  session: Session | null;
  /**
   * 是否已完成客户端初始化。
   * 注意 false **不代表未登录**，只代表「还没读到本地存储」。
   * 守卫必须先等它变 true 再判断跳转，否则会在刚进页面时误判成未登录。
   */
  hydrated: boolean;
  /** 登录成功后写入会话。 */
  signIn(token: string, user: User): void;
  /** 退出登录，清空会话。 */
  signOut(): void;
}

const SessionContext = createContext<SessionValue | null>(null);

/**
 * 会话 Provider，需要包在所有用到会话的页面外层（已挂在根布局里）。
 *
 * 实现上有两个关键约束：
 *
 * 1. **首屏必须渲染成「未登录」**。服务端渲染时读不到 localStorage，
 *    若在渲染期间直接读，客户端首帧会和服务端吐出的 HTML 不一致，
 *    React 会报 hydration 错误。因此读取只放在 useEffect 里，
 *    读完再 setHydrated(true)。
 * 2. **signIn / signOut 必须同时更新 localStorage 和 React 状态**。
 *    只动一边会得到两种错觉：「刷新后登录态还在」，或「UI 显示未登录，
 *    但 api.ts 的 request() 仍在带旧 token」——后者尤其难查，因为
 *    request() 读的是 localStorage，不读 React 状态。
 */
export function SessionProvider({ children }: { children: ReactNode }) {
  // 两个 state 职责不同：session 装会话内容，hydrated 标记「初始化是否完成」。
  // hydrated 的初值必须是 false——首屏还没有机会读到本地存储。
  const [session, setSession] = useState<Session | null>(null);
  const [hydrated, setHydrated] = useState(false);

  // 只在挂载后执行一次，因此依赖数组留空。
  // 若把 session 写进依赖，下面 setSession({ token, user }) 每次都会造出一个
  // 新对象（Object.is 判定为变化），触发重渲染 → effect 又因 session 变化重跑
  // → 再造新对象，形成无限循环。
  // 挂载时从 localStorage 恢复会话。它只在首屏执行一次，属于「与外部系统（浏览器存储）同步」，
  // 并非 react-hooks/set-state-in-effect 针对的那类「用 effect 派生状态」反例，
  // 代价仅为启动时多渲染一帧。
  // 撤销条件：改用 useSyncExternalStore 读 localStorage；或 token 迁到 httpOnly Cookie 后
  // 由服务端注入初始会话（届时 hydrated 这个标志可以连同豁免一起删掉）。
  useEffect(() => {
    const token = readToken();
    const user = readUser();
    if (token && user) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- 见上方说明
      setSession({ token, user });
    } else {
      // 只剩一半数据（如 token 在、user 被清掉）时视为未登录，
      // 并把残留清干净：否则 UI 说未登录，请求却仍带着旧 token。
      clearSession();
      setSession(null);
    }
    // 这一行不能漏，否则守卫会永远停在「初始化中」的加载态。
    setHydrated(true);
  }, []);

  // useMemo 固定 value 的引用，否则每次渲染都生成新对象，所有消费者白白重渲染。
  // signIn / signOut 不读取 session，所以只依赖 [session, hydrated] 也是安全的。
  const value = useMemo<SessionValue>(
    () => ({
      session,
      hydrated,
      signIn(token: string, user: User) {
        // 存储与状态必须成对更新，顺序无所谓，但不能只写一半。
        saveSession(token, user);
        setSession({ token, user });
      },
      signOut() {
        clearSession();
        setSession(null);
      },
    }),
    [session, hydrated],
  );

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

/**
 * 读取当前会话状态。必须在 <SessionProvider> 内使用。
 */
export function useSession(): SessionValue {
  const value = useContext(SessionContext);
  if (value === null) {
    throw new Error('useSession 必须在 <SessionProvider> 内使用');
  }
  return value;
}
