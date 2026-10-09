import { defineConfig } from 'vitest/config';
import { fileURLToPath } from 'node:url';

// Vitest 配置。
//
// 范围刻意收窄在 src/lib/ 下：这三个文件（org.ts / order-status.ts / format.ts）是纯函数，
// 不依赖 React 与 antd，不需要 jsdom 环境，跑得最快、反馈最直接。
// 组件测试要另配 jsdom + @testing-library，那是另一笔账，等真需要时再开。
export default defineConfig({
  test: {
    environment: 'node',
    include: ['src/lib/**/*.test.ts'],
    // 纯函数用例应当是确定性的；跑慢通常意味着有东西没封干净。
    testTimeout: 5000,
  },
  resolve: {
    alias: {
      // 让测试里能用 '@/lib/xxx' 这种与业务代码一致的写法。
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
});
