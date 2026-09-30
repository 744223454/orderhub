'use client';

import { useCallback, useEffect, useState } from 'react';
import type { TableProps } from 'antd';
import { Alert, Button, Card, Space, Table, Typography } from 'antd';

import { OrderStatusTag } from '@/components/order-status-tag';
import { api, ApiError } from '@/lib/api';
import { formatAmount } from '@/lib/format';
import type { Order, OrderStatus } from '@/lib/types';

/** 管理端对订单可执行的三种流转操作，与 ACTION_META 的键一一对应。 */
type AdminAction = 'ship' | 'complete' | 'refund';

/**
 * 状态 → 允许的操作。
 *
 * 必须与后端状态机一致（internal/order/service.go）：前端多给一个按钮，
 * 用户点下去只会撞 409。用 Record 而不是一串 if / else，是为了后端加状态、
 * types.ts 扩了联合类型时，这里漏填**直接编译报错**。
 */
const ACTIONS_BY_STATUS: Record<OrderStatus, AdminAction[]> = {
  pending: [],
  paid: ['ship', 'refund'],
  shipped: ['complete', 'refund'],
  completed: [],
  refunded: [],
};

/**
 * 操作 → 展示文案与执行入口。
 *
 * run 集中在这里，操作列与 handleAction 就都不用写 switch：将来加一个操作，
 * 本表加一条、再挂到 ACTIONS_BY_STATUS 对应状态上即可。
 */
const ACTION_META: Record<AdminAction, { label: string; run: (id: number) => Promise<Order> }> = {
  ship: { label: '发货', run: (id) => api.shipOrder(id) },
  complete: { label: '完成', run: (id) => api.completeOrder(id) },
  refund: { label: '退款', run: (id) => api.refundOrder(id) },
};

/**
 * 订单管理页（运营 / 管理员）。
 *
 * 后端 /api/admin/* 全部要求 admin 或 ops 角色，
 * 角色不符时后端返回 403，前端由 (staff)/layout.tsx 的守卫提前拦下。
 *
 * 骨架与 (user)/orders/page.tsx 同源，差别只有两处：
 *   1. 取数用 api.listAllOrders()（全量订单），不是 api.listMyOrders()；
 *   2. 「操作」列要按行内状态，给出**后端此刻真正允许**的流转操作。
 * 第 2 条是本页唯一需要你手写判断的地方，规则与坑写在 columns 的「操作」列注释里。
 */
export default function AdminOrdersPage() {
  // 与用户端同款：用 null 表示「首屏还没加载完」，loading 由 orders === null 直接算出来，
  // 不必再额外维护一个 boolean state 去和真实数据保持同步（两者迟早会不同步）。
  const [orders, setOrders] = useState<Order[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  // 行内操作的状态与错误，与整页加载分开：否则点一次发货，整张表都会变成 loading。
  // actingId 记的是「哪一行在处理」，只让那一行的按钮转圈。
  const [actingId, setActingId] = useState<number | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  /**
   * 拉取全量订单。
   *
   * 抽成函数而不是把取数直接写进 effect，是为了让「行内操作撞上 409」时能复用同一个入口重拉
   * ——订单可能已被另一个标签页 / 另一位管理员改过，手上这一条已经不新鲜了。
   */
  const loadOrders = useCallback(async (): Promise<void> => {
    try {
      // 后端已把空列表兜底成 []，所以这里不会拿到 null，不需要 `?? []` 这类防御。
      setOrders(await api.listAllOrders());
      // 成功时顺手清掉上次的加载错误，否则重试成功之后旧的红色横幅还挂在页面上。
      setLoadError(null);
    } catch (e) {
      setLoadError(e instanceof ApiError ? e.message : '加载订单失败');
    }
  }, []);

  async function handleAction(id: number, action: AdminAction): Promise<void> {
    setActingId(id);
    setActionError(null);
    try {
      const updated = await ACTION_META[action].run(id);
      setOrders((prev) => prev?.map((o) => (o.id === id ? updated : o)) ?? null);
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        setActionError('状态已变化，正在拉取最新数据');
        await loadOrders();
      } else if (e instanceof ApiError) {
        setActionError(e.message);
      } else {
        setActionError(`${ACTION_META[action].label}失败，请稍后重试`);
      }
    } finally {
      setActingId(null);
    }
  }

  // 首屏加载。loadOrders 被 useCallback 固定了引用，所以这个 effect 只会跑一次。
  //
  // ⚠️ 这会触发一条 react-hooks/set-state-in-effect 的 warn，是**预期**的、不是漏改：
  //    该规则已在本项目降级为 warn（web/eslint.config.mjs 末尾），原因与用户端页相同，
  //    详见 (user)/orders/page.tsx 里同一处的长注释。
  useEffect(() => {
    void loadOrders();
  }, [loadOrders]);

  /**
   * 表格列定义。
   *
   * 用 `TableProps<Order>['columns']` 取列类型，而不是从 'antd/es/table' 深路径导入 ColumnsType——
   * 前者不依赖 antd 的内部目录结构，antd 升级挪了文件也不会崩。
   */
  const columns: TableProps<Order>['columns'] = [
    { title: '订单号', dataIndex: 'id', width: 90 },
    // 管理端看的是**别人的**订单，「谁下的单」必须上屏，这是与用户端列表的第一处差异。
    // 注意后端目前只返回 user_id、没有用户名（internal/order/model.go 里没有 join user），
    // 想显示用户名要么改后端、要么前端另调接口，别在这一列上 hardcode 猜。
    { title: '下单用户', dataIndex: 'user_id', width: 110 },
    { title: '商品', dataIndex: 'product_name' },
    {
      title: '金额',
      dataIndex: 'amount',
      width: 120,
      render: (amount: number) => formatAmount(amount),
    },
    // 状态列用公共组件：文案与配色统一在 lib/order-status.ts 的映射里，
    // 与用户端「我的订单」共用同一份，所以这里不要再手写 Tag（否则两页又会分叉）。
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (status: OrderStatus) => <OrderStatusTag status={status} />,
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      width: 180,
      // 首屏渲染时 orders 还是 null（表格无数据），这列不会参与服务端/客户端首帧比对，
      // 不必担心时区导致的 hydration 不一致。
      render: (createdAt: string) => new Date(createdAt).toLocaleString('zh-CN'),
    },
    {
      title: '操作',
      width: 200,
      // 操作列：允许哪些操作由 ACTIONS_BY_STATUS 决定，这里只查表 + map，不写 if / else。
      // 表本身必须与后端状态机一致（internal/order/service.go）——判错了不会报错，
      // 只会让用户点了才发现 409：
      //   paid     → 发货 / 退款
      //   shipped  → 完成 / 退款
      //   pending / completed / refunded → 无操作，渲染「—」占位
      //
      // 三个动不得的约束：
      //   ① 行内操作用独立的 actingId，不复用整页 loading——否则点一次发货，整张表都会转圈。
      //   ② 挡连点的主防线是 antd 的 loading（源码 handleClick 里
      //      `if (innerLoading || mergedDisabled) return`），disabled 只负责挡「一次处理两单」。
      //      别指望用 state 判断做防重入：await 之后读到的是本次渲染的快照，不是最新值。
      //   ③ handleAction 成功后按 id 局部替换那一行，别重拉整表，更别 `[...prev, updated]`
      //      —— 原行还在又追加一条同 id 记录，rowKey="id" 撞 key，React 会报 duplicate key。
      //
      // 409 分支（状态被别处改过）写在 handleAction 里：先提示、再 loadOrders() 拉回真实状态。
      render: (_: unknown, order: Order) => {
        const actions = ACTIONS_BY_STATUS[order.status];
        if (!actions.length) {
          return <Typography.Text type="secondary">—</Typography.Text>;
        }
        return (
          <Space size={8}>
            {actions.map((action) => (
              <Button
                key={action}
                type="link"
                size="small"
                style={{ padding: 0 }}
                loading={actingId === order.id}
                disabled={actingId !== null && actingId !== order.id}
                onClick={() => void handleAction(order.id, action)}
              >
                {ACTION_META[action].label}
              </Button>
            ))}
          </Space>
        );
      },
    },
  ];

  return (
    <Card title="订单管理">
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
        // loading 由「数据是否到位」直接算出来，不用额外的 state 去同步。
        loading={orders === null}
        // 后端 /api/admin/orders 目前是全量返回、没有分页参数，所以这里只是前端分页；
        // 数据量大了要改成后端分页（属于后续「业务纵深」的一部分）。
      />
    </Card>
  );
}
