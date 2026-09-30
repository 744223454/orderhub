'use client';

import { Suspense } from 'react';
import { Spin } from 'antd';

import { CallbackShell, WecomCallback } from '@/components/wecom-callback';

/**
 * 企业微信登录回调页。
 *
 * 这个地址就是发给企微的 redirect_uri（后端按 WECOM_WEB_BASE_URL 拼出来），
 * 企微在用户扫码授权后会把浏览器 302 到这里并带上 ?code=...&state=...。
 *
 * ⚠️ 必须用 Suspense 包住：WecomCallback 内部用 useSearchParams，
 *    Next 15/16 在构建期会校验这一点，缺了会让 `npm run build` 直接失败。
 */
export default function WecomLoginCallbackPage() {
  return (
    <Suspense
      fallback={
        <CallbackShell>
          <Spin />
        </CallbackShell>
      }
    >
      <WecomCallback mode="login" />
    </Suspense>
  );
}
