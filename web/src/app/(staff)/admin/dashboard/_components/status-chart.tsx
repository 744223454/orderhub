'use client';

import type { PieSeriesOption } from 'echarts/charts';
import type { TooltipComponentOption } from 'echarts/components';
import type { ComposeOption } from 'echarts/core';

import { formatAmount } from '@/lib/format';
import { ORDER_STATUS_META } from '@/lib/order-status';
import type { StatusStat } from '@/lib/types';

import { EChart } from './echart';

/**
 * 本图真实用到的 option 类型：按需组合（饼图系列 + tooltip 组件）。
 *
 * EChartsCoreOption 只是宽泛的基础容器（字段被索引签名兜成 unknown），
 * 不给 formatter 的参数提供任何上下文类型；用 ComposeOption 组合出精确类型后，
 * 整份 option 的字段检查与回调参数推断才会生效。
 */
type StatusPieOption = ComposeOption<PieSeriesOption | TooltipComponentOption>;

/** 订单状态分布环形图的属性。 */
export interface StatusChartProps {
  /** 统计接口的 by_status（五状态定长、顺序固定）。 */
  data: StatusStat[];
}

/**
 * 订单状态分布环形图。
 *
 * 映射与实现说明：
 * - 扇区名称走 ORDER_STATUS_META 的中文文案（与订单列表的状态 Tag 同源）；
 * - 数值取 count；颜色按 by_status 的固定顺序（pending→paid→shipped→completed→
 *   refunded）与状态语义色一一对位，色值取 antd 色板的同款值（warning / 主蓝 /
 *   青 / success / error），⚠️ 不能写 CSS 变量——canvas 渲染不解析 var()；
 * - tooltip 通过 dataIndex 回查组件闭包里的 data：类型完整（StatusStat），
 *   无需对宽 union 的 params.data 做断言。
 */
export function StatusChart({ data }: StatusChartProps) {
  const option: StatusPieOption = {
    series: [
      {
        type: 'pie',
        radius: ['45%', '70%'],
        // 顺序即 by_status 的固定顺序（pending→paid→shipped→completed→refunded）。
        color: ['#faad14', '#1677ff', '#13c2c2', '#52c41a', '#ff4d4f'],
        data: data.map((item) => ({
          name: ORDER_STATUS_META[item.status].text,
          value: item.count,
        })),
      },
    ],
    tooltip: {
      trigger: 'item',
      formatter: (params) => {
        // params 是「单项 | 数组」的联合（顶层 formatter 两种都可能收到）——
        // 本图 trigger 是 item，实际永远是单项，但类型上要先收窄。
        const p = Array.isArray(params) ? params[0] : params;
        const stat = data[p.dataIndex];
        return `数量为: ${stat.count} 个, 金额为 ${formatAmount(stat.amount)}`;
      },
    },
  };

  return <EChart option={option} />;
}
