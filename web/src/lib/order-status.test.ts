import { describe, expect, it } from 'vitest';

import { ORDER_STATUS_META } from './order-status';
import type { OrderStatus } from './types';

/**
 * 订单状态映射的用例。
 *
 * 这类「配置表」测试的真正价值不在于逐条断言文案，而在于三件事：
 *   1. **不漏状态**：OrderStatus 的每个取值都必须有配置（Record 已在编译期保证一半，
 *      这里再保证运行时拿到的东西是完整的）；
 *   2. **文案不空**：任何状态都不会渲染出空白标签；
 *   3. **颜色是 antd 认识的值**：写错会静默回落成默认色，控制台不报错。
 */
describe('ORDER_STATUS_META', () => {
  const allStatuses: OrderStatus[] = ['pending', 'paid', 'shipped', 'completed', 'refunded'];

  it('覆盖 OrderStatus 的全部取值', () => {
    expect(Object.keys(ORDER_STATUS_META).sort()).toEqual([...allStatuses].sort());
  });

  it('每个状态都有非空文案', () => {
    for (const status of allStatuses) {
      expect(ORDER_STATUS_META[status].text, `${status} 缺少文案`).not.toBe('');
      expect(ORDER_STATUS_META[status].text.trim()).not.toBe('');
    }
  });

  /**
   * 文案不得重复：两个状态显示同一句话，用户无法区分。
   * 后端加新状态时若忘了改文案，这条会立刻失败。
   */
  it('不同状态不共用同一句文案', () => {
    const texts = allStatuses.map((s) => ORDER_STATUS_META[s].text);
    expect(new Set(texts).size).toBe(texts.length);
  });

  /**
   * 配色校验。antd v6 实测可用的取值分两类：
   *   - 状态色5个：success / processing / error / default / warning
   *   - 命名预设色：blue / purple / cyan / green / magenta / pink / red / orange /
   *                yellow / volcano / geekblue / lime / gold
   * 写错不会报错，只会静默回落成默认色，所以只能靠测试守住。
   */
  it('颜色都是 antd 认识的取值', () => {
    const stateColors = ['success', 'processing', 'error', 'default', 'warning'];
    const presetColors = [
      'blue',
      'purple',
      'cyan',
      'green',
      'magenta',
      'pink',
      'red',
      'orange',
      'yellow',
      'volcano',
      'geekblue',
      'lime',
      'gold',
    ];
    const allowed = new Set([...stateColors, ...presetColors]);

    for (const status of allStatuses) {
      expect(allowed, `${status} 的颜色不合法`).toContain(ORDER_STATUS_META[status].color);
    }
  });

  /**
   * 语义锁定：待支付是唯一「等用户动手」的状态，必须显眼；
   * 退款是异常终态，必须用错误色。
   * 这两个断言是为了防止有人调整配色时把语义一起弄丢
   * （比如把退款改成灰色 default，看起来更「中性」，但用户就看不出出问题了）。
   */
  it('待支付用醒目色、退款用错误色', () => {
    expect(ORDER_STATUS_META.pending.color).toBe('warning');
    expect(ORDER_STATUS_META.refunded.color).toBe('error');
  });

  it('正常终态（已完成）用成功色', () => {
    expect(ORDER_STATUS_META.completed.color).toBe('success');
  });
});
