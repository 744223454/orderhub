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

/** 登录请求体，对应 POST /api/login。 */
export interface LoginRequest {
  username: string;
  password: string;
}

/** 登录成功响应，对应 POST /api/login 的返回。 */
export interface LoginResponse {
  token: string;
  user: User;
}

/** 当前登录用户，对应 GET /api/me 的返回。 */
export interface MeResponse {
  user_id: number;
  role: Role;
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
