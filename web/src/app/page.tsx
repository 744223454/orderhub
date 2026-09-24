'use client';

import { useState } from 'react';
import { Button, Card, Space, Typography } from 'antd';

/**
 * 首页占位。
 *
 * 这里放一个 antd 组件仅用于自检：若按钮有样式（蓝色实心）且点击可计数，
 * 说明 antd + AntdRegistry + 客户端组件边界三件事都配对了。
 * 实现完真实页面后可以删掉。
 */
export default function HomePage() {
  const [count, setCount] = useState(0);

  return (
    <main style={{ padding: 48 }}>
      <Card style={{ maxWidth: 520 }}>
        <Space orientation="vertical" size="middle">
          <Typography.Title level={3} style={{ margin: 0 }}>
            多角色订单系统
          </Typography.Title>
          <Typography.Paragraph type="secondary" style={{ margin: 0 }}>
            前端骨架已就绪，尚未接入任何业务页面。
          </Typography.Paragraph>
          <Button type="primary" onClick={() => setCount((value) => value + 1)}>
            自检按钮：{count}
          </Button>
        </Space>
      </Card>
    </main>
  );
}
