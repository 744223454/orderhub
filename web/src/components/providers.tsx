/**
 * 全局客户端 Provider 集合。
 *
 * 为什么需要这一层：根布局 layout.tsx 是服务端组件，而 ConfigProvider
 * 内部用了 hooks、SessionProvider 用了 useState，都不能直接在服务端组件里渲染。
 * 把它们收进这个带 'use client' 的文件，再由根布局引用即可。
 *
 * children 是以 props 形式传进来的，所以它仍然可以是服务端组件——
 * 这是 Next.js 里「用客户端组件包住服务端组件」的标准写法，
 * 并不会把所有页面都拖进客户端。
 */

'use client';

import type { ReactNode } from 'react';
import { ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';

import { SessionProvider } from '@/lib/auth';

/**
 * 挂载 antd 中文语言包与会话 Context。
 */
export function Providers({ children }: { children: ReactNode }) {
  return (
    <ConfigProvider locale={zhCN}>
      <SessionProvider>{children}</SessionProvider>
    </ConfigProvider>
  );
}
