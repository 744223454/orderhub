'use client';

import { useEffect, useRef } from 'react';
import * as echarts from 'echarts/core';
import { EChartsCoreOption, EChartsType } from 'echarts/core';
import { LineChart, PieChart } from 'echarts/charts';
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';

echarts.use([
  LineChart,
  PieChart,
  GridComponent,
  TooltipComponent,
  LegendComponent,
  CanvasRenderer,
]);

/** EChart 组件的属性。 */
export interface EChartProps {
  /** ECharts 配置（echarts/core 的按需引入版配置）。 */
  option: EChartsCoreOption;
  /** 容器高度，单位 px。 */
  height?: number;
}

/**
 * ECharts 基础封装（echarts/core 按需引入）。
 *
 * ── 第一步：按需注册（放本模块顶层，全项目注册一次即可）──────────────
 * 从对应子路径精确引入再 `echarts.use([...])` 注册：
 *   - 图表：`echarts/charts` 的 LineChart、PieChart
 *   - 组件：`echarts/components` 的 GridComponent、TooltipComponent、LegendComponent
 *   - 渲染器：`echarts/renderers` 的 CanvasRenderer
 * 漏注册的报错长得像「Series line is not exists」——看到它先回来检查这里。
 *
 * ── 生命周期契约（TODO 手写，验收标准逐条如下）────────────────────
 * 1. 挂载：在容器 ref 上 `echarts.init` 建实例——effect 只在浏览器执行，SSR 安全
 * 2. 更新：option 变化时对**现有实例** `setOption`（不要销毁重建——重建会闪、
 *    且容易漏 dispose；入场动画与增量合并都依赖 setOption 本身）
 * 3. 自适应：ResizeObserver 监听容器尺寸 → `chart.resize()`。
 *    不能只听 window.resize：侧栏折叠这类「窗口没变、容器变了」不触发 window 事件
 * 4. 清理：卸载时断开 observer + `chart.dispose()`。⚠️ StrictMode 下 effect 会
 *    「执行 → 清理 → 再执行」，漏了 dispose 会残留实例（画布叠两层/内存泄漏警告）
 *
 * 验收（最终由 CDP 冒烟执行）：
 * - /admin/dashboard 两张图渲染出图形；控制台无 error / warn（「not exists」类必须为零）
 * - 拖动窗口宽度：图形跟随重排（网格与坐标轴自适应）
 * - 路由来回切换：无「实例已存在」告警、无残留画布
 */
export function EChart({ option, height = 320 }: EChartProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<EChartsType | null>(null);

  // TODO(手写)：把上面四条契约落地。切法自行决定——init/observe/dispose 一个 effect、
  // setOption 更新另一个 effect（依赖 option）是常见拆法；想清楚各自的执行与清理时序。
  useEffect(() => {
    const container = containerRef.current;
    if (container === null) {
      return;
    }
    chartRef.current = echarts.init(container);
    const resizeObserver = new ResizeObserver(() => {
      chartRef.current?.resize();
    });
    resizeObserver.observe(container);
    return () => {
      resizeObserver.disconnect();
      chartRef.current?.dispose();
      chartRef.current = null;
    };
  }, []);

  useEffect(() => {
    chartRef.current?.setOption(option);
  }, [option]);

  return <div ref={containerRef} style={{ width: '100%', height }} />;
}
