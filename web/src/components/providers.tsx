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
import { App, ConfigProvider } from 'antd';
import type { ThemeConfig } from 'antd';
import zhCN from 'antd/locale/zh_CN';

import { SessionProvider } from '@/lib/auth';

/**
 * 全局主题配置（主题入口）。
 *
 * 当前视觉与 antd 默认一致，所以不覆盖任何 token——先立「入口」再谈定制：
 * 以后调品牌色、圆角、布局底色只动这一处。配套约定：业务代码不写死色值，
 * 取色一律用 theme.useToken() 的语义 token（colorBgLayout 页面底色 /
 * colorBgContainer 容器面 / colorSplit 分隔线）。
 *
 * 必须保持为模块级常量：官方 FAQ 说明 ConfigProvider 的 theme 在 undefined
 * 与对象之间切换时整棵组件树会重挂载，传稳定对象则没有这个问题。
 */
const appTheme: ThemeConfig = {};

/**
 * 挂载 antd 中文语言包、全局主题与会话 Context。
 */
export function Providers({ children }: { children: ReactNode }) {
  return (
    <ConfigProvider locale={zhCN} theme={appTheme}>
      <App>
        <SessionProvider>{children}</SessionProvider>
      </App>
    </ConfigProvider>
  );
}
