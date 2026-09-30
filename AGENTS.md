# 项目协作规范（AGENTS.md）

本文件用于指导 AI 编程助手（如 Codex、Claude、Cursor 等）在本项目中工作的行为准则。所有参与本项目的自动化工具在动手修改代码前，应优先阅读并遵循本文件中的约定。

## 项目概览

这是一个基于 Go 语言的新项目，当前目录结构如下：

- `cmd/server/`：服务入口目录，包含程序启动相关代码。
- `internal/order/`：订单相关的内部业务逻辑。
- `internal/user/`：用户相关的内部业务逻辑。
- `internal/auth/`：JWT 签发与认证 / 授权中间件。
- `docs/`：swagger 接口文档，由 `swag` 生成，**请勿手工编辑**。
- `web/`：前端项目（Next.js + React + Ant Design），详见「前端约定」。
- `go.mod` / `go.sum`：Go 模块依赖管理文件。

`internal` 目录下的包仅限本项目内部使用，不应被外部模块导入。

## 通用行为准则

- 在执行任何修改代码、创建文件或运行命令的操作之前，先阅读本文件及相关上下文，确保理解项目约定。
- 优先使用项目已有的结构、命名风格与依赖，不要随意引入新的框架或库。
- 改动应当最小且聚焦，避免对无关文件进行大规模重构。
- 所有新增或修改的代码都应可以通过项目既有的构建与测试流程。

## 协作模式：学员手写，AI 辅助（重要）

本项目是用户**练习手写能力**的载体。除非用户明确说「帮我实现」「直接改掉」「修一下」这类授权，一律按以下分工执行：

- **方案先行**：用户询问下一步方案时，只提供方案建议与说明，不直接修改原代码。
- **只搭骨架，不写业务**：新增模块时只创建目录结构、类型定义、接口签名、空函数体与必要的注释说明，**不填充业务实现逻辑**。具体业务代码由用户手写。
- **搭完必须给出待办清单**：明确列出「你需要实现什么」，标明每个待实现位置的目标与验收标准。
- **测试由 AI 编写**：单元测试与集成测试由 AI 负责编写和维护，用户不手写测试。
- **发现错误只指出，不代改**：审查代码或测试失败时，只说明「文件 / 行号 / 现象 / 原因 / 建议修法」，由用户自己动手修改。**不得直接编辑用户的业务代码去修复它**。

## 代码风格

- 遵循官方 Go 代码规范（`gofmt` 格式化）。
- 包名、变量名、函数名使用简洁的英文命名；注释与文档使用中文。
- 导出的标识符应提供清晰的注释说明其用途。

## 错误处理规范

统一采用「**哨兵错误 + 分层包装**」。区分标准的唯一依据是：**调用方是否需要据此改变行为**。

**两类错误**

- **业务错误**：可预期、调用方需要分支处理（订单不存在 → 404、状态冲突 → 409、用户名重复 → 409）。
  每个模块在自己的 `errors.go` 中定义包级哨兵（`ErrXxx`），并用注释说明含义。
- **技术错误**：数据库故障、网络异常等，调用方无法处理，只能记日志并返回 500。
  用 `fmt.Errorf("中文动作描述: %w", err)` 包装后向上抛。

**各层职责**

| 层 | 职责 |
|---|---|
| 仓库层 | 把 ORM / 驱动错误**翻译**为模块自己的哨兵（`gorm.ErrRecordNotFound` → `ErrOrderNotFound`），使 gorm 不出现在仓库层之外；技术错误统一包装 |
| 服务层 | 用 `errors.Is` 判断业务错误、决定是否转换语义（如「用户不存在」对外合并为「用户名或密码错误」以防账号枚举）；技术错误原样上抛，**不得伪装成业务错误** |
| 接口层 | 用 `errors.Is` 把错误映射为 HTTP 状态码；未识别的错误一律 500 加通用文案，细节只进日志；**不得把 `err.Error()` 直接回显给调用方** |

**硬性要求**

- 判断错误类型一律用 `errors.Is` / `errors.As`，**禁止比较错误文案字符串**。
- 包装必须用 `%w` 保留错误链；用 `errors.New` 重新造一个错误会切断链路，只允许用在「定义哨兵」的场合。
- `if err != nil` 之后必须 `return`（或显式处理），不允许只记录后继续——吞掉的错误会让故障伪装成正常空结果。
- 错误文案统一中文，冒号统一半角 `: `，句末不加标点。
- 哨兵错误集中放在各包的 `errors.go`，不要散落在业务文件里。

## 前端约定

- **位置**：`web/` 子目录，与本后端同仓。前端代码不跨入 Go 目录，后端代码不跨入 `web/`。
- **技术栈**：Next.js（App Router）+ React + TypeScript + Ant Design v6。
- **后端是唯一数据入口**：前端不直连数据库。浏览器统一请求同源的 `/api/*`，
  由 `next.config.ts` 的 `rewrites` 代理到 Go 后端（概念等价 Vite 的 `server.proxy`）。
  这样做的好处是后端**不需要**开 CORS，前端也不需要 `NEXT_PUBLIC_` 前缀，后端地址不进客户端产物。
  注意：服务端组件里不能用相对路径取数，Node 环境的 fetch 会报 `Failed to parse URL`，
  需要另拼绝对地址。
- **接口类型来源**：以后端 `docs/swagger.json` 为准，前端 TS 类型应与之一致（落在 `src/lib/types.ts`）。
- **取数与 effect（2026-09-29 决策）**：取数场景**允许**在 `useEffect` 里 `setState`。
  `react-hooks/set-state-in-effect` 已在 `web/eslint.config.mjs` 末尾**降为 warn**——它过宽，
  会把「与外部系统正常同步（拉列表）」连同真反例一起拦；作为补偿，同处显式开启了插件里
  默认未启用的 `react-hooks/no-deriving-state-in-effects`（**error**），由它拦住
  「在 effect 里从 state / props 派生值」这个真正的反例。
  ⇒ 看到该 warn 属预期，**不是漏改**；但把 `setState` 都放到 `await` 之后可以连 warn 都不产生。
  唯一保留的 `eslint-disable` 在 `src/lib/auth.tsx`（同步读 localStorage，没有 await 可挂）。

### Ant Design v6 注意事项

依据官方文档「在 Next.js 中使用」「样式兼容」「v5 升 v6」三篇，以下为确认过的约束：

- **antd 组件必须放在带 `'use client'` 的客户端组件中**，这是本项目所有 antd 页面的前提。
- **根布局必须用 `@ant-design/nextjs-registry` 的 `<AntdRegistry>` 包裹**，否则 SSR 首屏没有样式。
  注意 `ConfigProvider` 这类内部用了 hooks 的组件**不能直接写在服务端组件的 layout 里**，
  要收进一个 `'use client'` 的 Provider 文件（见 `src/components/providers.tsx`）。
- **点子组件（`<Form.Item />`、`<Select.Option />`、`<Typography.Title />`）只在客户端组件里可用。**
  antd 组件是客户端模块，服务端组件不能访问其上的属性，用了必然失败：
  官方文档描述的报错是 `Cannot access .Option on the server ... You cannot dot into a client module from a server component`；
  实测 Next.js 16 + antd 6.6.5 报的是 `Element type is invalid: ... but got: undefined`。
  **解法是给该组件加 `'use client'`**——官方示例（`with-sub-components`）采用的就是这个方案。
  官方中文文档写的「需从具体路径导入」并非必须：只要组件落在客户端边界内，
  点子组件写法完全正常，本项目当前就是这么用的。
- **主题与样式覆盖走 CSS 变量**。v6 的 `@ant-design/cssinjs` 默认纯 CSS Variables 模式。
  改组件内部样式请用 `classNames` / `styles` 语义化 API，或 `theme.useToken()` 取 token，
  **不要写 `.ant-btn > span` 这类依赖内部 DOM 结构的选择器**——v6 重构过 DOM，这类选择器会失效。
- **`@ant-design/icons` 必须与 antd 主版本配套**：antd v6 要求 `@ant-design/icons >= 6`，
  且 icons v6 与 antd v5 不兼容，升级时必须两个一起升。
- **v6 起不再需要 `@ant-design/v5-patch-for-react-19`**（历史代码里有就直接删）；
  antd v6 要求 React >= 18，不再支持 React 17 及以下。
- **v6 有 150+ 个 props 已废弃**并迁移到 `classNames` / `styles`，
  例如 Alert 的 `message` → `title`、Card 的 `bodyStyle` → `styles.body`、
  Button 的 `iconPosition` → `iconPlacement`、`dropdownClassName` → `classNames.popup.root`。
  写组件前先查 v6 迁移文档；控制台出现 `deprecated` 警告要当回事，v7 会移除。

### 样式方案

**不引入 Tailwind，也不引入 Sass**：

- antd v6 已默认纯 CSS 变量模式，主题由 `ConfigProvider` 的 token 统一驱动；
  再引 Tailwind 等于同时维护两套设计 token，迟早对不上。
- antd v6 + Tailwind v4 存在已知样式冲突（ant-design#56014，官方 closed as not planned）。
  共存需要 `<StyleProvider layer>` 包裹 `<ConfigProvider>` + `@layer` 排布 +
  reset/antd.css 显式挂 `layer()`，且 SSR 下还有「声明 layer 顺序的规则必须先于 antd 注入的
  `<style>`」这一额外约束，是为一点写 class 的便利换来一整条调试链路。
- 自定义样式按此层次走：`globals.css` 全局 → `theme.useToken()` 取 token →
  antd 的 `classNames` / `styles` → 最后才 CSS Modules（Next.js 原生支持，需要时再开）。

## 项目审查

在对本项目进行代码审查时，除遵循上述「代码风格」外，还需满足以下要求：

- **GORM 一律采用泛型 API**：涉及 GORM 的数据库操作代码，统一使用泛型 API（如 `db.Find(&results)`、`db.Where(...).Find(&results)` 等基于泛型的方法），禁止使用传统的非泛型写法（如依赖 `interface{}`、反射式查询或已被弃用的旧式调用）。新增与既有代码均须保持一致。
- **保证代码风格统一**：审查时确认命名、包结构、注释风格与本项目约定一致，避免出现风格混用。
- **格式正常**：确认代码已通过 `gofmt` 格式化，无缩进错乱、多余空行等问题。
- **无冗余代码**：移除未使用的变量、函数、导入与死代码，避免重复逻辑。

## 构建与测试

后端（在仓库根目录执行）：

- `gofmt -l ./cmd ./internal` 检查格式。
  **不要用 `gofmt -l .`** —— 它会递归进 `web/node_modules`，极其缓慢。
- `go build ./...` 构建检查，`go vet ./...` 静态检查。
- `go test ./...` 运行全部测试。
  仓库层集成测试连真实 PostgreSQL（从 `.env` 读 `DATABASE_URL`），每个用例在独立事务中运行、
  结束时回滚，用例之间互不污染；未配置 `DATABASE_URL` 时自动跳过。
- `swag init -g cmd/server/main.go -o docs --parseDependency --exclude web,server-node` 重新生成
  swagger 文档。`--exclude` 不能省：`web/node_modules` 下混着 Go 源码，不排除会被扫进文档；
  `--parseDependency` 也建议保留，否则生成的 definitions 前缀会变，docs 会凭空多出一大段 diff。

前端（在 `web/` 目录执行）：

- `npm run dev` 启动开发服务器（默认 3000 端口）。
  注意 Next.js 只允许**一个** dev server 实例（共享 `.next` 目录锁）。
  端口被占时不要「再起一个」，先确认已有实例或先把它停掉。
  另外，**项目结构大改之后（新增路由组、调整 layout 层级、改根 Provider）要重启 dev server**：
  HMR 处理不了这个量级的变更，会表现为「服务端渲染的 HTML 与客户端对不上」的
  hydration mismatch 报错，看起来像代码 bug，实际是 dev server 的中间态。
  判断办法：直接 `curl` 出服务端 HTML 看结构是否正确，结构对就重启 dev server。
- `npm run build` 构建校验，`npm run typecheck` 类型检查。
  改动前端后至少跑 `npm run typecheck`；涉及路由 / 布局 / antd 客户端边界的改动必须跑 `npm run build`，
  因为类型检查发现不了 App Router 与 RSC 层面的问题。

在提交或声称任务完成之前，应至少通过对应侧的上述检查。

## 安全与合规

- 不处理任何涉及政治敏感、违法违规或侵犯隐私的内容。
- 不生成任何破坏性命令（如强制推送、硬重置等），除非用户明确要求。
