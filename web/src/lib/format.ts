/**
 * 展示层的纯格式化函数。
 *
 * 放在 lib 下且不带 'use client'：纯函数在服务端与客户端都能用，
 * 将来若有服务端组件要渲染金额，直接 import 即可，
 * 不会像带 'use client' 的模块那样在服务端报错。
 */

/**
 * 把「分」格式化成「元」。
 *
 * 金额在后端一律是整数分（见 internal/order/model.go 的 Amount 注释），只在展示这一刻换算。
 * 用「商 + 余数」拼字符串而不是 `amount / 100` 再 toFixed：前者全程整数运算，
 * 不会出现浮点尾数，也不依赖任何舍入规则。
 */
export function formatAmount(amount: number): string {
  const sign = amount < 0 ? '-' : '';
  const n = Math.trunc(Math.abs(amount));
  const quo = Math.floor(n / 100);
  const decimal = String(n % 100).padStart(2, '0');
  return `${sign}${quo}.${decimal} 元`;
}
