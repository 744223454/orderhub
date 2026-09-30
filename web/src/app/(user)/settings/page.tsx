'use client';

import { Space, Typography } from 'antd';

import { WecomBindCard } from '@/components/wecom-bind-card';

/**
 * 账号设置页。
 *
 * 必须带 'use client'：页面里用到了 antd 的点子组件（Typography.Title），
 * 它们是客户端模块，在服务端组件里访问其属性会直接失败。
 *
 * 落在 (user) 路由组，守卫只校验「已登录」不限角色，
 * 所以管理员、运营从管理端导航进来也是走这里。
 */
export default function SettingsPage() {
  return (
    <div style={{ maxWidth: 720 }}>
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        账号设置
      </Typography.Title>

      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        <WecomBindCard />
      </Space>
    </div>
  );
}
