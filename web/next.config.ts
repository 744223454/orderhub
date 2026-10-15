import type { NextConfig } from 'next';

/**
 * Go 后端地址。开发期在 .env.local 里配置。
 *
 * Next.js 在加载本配置文件之前就会先读入 .env.local，
 * 所以这里能直接取到；找不到时退回本地默认值，避免配置漏了就跑不起来。
 */
const apiBaseUrl = process.env.API_BASE_URL ?? 'http://localhost:8888';

/**
 * mcp-ts（企业微信身份型 MCP Server）地址。
 *
 * 为什么需要反代：mcp-ts 与本项目**共用同一个企业微信自建应用**
 * （CorpID ww7ff8a69687e8aa4a / AgentId 1000002），而企微后台的
 * 「授权回调域」按应用配置、且**一个应用只能填一个**。实测同一域名仅端口不同即被拒：
 *
 *   dev.orderhub.local:3000/...  → 通过
 *   dev.orderhub.local:3100/...  → errCode -31020
 *
 * 所以不能让 mcp-ts 单独占一个端口去登记回调域（那样本项目登录会失效），
 * 只能让它**共用 3000**，靠路径区分。企微只校验「域名+端口」、路径可自定，
 * 因此两个项目的回调地址可以并存于同一个授权回调域下。
 */
const mcpBaseUrl = process.env.MCP_BASE_URL ?? 'http://127.0.0.1:3100';

const nextConfig: NextConfig = {
  reactStrictMode: true,

  /**
   * 允许通过自定义本地域名访问 dev server。
   *
   * 为什么需要：企业微信的「授权回调域」不接受 localhost（它要求域名带点，
   * 实测 `localhost:3000` 会报"请填写正确的域名地址"），所以本地开发把回调域配成了
   * `dev.orderhub.local:3000`，并在 /etc/hosts 里把它指向 127.0.0.1。
   *
   * 而 Next 15.3 起 dev server 会校验请求来源：非白名单 origin 的 **HMR WebSocket 会被拒**
   * （实测现象：页面、样式、脚本都能正常加载，但
   * `ws://dev.orderhub.local:3000/_next/hmr` 握手失败，报 ERR_INVALID_HTTP_RESPONSE，
   * 热更新失效、每次改代码都要手动刷新）。
   *
   * 只影响 dev：生产模式（next start）不做这项来源校验。
   */
  allowedDevOrigins: ['dev.orderhub.local'],

  /**
   * 把浏览器发往 /api/* 的请求代理到 Go 后端，把 mcp-ts 的路径代理到 MCP 服务。
   *
   * 前者这么做：
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

      // ---- mcp-ts：MCP 端点与 OAuth 授权服务器 ----
      // MCP Streamable HTTP 端点（POST/GET/DELETE 都要转发，故用 :path*）
      { source: '/mcp', destination: `${mcpBaseUrl}/mcp` },
      { source: '/mcp/:path*', destination: `${mcpBaseUrl}/mcp/:path*` },
      // OAuth 发现文档：RFC 9728 受保护资源元数据 / RFC 8414 授权服务器元数据
      { source: '/.well-known/:path*', destination: `${mcpBaseUrl}/.well-known/:path*` },
      // 授权服务器：动态注册 / 授权入口 / 企业微信回调 / token 端点
      { source: '/register', destination: `${mcpBaseUrl}/register` },
      { source: '/authorize', destination: `${mcpBaseUrl}/authorize` },
      { source: '/token', destination: `${mcpBaseUrl}/token` },
      { source: '/wecom/callback', destination: `${mcpBaseUrl}/wecom/callback` },
    ];
  },
};

export default nextConfig;
