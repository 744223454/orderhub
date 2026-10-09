'use client';

import { useState } from 'react';
import { Alert, Form, Input, Modal, Select } from 'antd';

import { ASSIGNABLE_ROLES, ROLE_LABELS } from '@/lib/roles';
import type { AdminUser, CreateUserRequest } from '@/lib/types';
import { api, ApiError } from '@/lib/api';

/** 新建用户弹窗的入参。 */
interface UserCreateModalProps {
  /** 是否打开，由父组件（用户管理页）控制——打开状态放外面，触发按钮与弹窗才好联动。 */
  open: boolean;
  /** 关闭弹窗（点取消 / 关闭图标 / 遮罩）时回调。 */
  onClose: () => void;
  /** 建号成功时回调，参数是后端返回的新用户；列表如何刷新由调用方决定。 */
  onCreated: (user: AdminUser) => void;
}

/**
 * 新建用户弹窗（仅管理员，权限由调用页面把关）。
 *
 * 表单字段与后端 adminCreateUserRequest 一一对应（username / password / role）。
 * 校验分工：前端只做最轻的拦截（必填 + 输入框长度上限），
 * 长度下限、用户名重复（409）、角色合法性一律以后端为准——
 * 规则复制两份迟早对不上，后端 400 / 409 的文案本身就能直接展示。
 */
export function UserCreateModal({ open, onClose, onCreated }: UserCreateModalProps) {
  const [form] = Form.useForm<CreateUserRequest>();
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  /** 关闭弹窗的统一入口：顺手清掉上一次的错误横幅，避免下次打开时残留。 */
  function handleClose(): void {
    setError(null);
    onClose();
  }

  /** 建号提交，表单校验通过后由 antd Form 触发。 */
  async function handleSubmit(values: CreateUserRequest): Promise<void> {
    if (submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      const created = await api.createUser(values);
      onCreated(created);
      handleClose();
      form.resetFields();
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        form.setFields([{ name: ['username'], errors: [e.message] }]);
      } else if (e instanceof ApiError) {
        setError(e.message);
      } else {
        setError('创建用户失败，请稍后重试');
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Modal
      open={open}
      title="新建用户"
      okText="确认创建"
      cancelText="取消"
      // onOk 走 form.submit()：让 antd 的表单校验管道接管提交，而不是在 onOk 里手写校验。
      onOk={() => form.submit()}
      onCancel={handleClose}
      confirmLoading={submitting}
    >
      {error !== null && <Alert type="error" title={error} showIcon style={{ marginBottom: 16 }} />}

      <Form
        form={form}
        layout="vertical"
        requiredMark={false}
        onFinish={handleSubmit}
        // 默认角色取普通用户（最小权限）；需要别的角色，管理员在表单里自己选。
        initialValues={{ role: 'user' }}
      >
        <Form.Item
          label="用户名"
          name="username"
          rules={[{ required: true, message: '请输入用户名' }]}
        >
          <Input maxLength={16} placeholder="1-16 个字符" />
        </Form.Item>

        <Form.Item label="密码" name="password" rules={[{ required: true, message: '请输入密码' }]}>
          <Input.Password maxLength={16} placeholder="8-16 个字符" />
        </Form.Item>

        <Form.Item label="角色" name="role" rules={[{ required: true, message: '请选择角色' }]}>
          <Select
            options={ASSIGNABLE_ROLES.map((role) => ({ value: role, label: ROLE_LABELS[role] }))}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}
