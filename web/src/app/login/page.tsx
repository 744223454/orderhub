'use client';

import { useState } from 'react';
import { Alert, Button, Card, Divider, Form, Input, Typography } from 'antd';
import type { LoginRequest } from '@/lib/types';
import { api, ApiError } from '@/lib/api';
import { useSession } from '@/lib/auth';
import { homePathFor } from '@/lib/roles';
import { useRouter } from 'next/navigation';

/**
 * 登录页。
 *
 * 表单的字段名必须和后端 `loginRequest` 的 json tag 一致（username / password），
 * 改了这里没改后端，表现是恒定 400「请求参数不合法」。
 *
 * 注意：后端 Login 有意**不校验密码长度**（长度约束只在 Register 生效），
 * 所以前端也不要加最小长度规则，否则本地那个 5 位密码的 admin 账号登不进来。
 */
export default function LoginPage() {
  const [error, setError] = useState<string | null>(null);
  // 防重复提交必须用 state（或 useRef）——普通局部变量每次调用都是全新的 false，
  // 既拦不住连点，也没法驱动按钮的 loading 态。
  const [submitting, setSubmitting] = useState(false);
  // 企业微信登录是「跳走」型操作，与表单提交互不影响，因此单独一份 loading 状态，
  // 免得点它把密码登录的按钮也一起变成加载中。
  const [wecomStarting, setWecomStarting] = useState(false);
  // 这两个 Hook 必须在组件函数体顶层调用，不能挪进 handleFinish：
  // Hook 依赖 React 在渲染期间才设置的 dispatcher，渲染之外调用会直接抛
  // `Invalid hook call`（实测细节见下方 handleFinish 的注释）。
  const router = useRouter();
  const { signIn } = useSession();

  /**
   * 表单校验通过后触发。
   *
   * 三个必须记住的点：
   *
   * 1. **Hook 只能在组件函数体顶层调用**，不能放进事件处理函数。
   *    放进去时 `useRouter()` / `useSession()` 内部读不到 dispatcher，点击会抛
   *    `Invalid hook call`（实测：渲染之外调 `useContext` 报的是
   *    "Cannot read properties of null (reading 'useContext')"）。
   *    而 `useSession()` 排在 `await` 之后的话，表现更隐蔽——
   *    请求明明发出去了，然后「什么都没发生」。
   * 2. **防重复提交要用 state / ref**。真正挡住连点的是按钮的 `loading`：
   *    antd 的 Button 在 loading 时会让点击直接 return。上面那行 `if (submitting) return`
   *    只是兜底（state 更新是异步的，同一次事件循环内的连点读到的是旧值）。
   * 3. **用 `signIn` 而不是裸 `saveSession`**。saveSession 只写 localStorage、不更新 Context，
   *    而守卫读的是 Context——会话状态对不上，会被弹回登录页。
   */
  async function handleFinish(values: LoginRequest): Promise<void> {
    if (submitting) return;
    setError(null);
    setSubmitting(true);
    try {
      const { token, user } = await api.login(values);
      signIn(token, user);
      router.replace(homePathFor(user.role));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : '登录失败，请稍后重试');
    } finally {
      setSubmitting(false);
    }
  }

  /**
   * 发起企业微信扫码登录。
   *
   * 成功时**不重置 loading**：浏览器马上要离开这个页面，按钮保持加载中才是正确反馈；
   * 只有取授权链接失败（走不掉）时才需要恢复可点。
   *
   * 用 window.location.href 而不是 fetch：目标是企微自己的授权页，
   * 属于整页跳转，fetch 既拿不到那个页面，也不会把用户带过去。
   */
  async function handleWecomLogin(): Promise<void> {
    if (wecomStarting) return;
    setError(null);
    setWecomStarting(true);
    try {
      const { url } = await api.wecomAuthorize('login');
      window.location.href = url;
    } catch (e) {
      setError(e instanceof ApiError ? e.message : '企业微信登录发起失败，请稍后重试');
      setWecomStarting(false);
    }
  }

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
      <Card style={{ width: '100%', maxWidth: 400 }}>
        <Typography.Title level={3} style={{ marginTop: 0, marginBottom: 4 }}>
          登录
        </Typography.Title>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 24 }}>
          多角色订单系统
        </Typography.Paragraph>

        {error !== null && (
          <Alert type="error" title={error} showIcon style={{ marginBottom: 16 }} />
        )}

        <Form<LoginRequest> layout="vertical" requiredMark={false} onFinish={handleFinish}>
          <Form.Item
            label="用户名"
            name="username"
            rules={[{ required: true, message: '请输入用户名' }]}
          >
            <Input placeholder="请输入用户名" autoComplete="username" />
          </Form.Item>

          <Form.Item
            label="密码"
            name="password"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password placeholder="请输入密码" autoComplete="current-password" />
          </Form.Item>

          <Form.Item style={{ marginBottom: 0 }}>
            <Button type="primary" htmlType="submit" block loading={submitting}>
              登录
            </Button>
          </Form.Item>
        </Form>

        <Divider plain style={{ margin: '20px 0', fontSize: 12 }}>
          或
        </Divider>

        <Button block loading={wecomStarting} onClick={() => void handleWecomLogin()}>
          企业微信登录
        </Button>

        <Typography.Paragraph
          type="secondary"
          style={{ marginTop: 16, marginBottom: 0, fontSize: 12 }}
        >
          本地开发账号：admin / admin（角色 admin）
          <br />
          企业微信登录需从 http://dev.orderhub.local:3000 进入（企微只认配置好的授权回调域）
        </Typography.Paragraph>
      </Card>
    </main>
  );
}
