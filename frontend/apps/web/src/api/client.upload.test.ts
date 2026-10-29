import { beforeEach, describe, expect, it, vi } from 'vitest'
vi.mock('./config', () => ({ API_REQUEST_ORIGIN: '' }))

/** 测试用 XHR 允许控制网络进度、响应与会话变化，不依赖真实服务。 */
class TestXHR {
  static calls: TestXHR[] = []
  upload: { onprogress?: (event: { lengthComputable: boolean; loaded: number; total: number }) => void } = {}
  onload?: () => void
  onerror?: () => void
  onabort?: () => void
  status = 0
  responseText = ''
  headers: Record<string, string> = {}
  open(_method: string, _url: string) {}
  setRequestHeader(name: string, value: string) { this.headers[name] = value }
  send(_body: FormData) { TestXHR.calls.push(this) }
  respond(status: number, body: unknown) { this.status = status; this.responseText = JSON.stringify(body); this.onload?.() }
}
const values = new Map<string, string>()
const storage = { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value) }, removeItem: (key: string) => { values.delete(key) } }
const session = { session_id: 'session', orgid: -1, session_version: 1, refresh_token: 'test-refresh' }

describe('带进度的文件上传', () => {
  beforeEach(() => {
    vi.resetModules(); values.clear(); TestXHR.calls = []
    storage.setItem('photo-nini.session', JSON.stringify(session))
    vi.stubGlobal('localStorage', storage)
    vi.stubGlobal('window', { addEventListener() {}, dispatchEvent() {} })
    vi.stubGlobal('navigator', { locks: { request: async (_key: string, _options: unknown, callback: () => unknown) => callback() } })
    vi.stubGlobal('BroadcastChannel', undefined)
    vi.stubGlobal('XMLHttpRequest', TestXHR)
  })
  it('报告真实进度并保留 Bearer 鉴权', async () => {
    const { api, setAccessToken } = await import('./client')
    setAccessToken('test-access')
    const progress = vi.fn(); const promise = api.uploadWithProgress(new Blob(['test']), 'test.pdf', progress)
    const xhr = TestXHR.calls[0]!
    xhr.upload.onprogress?.({ lengthComputable: true, loaded: 4, total: 10 })
    xhr.respond(200, { data: { storekey: 'local_test' } })
    expect(await promise).toEqual({ storekey: 'local_test' })
    expect(progress).toHaveBeenCalledWith(4, 10)
    expect(xhr.headers.Authorization).toBe('Bearer test-access')
  })
  it('空间切换后拒绝旧请求结果', async () => {
    const { api } = await import('./client')
    const promise = api.uploadWithProgress(new Blob(['test']), 'test.pdf', () => {})
    storage.setItem('photo-nini.session', JSON.stringify({ ...session, orgid: 2, session_version: 2 }))
    TestXHR.calls[0]!.respond(200, { data: { storekey: 'local_test' } })
    await expect(promise).rejects.toMatchObject({ code: 'space_changed' })
  })
  it('401 后刷新令牌并仅重试一次', async () => {
    const { api, setAccessToken } = await import('./client')
    setAccessToken('expired')
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ data: { tokens: { ...session, access_token: 'renewed', refresh_token: 'renewed-refresh' } } }), { status: 200 })))
    const promise = api.uploadWithProgress(new Blob(['test']), 'test.pdf', () => {})
    TestXHR.calls[0]!.respond(401, { error: { code: 'unauthorized' } })
    await vi.waitFor(() => expect(TestXHR.calls).toHaveLength(2))
    expect(TestXHR.calls[1]!.headers.Authorization).toBe('Bearer renewed')
    TestXHR.calls[1]!.respond(200, { data: { storekey: 'local_test' } })
    expect(await promise).toEqual({ storekey: 'local_test' })
  })
})
