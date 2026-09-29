'use client';

import { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Card, Form, Input, InputNumber, Space, Table, Tag, Typography } from 'antd';
import type { TableProps } from 'antd';

import { api, ApiError } from '@/lib/api';
import type { Order, OrderStatus } from '@/lib/types';

/**
 * 订单状态的展示映射。
 *
 * 用 Record<OrderStatus, ...> 而不是普通对象：将来后端加了新状态、
 * types.ts 的 OrderStatus 跟着扩了联合类型，这里漏填会**直接编译报错**，
 * 不会静默显示成空白。
 */
const STATUS_META: Record<OrderStatus, { text: string; color: string }> = {
  pending: { text: '待支付', color: 'default' }, // TODO: color 由你定（可选预设：default / processing / success / warning / error）
  paid: { text: '已支付', color: 'default' },
  shipped: { text: '已发货', color: 'default' },
  completed: { text: '已完成', color: 'default' },
  refunded: { text: '已退款', color: 'default' },
};

/**
 * 把「分」格式化成「元」。
 *
 * 金额在后端一律是整数分（见 internal/order/model.go 的 Amount 注释），
 * 只在展示这一刻换算。
 */
function formatAmount(amount: number): string {
  // TODO: 由你实现。两个坑：
  //  1. 不要用浮点累加（0.1 + 0.2），也不要对 money 做浮点运算后再比较；
  //  2. 单纯展示用 (amount / 100).toFixed(2) 就够；想更严谨可用整数运算：
  //     `${Math.trunc(amount / 100)}.${String(amount % 100).padStart(2, '0')}`
  return `${Math.trunc(amount / 100)}.${String(amount % 100).padStart(2, '0')} 元`;
}

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
    // TODO: 由你实现。步骤：
    //   try { setOrders(await api.listMyOrders()); setLoadError(null); }
    //   catch (e) { setLoadError(e instanceof ApiError ? e.message : '加载订单失败'); }
    // 后端已把空列表兜底成 []（handler.go 的 emptyIfNil），所以这里不会拿到 null。
    //
    // 小技巧：把 setState **都放在 await 之后**（清空错误也放那儿），
    // 连 react-hooks/set-state-in-effect 的黄字警告都不会有。
    // 那个规则在本项目已被降级为 warn（见 web/eslint.config.mjs 末尾的说明），
    // 所以就算写在前面也只是提醒、不会挡住提交。
  }, []);

  // 首屏加载。loadOrders 被 useCallback 固定了引用，所以这个 effect 只会跑一次。
  useEffect(() => {
    void loadOrders();
  }, [loadOrders]);

  /**
   * 新建订单。表单校验通过后触发。
   */
  async function handleCreate(values: CreateOrderFormValues): Promise<void> {
    // TODO: 由你实现。要点：
    //  1. 置 creating = true、清空 actionError；
    //  2. **单位换算**：amount 从「元」换成「分」再发请求。
    //     const created = await api.createOrder({
    //       product_name: values.product_name,
    //       amount: Math.round(values.amount * 100),   // ← 必须四舍五入
    //     });
    //     ⚠️ 别漏 Math.round：19.99 * 100 在 JS 里等于 1998.9999999999998，
    //        后端收到 1998（少一分）不会报错，但金额就错了。
    //  3. 把 created 追加进列表：setOrders((prev) => [...(prev ?? []), created]);
    //  4. catch：ApiError 的 message 直接展示即可（后端给的是中文）；
    //     其他错误统一 '创建订单失败，请稍后重试'；
    //  5. finally：creating = false；成功时 form.resetFields()。
  }

  /**
   * 支付某笔订单（pending → paid）。
   */
  async function handlePay(id: number): Promise<void> {
    // TODO: 由你实现。要点：
    //  1. 置 payingId = id、清空 actionError；
    //  2. const updated = await api.payOrder(id);
    //     然后用返回值**局部替换**那一条（比整表重拉更好，后端流转是 CAS，返回的 Order 是权威值）：
    //     setOrders((prev) => prev?.map((o) => (o.id === id ? updated : o)) ?? null);
    //  3. catch 要**分开处理**（err instanceof ApiError 时可读 err.status）：
    //      409「订单当前状态不允许该操作」→ 状态已被别处改过（另一个标签页 / 管理端刚操作），
    //          提示用户并重新拉一次列表（直接 `await loadOrders()`，这就是把它抽成函数的原因）；
    //      404「订单不存在」→ 注意：后端对**越权访问他人订单**是故意返回 404 的
    //          （防 ID 枚举，见 internal/order/service.go），别提示成「订单没了」；
    //  4. finally：payingId = null。
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
      render: (status: OrderStatus) => (
        <Tag color={STATUS_META[status].color}>{STATUS_META[status].text}</Tag>
      ),
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
