/**
 * 统一页面容器：页面标题区 + 内容区。
 *
 * 背景（体检记录）：各页面标题与宽度各自为政——设置页有大标题 + maxWidth 720，
 * 订单页没有页标题（靠 Card title 承担），管理端有的单卡、有的小卡双栏；
 * 间距 12 / 16 混用。统一后所有业务页面都用本组件包一轮。
 *
 * 区块间距约定（实现与接入时遵守）：
 * - 标题区与内容区之间：16；
 * - 页面内多区块（子卡片）之间：统一 16（原 12 的改用 16）。
 */

import type { ReactNode } from 'react';
import { Typography } from 'antd';

export interface PageContainerProps {
  /** 页面标题；省略时不渲染标题区（少见，尽量都给标题）。 */
  title?: ReactNode;
  /** 标题右侧的操作区（如「新建用户」按钮、组织架构页的「重新抓取」）。 */
  extra?: ReactNode;
  /** 内容区最大宽度（px）；默认不限（表格页全宽），窄表单页传 720。 */
  maxWidth?: number;
  children: ReactNode;
}

export function PageContainer(props: PageContainerProps) {
  const { title, extra, maxWidth, children } = props;
  return (
    <div style={{ maxWidth }}>
      {title !== undefined && (
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            marginBottom: 16,
          }}
        >
          <Typography.Title level={4} style={{ margin: 0 }}>
            {title}
          </Typography.Title>
          {extra}
        </div>
      )}
      {children}
    </div>
  );
}
