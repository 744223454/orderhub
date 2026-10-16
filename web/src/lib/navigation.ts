/**
 * 全站导航模型。
 *
 * 背景：原先 (user) 与 (staff) 两个布局各写一份 Header 导航，构成与顺序互不相同
 * （用户端是「我的订单 → 订单管理 → 账号设置」，管理端是「订单管理 → 我的订单 → …」），
 * 从一端跳到另一端时，导航条上每一项的位置都在变，观感像两个不同的网站。
 *
 * 统一后：全站共用**同一份列表、同一个顺序**，两端的差异只体现为个别项的可见与否
 * （由 roles 白名单过滤）。AppShell 只负责渲染 navItemsFor(role) 的结果。
 *
 * 放在 lib 而不是组件文件里：纯数据 + 纯函数，和 roles.ts 一样不依赖 React 与 antd，
 * 服务端 / 客户端都可引用，也方便直接用 vitest 做单测（见 navigation.test.ts）。
 *
 * 为什么不做成「按角色分组的管理端/用户端两套导航」：两个端本质上是一个人可能
 * 同时拥有的两种视角（staff 也能看自己的订单），拆成两套只会让跳转时导航跳变；
 * 一份列表 + 过滤，天然保证顺序稳定。
 */

import type { Role } from './types';

/** 一个导航项。 */
export interface NavItem {
  /** 目标路径。 */
  href: string;
  /** 导航文案。 */
  label: string;
  /** 可见角色白名单；过滤时用 includes 判定。 */
  roles: Role[];
}

/**
 * 全站统一的导航列表（顺序即渲染顺序）。
 *
 * 统一顺序固定为：
 *   我的订单 → 订单管理 → 数据看板 → 用户管理 → 组织架构 → 账号设置
 *
 * 各项的可见范围（语义须与 lib/roles.ts 的 isStaff / isAdmin 以及后端
 * RequireRole 保持一致，不能放宽——前端多显示一个入口，用户点进去就是一排 403）：
 *   - 我的订单、账号设置：所有角色可见；
 *   - 订单管理、数据看板：运营与管理员可见（staff）；
 *   - 用户管理、组织架构：仅管理员可见（admin）。
 */
export const NAV_ITEMS: NavItem[] = [
  {
    href: '/orders',
    label: '我的订单',
    roles: ['user', 'ops', 'admin'],
  },
  {
    href: '/admin/orders',
    label: '订单管理',
    roles: ['ops', 'admin'],
  },
  {
    href: '/admin/dashboard',
    label: '数据看板',
    roles: ['ops', 'admin'],
  },
  {
    href: '/admin/users',
    label: '用户管理',
    roles: ['admin'],
  },
  {
    href: '/admin/org',
    label: '组织架构',
    roles: ['admin'],
  },
  {
    href: '/settings',
    label: '账号设置',
    roles: ['user', 'ops', 'admin'],
  },
];

/**
 * 过滤出某角色可见的导航项。
 *
 * 契约（navigation.test.ts 验收）：结果保持 NAV_ITEMS 的顺序，
 * 且不引入列表之外的项——即「保序子序列」。
 */
export function navItemsFor(role: Role): NavItem[] {
  return NAV_ITEMS.filter((item) => item.roles.includes(role));
}
