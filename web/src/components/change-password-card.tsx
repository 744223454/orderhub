'use client';

import { useEffect, useState } from 'react';
import { Alert, Button, Card, Form, Input, Typography } from 'antd';

import { api, ApiError } from '@/lib/api';

/** 修改密码表单的字段。确认密码只在前端校验，不会发给后端。 */
interface ChangePasswordFormValues {
  old_password?: string;
  new_password: string;
  confirm_password: string;
}

/**
 * 修改 / 设置密码卡片。
 *
 * 两类账号共用一张卡片，由 GET /api/me 的 has_password 决定表单形态：
 * - 已设置过密码的账号（注册、管理员建号）：显示「原密码」，必须验证；
 * - 无密码账号（企业微信扫码自动建号）：不显示「原密码」，直接设置新密码。
 *
 * 是否显示由服务端判定，前端不自行假设；设置成功后同一张卡片自动切换为「修改」形态。
 */
export function ChangePasswordCard() {
  const [form] = Form.useForm<ChangePasswordFormValues>();
  const [submitting, setSubmitting] = useState(false);
  const [success, setSuccess] = useState<string | null>(null);
  /** 当前账号是否已设置过密码；null = 尚未拉到（此阶段不渲染原密码输入框）。 */
  const [hasPassword, setHasPassword] = useState<boolean | null>(null);
  const [error, setError] = useState<string | null>(null);

  // 挂载时拉取账号状态，决定表单显示「设置密码」还是「修改密码」形态。
  // 失败时保持 null 并提示，不回落成 false —— 把有密码账号误判成无密码，
  // 用户就看不到「原密码」输入框了。
  useEffect(() => {
    void (async () => {
      try {
        const me = await api.me();
        setHasPassword(me.has_password);
      } catch (e) {
        setError(e instanceof ApiError ? e.message : '获取账号状态失败，请刷新页面重试');
      }
    })();
  }, []);

  /** 提交修改密码请求，表单校验通过后由 antd Form 触发。 */
  async function handleSubmit(values: ChangePasswordFormValues): Promise<void> {
    if (submitting) return;
    setSubmitting(true);
    setError(null);
    setSuccess(null);
    try {
      // old_password 空串与省略等价：服务端按账号状态决定是否校验，客户端无需区分两种形态。
      await api.changePassword({
        old_password: values.old_password ?? '',
        new_password: values.new_password,
      });
      setHasPassword(true);
      setSuccess('密码修改成功！');
      form.resetFields();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : '密码修改失败，请稍后重试');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Card title="登录密码">
      <Typography.Paragraph type="secondary" style={{ marginBottom: 16 }}>
        {hasPassword === null
          ? '正在获取账号状态…'
          : hasPassword
            ? '修改密码后请用新密码登录。'
            : '当前账号尚未设置密码（由企业微信自动创建）。设置后即可用密码登录。'}
      </Typography.Paragraph>

      {success !== null && (
        <Alert type="success" title={success} showIcon style={{ marginBottom: 16 }} />
      )}
      {error !== null && <Alert type="error" title={error} showIcon style={{ marginBottom: 16 }} />}

      <Form form={form} layout="vertical" onFinish={(values) => void handleSubmit(values)}>
        {hasPassword === true && (
          <Form.Item
            name="old_password"
            label="原密码"
            rules={[{ required: true, message: '请输入原密码' }]}
          >
            <Input.Password autoComplete="current-password" />
          </Form.Item>
        )}

        <Form.Item
          name="new_password"
          label={hasPassword === true ? '新密码' : '设置密码'}
          rules={[
            { required: true, message: '请输入新密码' },
            { min: 8, max: 16, message: '密码长度需在 8 到 16 个字符之间' },
          ]}
        >
          <Input.Password autoComplete="new-password" />
        </Form.Item>

        <Form.Item
          name="confirm_password"
          label="确认新密码"
          dependencies={['new_password']}
          rules={[
            { required: true, message: '请再次输入新密码' },
            ({ getFieldValue }) => ({
              validator(_, value: string) {
                if (!value || getFieldValue('new_password') === value) {
                  return Promise.resolve();
                }
                return Promise.reject(new Error('两次输入的密码不一致'));
              },
            }),
          ]}
        >
          <Input.Password autoComplete="new-password" />
        </Form.Item>

        <Button type="primary" htmlType="submit" loading={submitting}>
          {hasPassword === true ? '修改密码' : '设置密码'}
        </Button>
      </Form>
    </Card>
  );
}
