/**
 * ESLint 扁平配置（flat config）。
 *
 * Next.js 16 移除了 `next lint` 命令，官方改为直接使用 ESLint CLI，本文件取代了原来的包装器。
 * 三块内容分工不同，缺一块覆盖就少一截：
 * - core-web-vitals：Next + React + React Hooks 规则，并把影响 Core Web Vitals 的规则由警告升为错误
 * - typescript：追加 TypeScript 专属规则（基于 typescript-eslint 的 recommended）
 * - prettier：关掉 ESLint 内与 Prettier 冲突的格式类规则。分工是「ESLint 管代码质量，Prettier 管排版」
 */
import { defineConfig, globalIgnores } from 'eslint/config';
import nextVitals from 'eslint-config-next/core-web-vitals';
import nextTs from 'eslint-config-next/typescript';
import prettier from 'eslint-config-prettier/flat';

export default defineConfig([
  ...nextVitals,
  ...nextTs,
  prettier,
  // 覆盖 eslint-config-next 的默认忽略项
  globalIgnores(['.next/**', 'out/**', 'build/**', 'next-env.d.ts']),
  /**
   * 覆盖 react-hooks 的默认严重级别。
   * （flat config 的规则是「后出现的覆盖先出现的」，所以这一块必须放在最后。）
   *
   * 背景：react-hooks 插件里带一宽一准两条规则——
   * `set-state-in-effect` 过宽，会把「effect 里正常与外部系统同步（例如拉订单列表）」
   * 连同「用 effect 派生状态」这个真反例一起拦；而真正精准的
   * `no-deriving-state-in-effects`（只拦「从 state / props 派生值」）默认却没开。
   * 所以这里降宽的、升准的：取数写在 effect 里不再报错，但真反例仍然被 error 拦住。
   *
   * 注意：`lib/auth.tsx` 里那处 eslint-disable 仍然保留——它的 setState 是**同步**读
   * localStorage、没有 await 可挂，属于本条规则「真的没写错但确实同步」的场景。
   */
  {
    rules: {
      'react-hooks/set-state-in-effect': 'warn',
      'react-hooks/no-deriving-state-in-effects': 'error',
    },
  },
]);
