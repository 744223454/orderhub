/**
 * lib/api.ts 中 request() 的 401 拦截验收靶（练习六批次 2）。
 *
 * 契约（无感刷新的请求层）：
 * 1. 携带令牌的请求收到 401 → 调用 refreshAccessToken 一次 → 用新令牌把原请求
 *    原样重发（路径 / 方法 / body 不动）→ 正常返回重试结果；
 * 2. 未携带令牌的请求收到 401（如登录密码错误）→ 不做刷新，直接抛错——防刷新循环；
 * 3. 豁免路径（登出等凭据类接口）的 401 → 不做刷新重试；
 * 4. 刷新失败 → 抛 401 错误，且不重发原请求
 *    （会话的清空与广播由 session-refresh 侧负责，本层不做）；
 * 5. 只重试一次：重试后仍 401 → 直接抛错，不再刷新。
 *
 * 验收标准：本文件即 request() 401 拦截的行为契约，实现完成后应全部转绿；其余测试文件保持全绿。
 *
 * 说明：storage 与 session-refresh 都整体 mock；「刷新成功会更新存储里的令牌」
 * 用 currentToken 变量模拟（真实实现在 saveTokens 后，readToken 会读到新值）。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('./storage', () => ({
  readToken: vi.fn(),
}));
vi.mock('./session-refresh', () => ({
  refreshAccessToken: vi.fn(),
}));

import { ApiError, request } from './api';
import * as sessionRefresh from './session-refresh';
import * as storage from './storage';

const mockedStorage = vi.mocked(storage);
const mockedRefresh = vi.mocked(sessionRefresh);

/** 构造一个够用的 Response 替身（实现只需看 ok / status / json）。 */
function jsonResponse(status: number, body: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  } as unknown as Response;
}

let fetchMock: ReturnType<typeof vi.fn>;
/** 模拟存储里的 access 令牌；刷新成功时由刷新 mock 更新。 */
let currentToken: string | null;

beforeEach(() => {
  vi.clearAllMocks();
  currentToken = 'old-access';
  fetchMock = vi.fn();
  vi.stubGlobal('fetch', fetchMock);
  mockedStorage.readToken.mockImplementation(() => currentToken);
  mockedRefresh.refreshAccessToken.mockImplementation(async () => {
    currentToken = 'new-access';
    return 'new-access';
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('request：401 拦截与重试', () => {
  it('携带令牌的请求 401 → 刷新一次后原样重发，返回重试结果', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse(401, { error: '未认证' }))
      .mockResolvedValueOnce(jsonResponse(200, { id: 7 }));

    const body = JSON.stringify({ product_name: '测试商品', amount: 100 });
    const result = await request<{ id: number }>('/api/orders', { method: 'POST', body });

    expect(result).toEqual({ id: 7 });
    expect(mockedRefresh.refreshAccessToken).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);

    const [firstPath, firstInit] = fetchMock.mock.calls[0] as [string, RequestInit];
    const [retryPath, retryInit] = fetchMock.mock.calls[1] as [string, RequestInit];
    expect(firstPath).toBe('/api/orders');
    expect(retryPath).toBe('/api/orders');
    expect(retryInit.method).toBe('POST');
    expect(retryInit.body).toBe(body);
    expect((firstInit.headers as Headers).get('Authorization')).toBe('Bearer old-access');
    expect((retryInit.headers as Headers).get('Authorization')).toBe('Bearer new-access');
  });

  it('未携带令牌的请求 401（登录场景）→ 不刷新，直接抛错', async () => {
    currentToken = null;
    fetchMock.mockResolvedValueOnce(jsonResponse(401, { error: '用户名或密码错误' }));

    await expect(request('/api/login', { method: 'POST', body: '{}' })).rejects.toThrow(
      '用户名或密码错误',
    );
    expect(mockedRefresh.refreshAccessToken).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('豁免路径（登出）的 401 → 不刷新', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(401, { error: '未认证' }));

    await expect(request('/api/logout', { method: 'POST', body: '{}' })).rejects.toThrow();
    expect(mockedRefresh.refreshAccessToken).not.toHaveBeenCalled();
  });

  it('刷新失败 → 抛 401 错误，且不重发原请求', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(401, { error: '未认证' }));
    mockedRefresh.refreshAccessToken.mockRejectedValueOnce(new Error('会话已失效'));

    const err = await request('/api/orders').catch((e: unknown) => e);

    expect(mockedRefresh.refreshAccessToken).toHaveBeenCalledTimes(1);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(401);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('重试后仍 401 → 直接抛错，不再刷新（只重试一次）', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse(401, { error: '未认证' }))
      .mockResolvedValueOnce(jsonResponse(401, { error: '未认证' }));

    const err = await request('/api/orders').catch((e: unknown) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect(mockedRefresh.refreshAccessToken).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
