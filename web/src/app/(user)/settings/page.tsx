'use client';

import { Space } from 'antd';

import { PageContainer } from '@/components/page-container';

import { ChangePasswordCard } from './_components/change-password-card';
import { WecomBindCard } from './_components/wecom-bind-card';

/**
 * 账号设置页。
 *
 * 必须带 'use client'：页面里用到了 antd 组件与 PageContainer，
 * 都是客户端模块，在服务端组件里使用会失败。
 *
 * 落在 (user) 路由组，守卫只校验「已登录」不限角色，
 * 所以管理员、运营从管理端导航进来也是走这里。
 */
export default function SettingsPage() {
  return (
    <PageContainer title="账号设置" maxWidth={720}>
      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        <ChangePasswordCard />
        <WecomBindCard />
      </Space>
    </PageContainer>
  );
}
