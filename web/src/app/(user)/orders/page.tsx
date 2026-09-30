'use client';

import { useCallback, useEffect, useState } from 'react';
import type { TableProps } from 'antd';
import { Alert, Button, Card, Form, Input, InputNumber, Space, Table, Typography } from 'antd';

import { OrderStatusTag } from '@/components/order-status-tag';
import { api, ApiError } from '@/lib/api';
import { formatAmount } from '@/lib/format';
import type { Order, OrderStatus } from '@/lib/types';

/**
 * 新建订单表单的字段。
 *
 * 特意不复用后端的 CreateOrderRequest：那里 amount 的单位是**分**，
 * 而人在这里填的是**元**。两者单位不同就别共用同一个类型，
 * 换算发生在提交那一刻，写在一个地方。
 */
interface CreateOrderFormValues {
  product_name: string;
  /** 金额，单位：元。 */
  amount: number;
}

/**
 * 我的订单页。
 *
 * 这是登录后普通用户的落地页，也是整个前端验证「接口是否设计得顺手」的第一现场。
 */
export default function MyOrdersPage() {
  // 用 null 表示「首屏还没加载完」，所以 loading 是**算出来的**（orders === null），
  // 不必再额外维护一个 boolean state 去和真实数据保持同步。
  const [orders, setOrders] = useState<Order[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  // 行内操作（支付）的状态与错误，与整页加载分开：
  // 否则点一次支付会把整张表刷成 loading 态。
  const [payingId, setPayingId] = useState<number | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const [form] = Form.useForm<CreateOrderFormValues>();

  /**
   * 拉取自己的订单列表。
   *
   * 特意抽成函数而不是把取数直接写进 effect：**要复用**。
   * 首屏加载、以及「状态已被别处改过」时重新拉取，都走同一个入口。
   */
  const loadOrders = useCallback(async (): Promise<void> => {
    try {
      // 后端已把空列表兜底成 []（handler.go 的 emptyIfNil），
      // 所以这里不会拿到 null，不需要 `?? []` 这类防御。
      setOrders(await api.listMyOrders());
      // 成功时顺手清掉上次的错误，否则「重试成功」之后旧的红色横幅还挂在页面上。
      setLoadError(null);
    } catch (e) {
      setLoadError(e instanceof ApiError ? e.message : '加载订单失败');
    }
  }, []);

  // 首屏加载。loadOrders 被 useCallback 固定了引用，所以这个 effect 只会跑一次。
  //
  // ⚠️ 这里会触发一条 react-hooks/set-state-in-effect 的 warn，是**预期**的，不是漏改：
  //    该规则顺着调用链看到「effect 调用的函数里有 setState」就报，**与 setState 排在
  //    第几行、在不在 await 之后都无关**（2026-09-30 实测：同样写在 await 之后，
  //    effect 内 async IIFE 不报、useCallback + effect 调用则报）。
  //    规则已在本项目降级为 warn（见 web/eslint.config.mjs 末尾）。
  //    之所以接受这条 warn 而不是把取数塞回 effect 里写成 IIFE——因为 loadOrders 要被
  //    handlePay 的 409 分支复用（「状态被别处改过」时重拉），为消一条警告把取数逻辑
  //    复制两遍并不划算。
  useEffect(() => {
    void loadOrders();
  }, [loadOrders]);

  /**
   * 新建订单。表单校验通过后触发。
   */
  async function handleCreate(values: CreateOrderFormValues): Promise<void> {
    if (creating) return;
    setCreating(true);
    setActionError(null);
    try {
      const created = await api.createOrder({
        product_name: values.product_name,
        // 单位换算：表单收的是「元」，后端要的是「分」。
        // ⚠️ Math.round 不能省：19.99 * 100 在 IEEE 754 下等于 1998.9999999999998，
        //    截断后后端收到 1998，不报错但金额静默少一分。
        amount: Math.round(values.amount * 100),
      });
      // 插到队首而不是追加：仓库层 ListByUser 按 created_at DESC 返回（最新在前），
      // 追加会让新单排在列表末尾，条数超过一页时用户根本看不见自己刚下的单。
      setOrders((prev) => [created, ...(prev ?? [])]);
      // 只在成功时清空：失败时保留用户已填内容，别让人重打一遍。
      form.resetFields();
    } catch (e) {
      // 创建失败属于「行内操作错误」，显示在操作区；loadError 专管整页加载失败。
      setActionError(e instanceof ApiError ? e.message : '创建订单失败，请稍后重试');
    } finally {
      setCreating(false);
    }
  }

  /**
   * 支付某笔订单（pending → paid）。
   */
  async function handlePay(id: number): Promise<void> {
    setPayingId(id);
    setActionError(null);
    try {
      const updated = await api.payOrder(id);
      // 局部替换那一条，而不是重拉整张表：后端流转是 CAS，
      // 返回的 Order 就是这次操作的权威值。
      // ⚠️ 别写成 [...prev, updated]——原行还在、又追加一条同 id 的记录，
      //    rowKey="id" 撞 key，React 会报 duplicate key，表格行为变得诡异。
      setOrders((prev) => prev?.map((o) => (o.id === id ? updated : o)) ?? null);
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        // 状态已被别处改过（另一个标签页 / 管理端刚操作过这张单）。
        // 先提示、再重拉，让用户看到真实状态——这正是 loadOrders 抽成函数的原因。
        setActionError('订单状态已变化，已为你刷新列表');
        await loadOrders();
      } else if (e instanceof ApiError) {
        // 这里的 404 有两种来源：订单真的不存在，或**越权操作他人订单**——
        // 后端故意不区分（防 ID 枚举，见 internal/order/service.go 的 findForUser），
        // 所以文案直接用后端给的「订单不存在」，不要自作主张写成「订单已被删除」。
        setActionError(e.message);
      } else {
        // 网络中断、响应体不是 JSON 等非 ApiError：必须兜底。
        // 少了这一支，用户点了「支付」就是毫无反馈——最难排查的那种 bug。
        setActionError('支付失败，请稍后重试');
      }
    } finally {
      setPayingId(null);
    }
  }

  /**
   * 表格列定义。
   *
   * 用 `TableProps<Order>['columns']` 取列类型，而不是从 'antd/es/table' 深路径导入 ColumnsType——
   * 前者不依赖内部目录结构，antd 升级时不会因为文件挪位置而崩。
   */
  const columns: TableProps<Order>['columns'] = [
    { title: '订单号', dataIndex: 'id', width: 90 },
    { title: '商品', dataIndex: 'product_name' },
    {
      title: '金额',
      dataIndex: 'amount',
      width: 120,
      render: (amount: number) => formatAmount(amount),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      // 文案与配色统一在 lib/order-status.ts 里，管理端用的是同一个组件，
      // 两页不会出现「文案一样、颜色不一样」这种分叉。
      render: (status: OrderStatus) => <OrderStatusTag status={status} />,
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      width: 180,
      // 首屏渲染时 orders 还是 null（表格无数据），所以这里不会参与服务端/客户端首帧比对，
      // 不必担心时区导致的 hydration 不一致。将来若改成服务端注入数据，这里要重新考虑。
      render: (createdAt: string) => new Date(createdAt).toLocaleString('zh-CN'),
    },
    {
      title: '操作',
      width: 100,
      render: (_: unknown, order: Order) =>
        order.status === 'pending' ? (
          <Button
            type="link"
            style={{ padding: 0 }}
            loading={payingId === order.id}
            onClick={() => void handlePay(order.id)}
          >
            支付
          </Button>
        ) : (
          <Typography.Text type="secondary">—</Typography.Text>
        ),
    },
  ];

  return (
    // 用 orientation 而不是 direction：v6 里 direction 已标 @deprecated（v7 会移除），
    // 控制台实测会打印 `[antd: Space] 'direction' is deprecated`。
    <Space orientation="vertical" size={16} style={{ width: '100%' }}>
      <Card title="新建订单">
        <Form<CreateOrderFormValues>
          form={form}
          layout="inline"
          requiredMark={false}
          onFinish={handleCreate}
        >
          <Form.Item
            label="商品名称"
            name="product_name"
            rules={[{ required: true, message: '请输入商品名称' }]}
          >
            <Input placeholder="例如：机械键盘" style={{ width: 200 }} />
          </Form.Item>

          <Form.Item
            label="金额"
            name="amount"
            // min: 1 不能去掉：后端 createOrderRequest 的 `binding:"required"` 对整数意味着
            // 「非零」，amount = 0 会被绑定层拦成 400「请求参数不合法」——文案里根本不提金额。
            rules={[
              { required: true, message: '请输入金额' },
              { type: 'number', min: 1, message: '金额至少 1 元' },
            ]}
          >
            <InputNumber min={1} precision={2} placeholder="单位：元" style={{ width: 160 }} />
          </Form.Item>

          <Form.Item style={{ marginBottom: 0 }}>
            <Button type="primary" htmlType="submit" loading={creating}>
              创建订单
            </Button>
          </Form.Item>
        </Form>
      </Card>

      <Card title="我的订单">
        {loadError !== null && (
          <Alert type="error" title={loadError} showIcon style={{ marginBottom: 16 }} />
        )}
        {actionError !== null && (
          <Alert type="error" title={actionError} showIcon style={{ marginBottom: 16 }} />
        )}

        <Table<Order>
          rowKey="id"
          columns={columns}
          // orders 为 null 时给空数组，Table 才不会因为 dataSource 是 null 而崩。
          dataSource={orders ?? []}
          // loading 直接由「数据是否到位」算出来，不用额外的 state。
          loading={orders === null}
          // 后端目前是全量返回、没有分页参数，所以这里只是前端分页；
          // 数据量大了要改成后端分页（属于后续「业务纵深」的一部分）。
        />
      </Card>
    </Space>
  );
}
