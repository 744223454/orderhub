/**
 * lib/redirect.ts 的验收靶。
 *
 * 契约（与 1b 设计一致）：
 * 1. sanitizeReturnTo：站内相对路径放行，绝对 URL / 协议相对 / 反斜杠变体 /
 *    登录相关页面 / 非斜杠开头的值一律拒绝（这是防开放重定向的安全边界）；
 * 2. buildLoginPath：from 参数必须正确编码，可被 URLSearchParams 完整还原；
 * 3. resolvePostLoginPath：合法 from 优先，否则回落角色首页；
 * 4. currentReturnTo：客户端返回 pathname + search，服务端回落 '/'；
 * 5. loginFrom 暂存：save 后 take 取回、只消费一次；服务端调用静默
 *    （save 无操作、take 返回 null）；
 * 6. readLoginFrom：从 URL 的 from 参数读取并解码，无参数 / 服务端返回 null。
 *
 * 验收标准：本文件即 redirect.ts 的行为契约，实现完成后应全部转绿；其余测试文件保持全绿。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  buildLoginPath,
  currentReturnTo,
  readLoginFrom,
  resolvePostLoginPath,
  sanitizeReturnTo,
  saveLoginFrom,
  takeLoginFrom,
} from './redirect';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('sanitizeReturnTo：开放重定向防护', () => {
  it('站内相对路径原样放行（含查询串）', () => {
    expect(sanitizeReturnTo('/orders')).toBe('/orders');
    expect(sanitizeReturnTo('/admin/orders?page=2&keyword=x')).toBe(
      '/admin/orders?page=2&keyword=x',
    );
  });

  it('拒绝绝对 URL（会把用户带出站外）', () => {
    expect(sanitizeReturnTo('https://evil.com')).toBeNull();
    expect(sanitizeReturnTo('http://evil.com/x')).toBeNull();
  });

  it('拒绝协议相对地址（// 开头）', () => {
    expect(sanitizeReturnTo('//evil.com')).toBeNull();
  });

  it('拒绝反斜杠变体（浏览器把 /\\ 按协议相对处理）', () => {
    expect(sanitizeReturnTo('/\\evil.com')).toBeNull();
    expect(sanitizeReturnTo('/\\/evil.com')).toBeNull();
  });

  it('拒绝登录相关页面，避免回跳循环', () => {
    expect(sanitizeReturnTo('/login')).toBeNull();
    expect(sanitizeReturnTo('/login?from=/orders')).toBeNull();
    expect(sanitizeReturnTo('/login/wecom/callback')).toBeNull();
    expect(sanitizeReturnTo('/login#x')).toBeNull();
  });

  it('拒绝空值与不以 / 开头的值', () => {
    expect(sanitizeReturnTo(null)).toBeNull();
    expect(sanitizeReturnTo('')).toBeNull();
    expect(sanitizeReturnTo('orders')).toBeNull();
    expect(sanitizeReturnTo('javascript:alert(1)')).toBeNull();
  });

  it('前缀相似的路径不受 /login 排除影响', () => {
    expect(sanitizeReturnTo('/login-help')).toBe('/login-help');
  });
});

describe('buildLoginPath：编码回跳目标', () => {
  it('把 / 与 ? 等编码进 from 参数', () => {
    expect(buildLoginPath('/admin/orders?page=2')).toBe('/login?from=%2Fadmin%2Forders%3Fpage%3D2');
  });

  it('编码结果可被 URLSearchParams 完整还原（含中文）', () => {
    const target = '/admin/users?keyword=张三';
    const path = buildLoginPath(target);
    const query = path.slice(path.indexOf('?') + 1);
    expect(new URLSearchParams(query).get('from')).toBe(target);
  });
});

describe('resolvePostLoginPath：登录后去哪', () => {
  it('合法 from 优先于角色首页', () => {
    expect(resolvePostLoginPath('/admin/orders?page=2', 'admin')).toBe('/admin/orders?page=2');
    expect(resolvePostLoginPath('/settings', 'user')).toBe('/settings');
  });

  it('from 缺失或非法时回落角色首页', () => {
    expect(resolvePostLoginPath(null, 'admin')).toBe('/admin/orders');
    expect(resolvePostLoginPath('//evil.com', 'user')).toBe('/orders');
    expect(resolvePostLoginPath('https://evil.com', 'ops')).toBe('/admin/orders');
  });
});

describe('currentReturnTo：当前地址', () => {
  it('服务端（无 window）回落根路径', () => {
    expect(currentReturnTo()).toBe('/');
  });

  it('客户端返回 pathname + search，忽略 hash', () => {
    vi.stubGlobal('window', {
      location: { pathname: '/orders', search: '?page=2', hash: '#top' },
    });
    expect(currentReturnTo()).toBe('/orders?page=2');
  });
});

describe('readLoginFrom：从 URL 读回跳目标', () => {
  it('服务端（无 window）返回 null', () => {
    expect(readLoginFrom()).toBeNull();
  });

  it('解析并解码 from 参数：%2F 回到 /、%3F 回到 ?', () => {
    vi.stubGlobal('window', { location: { search: '?from=%2Fadmin%2Forders%3Fpage%3D2' } });
    expect(readLoginFrom()).toBe('/admin/orders?page=2');
  });

  it('没有 from 参数时返回 null', () => {
    vi.stubGlobal('window', { location: { search: '?other=1' } });
    expect(readLoginFrom()).toBeNull();
  });
});

describe('loginFrom 暂存：企微授权往返', () => {
  it('服务端调用（无 window）时静默不抛：save 无操作、take 返回 null', () => {
    expect(() => saveLoginFrom('/orders')).not.toThrow();
    expect(takeLoginFrom()).toBeNull();
  });

  it('save 后可以 take 取回，且只消费一次', () => {
    const store = new Map<string, string>();
    vi.stubGlobal('window', {
      sessionStorage: {
        getItem: (key: string) => store.get(key) ?? null,
        setItem: (key: string, value: string) => void store.set(key, value),
        removeItem: (key: string) => void store.delete(key),
      },
    });

    saveLoginFrom('/admin/orders?page=2');
    expect(takeLoginFrom()).toBe('/admin/orders?page=2');
    expect(takeLoginFrom()).toBeNull();
  });
});
