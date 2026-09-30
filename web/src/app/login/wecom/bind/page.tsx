'use client';

import { Suspense } from 'react';
import { Spin } from 'antd';

import { CallbackShell, WecomCallback } from '@/components/wecom-callback';

/**
 * 企业微信绑定回调页。
 *
 * 与登录回调页成对：绑定的 redirect_uri 指向这里，落点由后端按 intent=bind 拼出。
 * 之所以单独开一个页面而不是复用登录回调页，是因为两者成功后的落点不同
 * —— 登录要跳转进系统，绑定只需给个反馈。放在同一个页面里会让「现在到底在登录还是绑定」
 * 变成一个需要靠 URL 参数猜的状态。
 *
 * 这个页面本身是公开的（不在 (user) / (staff) 守卫组内），
 * 但换票/绑定接口需要登录态，由页面把 localStorage 里的 token 通过 Authorization 头发给后端。
 */
export default function WecomBindCallbackPage() {
  return (
    <Suspense
      fallback={
        <CallbackShell>
          <Spin />
        </CallbackShell>
      }
    >
      <WecomCallback mode="bind" />
    </Suspense>
  );
}
