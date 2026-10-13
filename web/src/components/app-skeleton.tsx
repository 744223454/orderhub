/**
 * 全页加载骨架。
 *
 * 用在 AuthGuard 检查会话期间（替换原先的裸 <Spin />）：裸 Spin 没有外壳、
 * 没有居中，刷新任何受保护页面都是「白屏 + 左上角转个圈」，随后页面数据再
 * loading 一次——观感是连闪两下。骨架屏提前铺出页面的大形状，与真实内容
 * 切换时更平滑，也顺便让「第一次打开像白屏」的问题消失。
 *
 * 约束：
 * - 不依赖会话与路由：守卫检查期间就能渲染（此时 useSession 的 hydrated 还是 false）；
 * - 形状通用：一条顶栏 + 内容区两块卡片形占位，适配本项目「顶栏 + 卡片」的页面结构。
 */

import { Card, Skeleton } from 'antd';

/**
 * 设计备注：顶栏刻意用普通 div + 全内联样式，而不是 <Layout.Header>——
 * 后者脱离外层 <Layout> 单独使用时，--ant-layout-* 变量没有作用域，
 * height / padding 会静默失效、塌成内容高度；且它自带 line-height: 64px，
 * 会撑高内部块的行盒（2026-10-12 在浏览器实测复现）。
 * 改回 <Layout.Header> 之前，先处理好这两个坑。
 */
export function AppSkeleton() {
  return (
    <div style={{ minHeight: '100vh', background: '#f5f5f5' }}>
      <div
        style={{
          height: 64,
          display: 'flex',
          alignItems: 'center',
          gap: 24,
          paddingInline: 24,
          background: '#fff',
          borderBottom: '1px solid rgba(5, 5, 5, 0.06)',
        }}
      >
        <Skeleton.Button active size="small"></Skeleton.Button>
        <Skeleton.Button active size="small"></Skeleton.Button>
        <Skeleton.Button active size="small"></Skeleton.Button>
      </div>
      <div style={{ padding: 24 }}>
        <Card>
          <Skeleton active paragraph={{ rows: 2 }} />
        </Card>
        <Card>
          <Skeleton active paragraph={{ rows: 6 }} />
        </Card>
      </div>
    </div>
  );
}
