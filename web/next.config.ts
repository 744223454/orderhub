import type { NextConfig } from 'next';

/**
 * Go 后端地址。开发期在 .env.local 里配置。
 *
 * Next.js 在加载本配置文件之前就会先读入 .env.local，
 * 所以这里能直接取到；找不到时退回本地默认值，避免配置漏了就跑不起来。
 */
const apiBaseUrl = process.env.API_BASE_URL ?? 'http://localhost:8888';

const nextConfig: NextConfig = {
  reactStrictMode: true,

  /**
   * 把浏览器发往 /api/* 的请求代理到 Go 后端。
   *
   * 为什么这么做：
   *   1. 浏览器看到的是同源请求，Go 那边不需要加 CORS 中间件；
   *   2. 前端代码里只写相对路径 '/api/login'，不关心后端部署在哪，
   *      换成测试环境只改 .env.local 一个文件；
   *   3. 不需要 NEXT_PUBLIC_ 前缀，后端地址不会被打进客户端产物。
   *
   * 概念上等价于 Vite 的 server.proxy。
   *
   * 注意：仅对开发服务器 / next start 生效。真正部署时通常是
   * Nginx 之类按路径把 /api 转给后端，那时这段 rewrites 可以留着不管。
   */
  async rewrites() {
    return [
      {
        source: '/api/:path*',
        destination: `${apiBaseUrl}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
