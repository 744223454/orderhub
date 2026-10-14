/**
 * lib/session-refresh.ts 的验收靶。
 *
 * 契约：
 * 1. 合流（singleflight）：并发调用只发一次刷新请求，调用方拿到同一个新令牌——
 *    否则多个请求同时 401 时各自轮转同一个旧串，第二次被后端判重放、连坐撤销；
 * 2. 请求形状：POST /api/refresh，body 为 { refresh_token }；
 * 3. 成功：saveTokens(新令牌对) 落库，返回新 access 令牌；
 * 4. 本地没有 refresh 令牌：直接拒绝，不发请求；
 * 5. 被拒绝（401）：clearSession + notifySessionExpired，再拒绝；
 * 6. 网络故障：只拒绝，不动会话；
 * 7. 进行中的状态不缓存结果：成功后 / 失败后再次调用都会重新发起请求
 *    （第一次失败不能把后续刷新拖死）。
 *
 * 验收标准：本文件即 session-refresh.ts 的行为契约，实现完成后应全部转绿；其余测试文件保持全绿。
 *
 * 说明：Node 环境没有 localStorage，storage 整体 mock；网络用 stubGlobal 替换 fetch。
 * 合流依赖模块级的「进行中」状态，因此每个用例都把发起的 Promise 等到落定，
 * 避免状态跨用例串味。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('./storage', () => ({
  readRefreshToken: vi.fn(),
  saveTokens: vi.fn(),
  clearSession: vi.fn(),
  notifySessionExpired: vi.fn(),
}));

import * as storage from './storage';
import { refreshAccessToken } from './session-refresh';

const mocked = vi.mocked(storage);

/** 构造一个够用的 Response 替身（实现只需看 ok / status / json）。 */
function jsonResponse(status: number, body: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  } as unknown as Response;
}

/** 刷新接口成功时的标准响应体。 */
const OK_BODY = { token: 'new-access', refresh_token: 'new-refresh' };

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.clearAllMocks();
  fetchMock = vi.fn();
  vi.stubGlobal('fetch', fetchMock);
  mocked.readRefreshToken.mockReturnValue('old-refresh');
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('refreshAccessToken：并发合流（singleflight）', () => {
  it('三个并发调用只发一次请求，且都拿到同一个新令牌', async () => {
    // 用手动控制的 Promise 把请求「悬停」住，确保三次调用都发生在响应之前。
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    fetchMock.mockReturnValue(gate.then(() => jsonResponse(200, OK_BODY)));

    const p1 = refreshAccessToken();
    const p2 = refreshAccessToken();
    const p3 = refreshAccessToken();
    // 三个 Promise 要到最后才被 await；先各挂一个空的拒绝处理器防噪音——
    // 中间断言失败时它们会以 unhandled rejection 的形式额外刷屏（不影响断言本身）。
    void p1.catch(() => {});
    void p2.catch(() => {});
    void p3.catch(() => {});

    // 第一次调用已经发出请求，第二、三次必须复用它，而不是各发一份。
    expect(fetchMock).toHaveBeenCalledTimes(1);

    release();
    await expect(p1).resolves.toBe('new-access');
    await expect(p2).resolves.toBe('new-access');
    await expect(p3).resolves.toBe('new-access');
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(mocked.saveTokens).toHaveBeenCalledTimes(1);
    expect(mocked.saveTokens).toHaveBeenCalledWith('new-access', 'new-refresh');
  });

  it('请求形状：POST /api/refresh，body 携带 refresh_token', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, OK_BODY));

    await refreshAccessToken();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [path, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(path).toBe('/api/refresh');
    expect(init?.method).toBe('POST');
    expect(init?.body).toBe(JSON.stringify({ refresh_token: 'old-refresh' }));
  });
});

describe('refreshAccessToken：失败语义', () => {
  it('本地没有 refresh 令牌时直接拒绝，不发请求', async () => {
    mocked.readRefreshToken.mockReturnValue(null);

    await expect(refreshAccessToken()).rejects.toThrow();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('被拒绝（401）时：清会话 + 广播失效事件 + 拒绝', async () => {
    fetchMock.mockResolvedValue(jsonResponse(401, { error: '刷新令牌无效' }));

    await expect(refreshAccessToken()).rejects.toThrow();
    expect(mocked.clearSession).toHaveBeenCalledTimes(1);
    expect(mocked.notifySessionExpired).toHaveBeenCalledTimes(1);
  });

  it('网络故障只拒绝，不动会话', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'));

    await expect(refreshAccessToken()).rejects.toThrow();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(mocked.clearSession).not.toHaveBeenCalled();
    expect(mocked.notifySessionExpired).not.toHaveBeenCalled();
  });
});

describe('refreshAccessToken：进行中状态的生命周期', () => {
  it('失败不被缓存：一次失败后再次调用会重新发起请求', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(401, { error: '刷新令牌无效' }));
    await expect(refreshAccessToken()).rejects.toThrow();

    fetchMock.mockResolvedValueOnce(jsonResponse(200, OK_BODY));
    await expect(refreshAccessToken()).resolves.toBe('new-access');
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('成功也不缓存：刷新完成后再次调用会重新发起请求', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, OK_BODY));

    await refreshAccessToken();
    await refreshAccessToken();

    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
