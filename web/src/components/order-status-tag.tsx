/**
 * 订单状态标签。
 *
 * 用在哪：用户端「我的订单」与管理端「订单管理」的状态列。
 * 文案与配色统一由 lib/order-status.ts 的映射决定，页面里不要再各自手写 Tag，
 * 否则两页的显示又会出现分叉。
 *
 * 为什么显式加 'use client'：它渲染 antd 的 Tag，属于客户端模块。
 * 即便当前只在 'use client' 的页面里被引用，显式标出边界也能让将来的误用
 * （服务端组件直接 import）在构建期就暴露，而不是等到运行时才炸。
 */

'use client';

import { Tag } from 'antd';

import { ORDER_STATUS_META } from '@/lib/order-status';
import type { OrderStatusMeta } from '@/lib/order-status';
import type { OrderStatus } from '@/lib/types';

export interface OrderStatusTagProps {
  status: OrderStatus;
}

/** 按状态渲染对应的标签。 */
export function OrderStatusTag({ status }: OrderStatusTagProps) {
  // 显式放宽成可能为 undefined：后端带着一个新状态上线、而前端的 OrderStatus 还没同步时，
  // 查表会拿到 undefined。此时把状态码原样显示出来，总好过读 meta.color 抛 TypeError——
  // React 里一个渲染期异常就够卸载掉整棵树，整张表跟着白屏。
  const meta: OrderStatusMeta | undefined = ORDER_STATUS_META[status];
  if (meta === undefined) {
    return <Tag>{status}</Tag>;
  }
  return <Tag color={meta.color}>{meta.text}</Tag>;
}
