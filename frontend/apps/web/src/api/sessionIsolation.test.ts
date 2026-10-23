import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { TokenPair } from './types'

vi.mock('./config', () => ({ API_REQUEST_ORIGIN: '' }))

const pair = (orgid: number, version: number): TokenPair => ({ access_token: `access-${version}`, refresh_token: `refresh-${version}`, token_type: 'Bearer', expires_in: 60, refresh_expires_in: 600, orgid, session_id: 'same-browser-session', session_version: version })
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }

beforeEach(() => {
  vi.resetModules()
  const values = new Map<string, string>()
  vi.stubGlobal('localStorage', { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value), removeItem: (key: string) => values.delete(key) })
  const windowStub = new EventTarget()
  Object.assign(windowStub, { setTimeout, clearTimeout })
  vi.stubGlobal('window', windowStub)
  vi.stubGlobal('navigator', { locks: { request: async (_: string, _options: unknown, action: () => unknown) => action() } })
  vi.stubGlobal('BroadcastChannel', class { addEventListener() {} postMessage() {} })
})

describe('空间切换中的请求隔离', () => {
  it('旧请求返回401时不能用新组织凭证重放，也不能清除新会话', async () => {
    const client = await import('./client')
    client.acceptTokenPair(pair(11, 1))
    const response = deferred<Response>()
    const fetchMock = vi.fn(() => response.promise)
    vi.stubGlobal('fetch', fetchMock)
    const request = client.api.tags()
    const rejected = expect(request).rejects.toMatchObject({ code: 'space_changed' })
    client.acceptTokenPair(pair(22, 2))
    response.resolve(new Response('', { status: 401 }))
    await rejected
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(client.getRefreshToken()).toBe('refresh-2')
  })

  it('迟到的JSON正文不能进入新空间的页面状态', async () => {
    const client = await import('./client')
    client.acceptTokenPair(pair(11, 1))
    const body = deferred<unknown>()
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: () => body.promise })))
    const request = client.api.tags()
    const rejected = expect(request).rejects.toMatchObject({ code: 'space_changed' })
    await Promise.resolve()
    client.acceptTokenPair(pair(22, 2))
    body.resolve({ data: { items: [] } })
    await rejected
  })

  it('同空间正常令牌刷新不废弃正在读取的数据', async () => {
    const client = await import('./client')
    client.acceptTokenPair(pair(11, 1))
    const response = deferred<Response>()
    vi.stubGlobal('fetch', vi.fn(() => response.promise))
    const request = client.api.tags()
    client.acceptTokenPair({ ...pair(11, 1), access_token: 'rotated-access', refresh_token: 'rotated-refresh' })
    response.resolve(Response.json({ data: { items: ['same-space'] } }))
    await expect(request).resolves.toEqual({ items: ['same-space'] })
  })

  it('迟到的旧代次凭证不能覆盖已提交的新组织', async () => {
    const client = await import('./client')
    client.acceptTokenPair(pair(22, 2))
    client.acceptTokenPair(pair(11, 1))
    expect(client.getSessionInfo()?.orgid).toBe(22)
    expect(client.getRefreshToken()).toBe('refresh-2')
  })
})
