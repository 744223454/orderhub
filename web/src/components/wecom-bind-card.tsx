'use client';

import { useState } from 'react';
import { Alert, Button, Card, Typography } from 'antd';

import { api, ApiError } from '@/lib/api';

/**
 * 绑定企业微信卡片。
 *
 * 为什么需要绑定这条路：扫码登录的默认行为是「没绑过就新建一个普通账号」，
 * 于是管理员、运营这些预置角色的账号扫进来只会得到一个普通用户，
 * 还得回头用密码登录才能管单。绑定让他们能先用手上的密码登录、再把企业微信挂上去。
 */
export function WecomBindCard() {
  const [error, setError] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);

  /**
   * 取回授权链接并整页跳转。
   *
   * 成功后不重置 starting：浏览器马上要离开这个页面，保持 loading 才是正确的反馈。
   * 只有失败（跳不出去）时才需要把按钮恢复可点。
   */
  async function start(): Promise<void> {
    if (starting) return;
    setError(null);
    setStarting(true);
    try {
      const { url } = await api.wecomAuthorize('bind');
      // 授权页在企微自己的域下，只能整页跳转：fetch 拿不到跨站跳转，
      // 也不会把浏览器带过去。
      window.location.href = url;
    } catch (e) {
      setError(e instanceof ApiError ? e.message : '发起绑定失败，请稍后重试');
      setStarting(false);
    }
  }

  return (
    <Card title="企业微信">
      <Typography.Paragraph type="secondary" style={{ marginBottom: 16 }}>
        绑定后可直接用企业微信扫码登录当前账号，不必再输入密码。
      </Typography.Paragraph>

      {error !== null && <Alert type="error" title={error} showIcon style={{ marginBottom: 16 }} />}

      <Button type="primary" loading={starting} onClick={() => void start()}>
        绑定企业微信
      </Button>
    </Card>
  );
}
