/**
 * lib/navigation.ts 的验收靶。
 *
 * 契约（与 1a 设计一致；2026-10-16 新增数据看板）：
 * 1. 全站统一顺序：我的订单 → 订单管理 → 数据看板 → 用户管理 → 组织架构 → 账号设置；
 * 2. 可见范围：我的订单 / 账号设置 = 所有角色；订单管理 / 数据看板 = 运营与管理员；
 *    用户管理 / 组织架构 = 仅管理员。
 *
 * 实现前预期红（NAV_ITEMS 为空、navItemsFor 返回空数组），实现后应变绿。
 */

import { describe, expect, it } from 'vitest';

import { NAV_ITEMS, navItemsFor } from './navigation';
import type { Role } from './types';

/** 全站统一顺序。任何角色的导航都应是它的保序子序列。 */
const FULL_ORDER = [
  '/orders',
  '/admin/orders',
  '/admin/dashboard',
  '/admin/users',
  '/admin/org',
  '/settings',
];

describe('navigation', () => {
  it('NAV_ITEMS 覆盖全部六个导航项，且顺序统一', () => {
    expect(NAV_ITEMS.map((item) => item.href)).toEqual(FULL_ORDER);
  });

  it('导航项文案与路径对应', () => {
    const labels = new Map(NAV_ITEMS.map((item) => [item.href, item.label]));
    expect(labels.size).toBe(6);
    expect(labels.get('/orders')).toBe('我的订单');
    expect(labels.get('/admin/orders')).toBe('订单管理');
    expect(labels.get('/admin/dashboard')).toBe('数据看板');
    expect(labels.get('/admin/users')).toBe('用户管理');
    expect(labels.get('/admin/org')).toBe('组织架构');
    expect(labels.get('/settings')).toBe('账号设置');
  });

  it('普通用户：只有我的订单与账号设置', () => {
    expect(navItemsFor('user').map((item) => item.href)).toEqual(['/orders', '/settings']);
  });

  it('运营：可见订单管理与数据看板，不可见用户管理与组织架构', () => {
    expect(navItemsFor('ops').map((item) => item.href)).toEqual([
      '/orders',
      '/admin/orders',
      '/admin/dashboard',
      '/settings',
    ]);
  });

  it('管理员：全部可见且保持统一顺序', () => {
    expect(navItemsFor('admin').map((item) => item.href)).toEqual(FULL_ORDER);
  });

  it('任意角色的结果都是 NAV_ITEMS 的保序子序列（统一顺序契约）', () => {
    const all = NAV_ITEMS.map((item) => item.href);
    (['user', 'ops', 'admin'] as Role[]).forEach((role) => {
      const hrefs = navItemsFor(role).map((item) => item.href);
      expect(hrefs.length).toBeGreaterThan(0);
      expect(hrefs).toEqual(all.filter((href) => hrefs.includes(href)));
    });
  });
});
