/**
 * 订单状态的展示映射（中文文案 + Tag 颜色）。
 *
 * 抽成公共模块的原因：用户端「我的订单」与管理端「订单管理」要显示同一套状态标签，
 * 各留一份的结果是改一处忘一处，两个页面显示不一致。
 *
 * 用 Record<OrderStatus, ...> 而不是普通对象：将来后端加了新状态、types.ts 的
 * OrderStatus 跟着扩了联合类型，这里漏填会**直接编译报错**，不会静默渲染成空白。
 *
 * 配色按「用户看到这一行时该有什么反应」来选，不是按状态先后随便排：
 * 黄 = 轮到你操作，蓝/青 = 后端在推进，绿 = 顺利走完，红 = 中止。
 * 可用取值（antd v6 实测）——状态色 5 个：success / processing / error / default / warning；
 * 命名预设色 13 个：blue / purple / cyan / green / magenta / pink / red / orange /
 * yellow / volcano / geekblue / lime / gold。
 */

import type { OrderStatus } from './types';

/** 单个订单状态的展示信息。 */
export interface OrderStatusMeta {
  /** 展示用的中文文案。 */
  text: string;
  /** 传给 antd Tag 的 color，可用取值见文件头注释。 */
  color: string;
}

/** 订单状态 → 展示信息。新增状态时这里漏填会编译报错。 */
export const ORDER_STATUS_META: Record<OrderStatus, OrderStatusMeta> = {
  // 黄：唯一「等用户动手」的状态，要在一屏里最先被认出来。
  pending: { text: '待支付', color: 'warning' },
  // 蓝：钱已收到、流程开始走。
  paid: { text: '已支付', color: 'processing' },
  // 青：和「已支付」同属进行中，但更靠后一步，用色阶而不是换语义色来区分。
  shipped: { text: '已发货', color: 'cyan' },
  // 绿：正常终态。
  completed: { text: '已完成', color: 'success' },
  // 红：非正常终止。想改成中性观感的话换成 'default'（灰）即可，只动这一个词。
  refunded: { text: '已退款', color: 'error' },
};
