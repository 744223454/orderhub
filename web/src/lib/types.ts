/**
 * 后端接口的 TypeScript 类型定义。
 *
 * 字段与 Go 侧的 model JSON tag 一一对应（internal/user/model.go、internal/order/model.go），
 * 与 docs/swagger.json 也应保持一致。后端字段若变更，这里必须同步——
 * 否则前端只会在运行期拿到 undefined，编译期不会有任何提示。
 */

/** 用户角色，取值与后端 user.Role 一致。 */
export type Role = 'admin' | 'user' | 'ops';

/** 用户信息，对应后端 user.User（不含密码哈希）。 */
export interface User {
  id: number;
  name: string;
  role: Role;
  created_at: string;
  updated_at: string;
}

/** 订单状态，取值与后端 order.Status 一致。 */
export type OrderStatus = 'pending' | 'paid' | 'shipped' | 'completed' | 'refunded';

/** 订单，对应后端 order.Order。 */
export interface Order {
  id: number;
  user_id: number;
  product_name: string;
  /** 金额，单位为分。展示时需自行除以 100，不要用浮点运算累加。 */
  amount: number;
  status: OrderStatus;
  created_at: string;
  updated_at: string;
}

/**
 * 订单列表的分页响应，对应 GET /api/orders 与 GET /api/admin/orders。
 *
 * 字段与 AdminUserPage 刻意保持一致 —— 两个列表接口形状统一，
 * 前端才能复用同一套分页状态与组件。
 */
export interface OrderPage {
  items: Order[];
  /** 满足条件的订单总数，用于算总页数。 */
  total: number;
  /** 生效后的页码（入参越界时后端会回落，所以以后端回显的为准）。 */
  page: number;
  /** 生效后的每页条数。 */
  page_size: number;
}

/** 单个状态的聚合统计，对应统计接口 by_status 的元素。 */
export interface StatusStat {
  status: OrderStatus;
  /** 该状态的订单数。 */
  count: number;
  /** 该状态的金额合计（分）。 */
  amount: number;
}

/** 单日聚合统计，对应统计接口 daily 的元素。 */
export interface DailyStat {
  /** 日期，北京时间口径，格式 YYYY-MM-DD。 */
  date: string;
  /** 该天的订单数。 */
  count: number;
  /** 该天的金额合计（分）。 */
  amount: number;
}

/**
 * 订单统计响应，对应 GET /api/admin/orders/stats（仅运营 / 管理员）。
 *
 * by_status 与 daily 都是**定长**序列（5 条 / 14 条），后端已补齐缺项并固定顺序：
 * by_status 的顺序为 pending → paid → shipped → completed → refunded；
 * daily 为近 14 个自然日（北京时间）按日期升序、末条是今天，无数据的天补 0。
 * 图表可以按序直接渲染，不需要前端再做缺项兜底。
 */
export interface OrderStats {
  /** 全量订单数。 */
  total_orders: number;
  /** 全量金额合计（分）。 */
  total_amount: number;
  /** 按状态分组的统计：五种状态定长、顺序固定。 */
  by_status: StatusStat[];
  /** 近 14 天逐日统计：连续定长、按日期升序。 */
  daily: DailyStat[];
}

/** 登录请求体，对应 POST /api/login。 */
export interface LoginRequest {
  username: string;
  password: string;
}

/** 登录成功响应，对应 POST /api/login 与 POST /api/auth/wecom/login 的返回。 */
export interface LoginResponse {
  token: string;
  /**
   * 刷新令牌（长寿命的不透明串）。访问令牌过期后凭它静默换取新令牌对；
   * 与密码同级敏感，只在存储层与刷新流程里使用，不进 React 状态。
   */
  refresh_token: string;
  user: User;
}

/**
 * 刷新成功响应，对应 POST /api/refresh。
 *
 * 令牌对全套换新：旧 refresh 已在服务端作废，必须整体替换保存。
 */
export interface RefreshResponse {
  token: string;
  refresh_token: string;
}

/** 当前登录用户，对应 GET /api/me 的返回。 */
export interface MeResponse {
  user_id: number;
  role: Role;
  /** 当前账号是否已设置过密码，决定设置页显示「设置密码」还是「修改密码」形态。 */
  has_password: boolean;
}

/** 修改密码请求体，对应 PATCH /api/me/password。 */
export interface ChangePasswordRequest {
  /** 原密码。仅当账号已设置过密码时必填；无密码账号可不提供（省略与传空串等价）。 */
  old_password?: string;
  new_password: string;
}

/** 下单请求体，对应 POST /api/orders。 */
export interface CreateOrderRequest {
  product_name: string;
  amount: number;
}

/** 后端统一的错误响应体。所有非 2xx 响应都是这个形状。 */
export interface ApiErrorBody {
  error: string;
}

/**
 * 企业微信授权的用途。
 *
 * login：扫码后登录，该企业微信身份没绑过账号就自动建一个普通账号；
 * bind：扫码后把该身份绑定到当前登录的账号（管理员、运营这样把自己的账号挂上去）。
 */
export type WecomIntent = 'login' | 'bind';

/** 获取企业微信扫码登录链接的返回，对应 GET /api/auth/wecom/authorize。 */
export interface WecomAuthorizeResponse {
  /** 企业微信扫码登录页地址。必须整页跳转到它，不能 fetch。 */
  url: string;
}

/** 企业微信回调参数，对应 POST /api/auth/wecom/login 与 POST /api/auth/wecom/bind。 */
export interface WecomCallbackRequest {
  /** 企微回调带回的授权码，只能用一次、5 分钟过期。 */
  code: string;
  /** 发起授权时下发的状态串，后端用它比对 Cookie 以防范 CSRF。 */
  state: string;
}

/**
 * 外部身份提供方，取值与后端 user.IdentityProvider 一致。
 *
 * 目前只有企业微信；将来接入微信、飞书时在这里追加即可，
 * 页面上的展示名统一走 lib/identity.ts，不必逐页去改。
 */
export type IdentityProvider = 'wecom';

/** 一条外部身份绑定关系，对应后端 user.UserIdentity。 */
export interface UserIdentity {
  id: number;
  provider: IdentityProvider;
  external_id: string;
}

/** 管理端看到的用户信息，对应 GET /api/admin/users 的 items。 */
export interface AdminUser {
  id: number;
  name: string;
  role: Role;
  /** 账号是否设置了可用密码。企业微信扫码自动建出的账号没有密码。 */
  has_password: boolean;
  /** 账号绑定的外部身份。 */
  identities: UserIdentity[];
  created_at: string;
  updated_at: string;
}

/** 用户列表的分页响应，对应 GET /api/admin/users。 */
export interface AdminUserPage {
  items: AdminUser[];
  /** 满足条件的用户总数，用于算总页数。 */
  total: number;
  /** 生效后的页码（入参越界时后端会回落，所以以后端回显的为准）。 */
  page: number;
  /** 生效后的每页条数。 */
  page_size: number;
}

/** 修改用户角色的请求体，对应 PATCH /api/admin/users/:id/role。 */
export interface UpdateUserRoleRequest {
  role: Role;
}

/** 管理员建号的请求体，对应 POST /api/admin/users（仅管理员）。 */
export interface CreateUserRequest {
  username: string;
  password: string;
  role: Role;
}

/**
 * 企业微信部门，对应后端 wecom.Department（GET /api/admin/org/directory）。
 *
 * ⚠️ 两点不能假设：
 *   1. `parent_id` 可能指向一个**不在 departments 里**的部门 ——
 *      自建应用只能读「可见范围」内的通讯录，父部门不在范围内时就成了孤儿。
 *   2. 这棵树不一定连通，也不保证没有环，组装时必须兜住（见 lib/org.ts）。
 */
export interface OrgDepartment {
  id: number;
  name: string;
  /** 上级部门 id，根部门为 0。 */
  parent_id: number;
  /** 在上级部门内的排序号。后端已按 (order, id) 排好序，前端照用即可。 */
  order: number;
  /** 部门负责人的 userid 列表。 */
  leader_user_ids: string[];
}

/** 成员在某个部门里的身份，对应后端 wecom.MemberDepartment。 */
export interface OrgMemberDepartment {
  id: number;
  /** 该成员是否是这个部门的负责人。 */
  is_leader: boolean;
}

/**
 * 成员状态的已知取值，与后端 wecom.MemberStatusXxx 一致。
 *
 * 只用来给映射表定键；字段类型仍是 number —— 收紧成联合类型的话，
 * 企微将来新增状态值时前端在编译期就写不出「未知状态」这条分支了。
 */
export type OrgMemberStatus = 1 | 2 | 4 | 5;

/** 企业微信成员，对应后端 wecom.Member。 */
export interface OrgMember {
  /** 成员帐号，企业内唯一，用作表格 rowKey。 */
  userid: string;
  name: string;
  /** 职务，可能为空串。 */
  position: string;
  /** 1=已激活 2=已禁用 4=未激活 5=已退出；未知取值由前端回落显示。 */
  status: number;
  main_department_id: number;
  /** 所属部门（含是否负责人），至少一个。 */
  departments: OrgMemberDepartment[];
  /** 直属上级的 userid。上级本人可能不在可见范围内，查不到名字时回落显示 userid。 */
  direct_leader_ids: string[];
}

/**
 * 通讯录快照，对应 GET /api/admin/org/directory。
 *
 * 部门与成员都是**扁平**列表：后端只做搬运与翻译，树由前端组装（见 lib/org.ts）。
 */
export interface OrgDirectory {
  departments: OrgDepartment[];
  members: OrgMember[];
  /** 本次快照的抓取完成时间（ISO 字符串）。 */
  fetched_at: string;
  /** 本次响应是否命中服务端缓存（缓存 5 分钟）。 */
  from_cache: boolean;
}
