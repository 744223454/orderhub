/**
 * 企业微信回调处理件。
 *
 * 为什么回调落在前端页面而不是后端接口：企微 302 回来时，后端接口要么返回一坨 JSON
 * 给用户看，要么再 302 回前端 —— 而后者绕不开「把访问令牌塞进 URL」（会进浏览器历史、
 * Referer 与各级访问日志）或者「再做一张一次性票据表」。让页面接收授权码、再转交后端换票，
 * 就是标准的授权码流程：令牌走响应体，两端都干净。
 *
 * 登录与绑定共用这一份实现：两者只有「回调后调哪个接口、成功后去哪」不同。
 */

'use client';

import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import { Alert, Button, Card, Spin, Typography } from 'antd';

import { api, ApiError } from '@/lib/api';
import { useSession } from '@/lib/auth';
import { homePathFor } from '@/lib/roles';
import { readToken } from '@/lib/storage';
import type { WecomIntent } from '@/lib/types';

export interface WecomCallbackProps {
  /** 授权用途：login 换票登录，bind 绑定到当前账号。 */
  mode: WecomIntent;
}

/** 回调相关页面统一的居中卡片容器，Suspense 的 fallback 也复用它。 */
export function CallbackShell({ children }: { children: ReactNode }) {
  return (
    <main
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: 24,
        background: '#f5f5f5',
      }}
    >
      <Card style={{ width: '100%', maxWidth: 400 }}>{children}</Card>
    </main>
  );
}

/**
 * 处理企微回调：校验参数 → 调后端换票/绑定 → 落地会话。
 *
 * 用 `useSearchParams` 取参，因此调用方必须把它包在 `<Suspense>` 里 —— Next 15/16 的硬性要求，
 * 否则 `next build` 会直接报 "useSearchParams() should be wrapped in a suspense boundary"。
 */
export function WecomCallback({ mode }: WecomCallbackProps) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { signIn } = useSession();

  const [error, setError] = useState<string | null>(null);
  const [succeeded, setSucceeded] = useState(false);

  /**
   * 一次性守卫，**不能省**：dev 下 `reactStrictMode: true` 会让 effect 跑两遍，
   * 而企微授权码只能用一次，第二次必然拿到 40029「授权已失效」。
   * 表现是「随机登录失败」，且只在开发环境出现 —— 最难排查的那类问题。
   * useRef 在 StrictMode 的二次执行之间不会被重置，因此能挡住。
   */
  const startedRef = useRef(false);

  const code = searchParams.get('code');
  const state = searchParams.get('state');

  // 「有 state、没有 code」= 用户在企微授权页点了拒绝（企微官方语义）。
  // 这不是错误，直接由 URL 派生，不写进 state —— 也就少一帧中间渲染。
  const cancelled = code === null || state === null;

  const isBind = mode === 'bind';
  const backPath = isBind ? '/orders' : '/login';

  useEffect(() => {
    if (startedRef.current) return;
    startedRef.current = true;

    // 显式判空而不是复用 cancelled：闭包里 TS 不做跨变量收窄。
    if (code === null || state === null) return;

    void (async () => {
      try {
        if (isBind) {
          // 绑定依赖登录态：会话在 localStorage 里，后端靠 Authorization 头识别用户。
          if (readToken() === null) {
            setError('请先登录后再绑定企业微信');
            return;
          }
          await api.wecomBind({ code, state });
          setSucceeded(true);
          return;
        }

        const { token, user } = await api.wecomLogin({ code, state });
        // 必须走 signIn 而不是裸写 localStorage：守卫读的是 Context，
        // 只写存储会让「UI 说已登录、请求也带上了 token」两处状态对不上，
        // 症状是人刚登录就被弹回登录页。
        signIn(token, user);
        router.replace(homePathFor(user.role));
      } catch (e) {
        setError(e instanceof ApiError ? e.message : '操作失败，请稍后重试');
      }
    })();
  }, [code, state, isBind, signIn, router]);

  return (
    <CallbackShell>
      <Typography.Title level={3} style={{ marginTop: 0, marginBottom: 4 }}>
        {isBind ? '绑定企业微信' : '企业微信登录'}
      </Typography.Title>
      <Typography.Paragraph type="secondary" style={{ marginBottom: 24 }}>
        {isBind ? '把企业微信关联到当前账号' : '正在校验企业微信授权结果'}
      </Typography.Paragraph>

      {error !== null && <Alert type="error" title={error} showIcon style={{ marginBottom: 16 }} />}

      {error === null && cancelled && (
        <Alert type="warning" title="已取消授权" showIcon style={{ marginBottom: 16 }} />
      )}

      {error === null && !cancelled && succeeded && (
        <Alert
          type="success"
          title="绑定成功，下次可直接扫码登录"
          showIcon
          style={{ marginBottom: 16 }}
        />
      )}

      {error === null && !cancelled && !succeeded && (
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Spin size="small" />
          <Typography.Text type="secondary">正在处理授权结果…</Typography.Text>
        </div>
      )}

      <div style={{ marginTop: 24 }}>
        {/* 用 Button 的 href 而不是外包一层 Link：后者会渲染出 <a><button>，
            嵌套可聚焦元素，点击行为与无障碍语义都会出问题。 */}
        <Button type="link" href={backPath} style={{ padding: 0 }}>
          {isBind ? '返回我的订单' : '返回登录页'}
        </Button>
      </div>
    </CallbackShell>
  );
}
