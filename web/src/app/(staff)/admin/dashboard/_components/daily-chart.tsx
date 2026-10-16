'use client';

import type { LineSeriesOption } from 'echarts/charts';
import type { TooltipComponentOption } from 'echarts/components';
import type { ComposeOption } from 'echarts/core';

import { formatAmount } from '@/lib/format';
import type { DailyStat } from '@/lib/types';

import { EChart } from './echart';

/** 本图真实用到的 option 类型：按需组合（折线系列 + tooltip 组件）。 */
type DailyLineOption = ComposeOption<LineSeriesOption | TooltipComponentOption>;

/** 近 14 天趋势折线图的属性。 */
export interface DailyChartProps {
  /** 统计接口的 daily（14 天定长、按日期升序、末条为今天）。 */
  data: DailyStat[];
}

/**
 * 近 14 天订单趋势折线图。
 *
 * 映射与实现说明：
 * - x 轴直接用 date 字符串（YYYY-MM-DD、升序；不解析成 Date，顺带绕开时区问题），
 *   14 个刻度放不下，axisLabel 旋转 45° 斜排；
 * - 线只画订单量——金额与订单量量纲差 4~5 个数量级，双轴刻度易误读、性价比低；
 *   金额信息放进 tooltip，通过 dataIndex 回查闭包里的 data 一并展示；
 * - 缺数据的天后端已补 0，前端不做兜底。
 */
export function DailyChart({ data }: DailyChartProps) {
  const option: DailyLineOption = {
    xAxis: {
      type: 'category',
      data: data.map((item) => item.date),
      axisLabel: { rotate: 45 },
    },
    // 折线图需要直角坐标系：配了 xAxis 就必须显式配 yAxis
    // （只配其一不会自动补全，setOption 会抛 Error: yAxis "0" not found）。
    yAxis: { type: 'value' },
    series: [
      {
        name: '订单量',
        type: 'line',
        data: data.map((item) => item.count),
      },
    ],
    tooltip: {
      trigger: 'axis',
      formatter: (params) => {
        // params 是「单项 | 数组」的联合（axis 触发实际恒为数组，先收窄）。
        const list = Array.isArray(params) ? params : [params];
        const day = data[list[0].dataIndex];
        return `${day.date}<br/>订单量: ${day.count} 单<br/>金额: ${formatAmount(day.amount)}`;
      },
    },
  };

  return <EChart option={option} />;
}
