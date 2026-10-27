import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../api/config', () => ({ API_REQUEST_ORIGIN: '' }))
vi.mock('./appShell', () => ({ useAppShellStore: () => ({ reset: vi.fn() }) }))

const values = new Map<string, string>()
const tokens = { access_token: 'fixture-access', refresh_token: 'fixture-refresh', token_type: 'Bearer', expires_in: 60, refresh_expires_in: 600, orgid: 17, session_id: 'fixture-session', session_version: 2 }
const user = { business_id: 'fixture-user', nickname: 'fixture' }
beforeEach(() => {
  vi.resetModules(); values.clear()
  vi.stubGlobal('localStorage', { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value), removeItem: (key: string) => values.delete(key) })
  const target = new EventTarget(); Object.assign(target, { setTimeout, clearTimeout, location: { reload: vi.fn() } }); vi.stubGlobal('window', target)
  vi.stubGlobal('navigator', { locks: { request: async (_: string, _options: unknown, action: () => unknown) => action() } })
  vi.stubGlobal('BroadcastChannel', class { addEventListener() {} postMessage() {} })
})
async function freshStore() {
  const { createPinia, setActivePinia } = await import('pinia'); setActivePinia(createPinia())
  return (await import('./auth')).useAuthStore()
}
function seed() { values.set('photo-nini.session', JSON.stringify(tokens)); values.set('photo-nini.refresh-token', tokens.refresh_token) }
function healthy() { return vi.fn(async (path: string) => path.endsWith('/auth/refresh') ? Response.json({ data: { tokens } }) : Response.json({ data: user })) }

describe('页面重载后的登录恢复', () => {
  it('模块和Pinia内存全部重建后从持久凭据恢复用户和空间，不调用登录', async () => {
    seed(); const fetch = healthy(); vi.stubGlobal('fetch', fetch)
    let store = await freshStore(); await store.initialize(); expect(store.user?.business_id).toBe(user.business_id)
    vi.resetModules(); store = await freshStore(); expect(store.user).toBeNull(); expect(store.ready).toBe(false)
    await store.initialize(); expect(store.user?.business_id).toBe(user.business_id); expect(store.orgid).toBe(17); expect(store.sessionVersion).toBe(2)
    expect(fetch.mock.calls.map(call => call[0]).filter(path => path.endsWith('/auth/refresh'))).toHaveLength(2)
    expect(fetch.mock.calls.some(call => call[0].endsWith('/auth/login'))).toBe(false)
  })
  it('启动网络失败保留凭据和可重试状态，恢复网络后能重建store', async () => {
    seed(); vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('offline') }))
    const store = await freshStore(); await store.initialize(); expect(store.user).toBeNull(); expect(store.recoveryPending).toBe(true); expect(store.ready).toBe(false)
    expect(values.get('photo-nini.refresh-token')).toBe(tokens.refresh_token)
    vi.stubGlobal('fetch', healthy()); await store.initialize(); expect(store.user?.business_id).toBe(user.business_id); expect(store.ready).toBe(true); expect(store.recoveryPending).toBe(false)
  })
  it('资料读取503同样保留已轮换凭据，明确401才退出', async () => {
    seed(); vi.stubGlobal('fetch', vi.fn(async (path: string) => path.endsWith('/auth/refresh') ? Response.json({ data: { tokens } }) : Response.json({ error: { code: 'unavailable' } }, { status: 503 })))
    const store = await freshStore(); await store.initialize(); expect(store.recoveryPending).toBe(true); expect(values.has('photo-nini.session')).toBe(true)
    vi.stubGlobal('fetch', vi.fn(async () => Response.json({ error: { code: 'unauthorized' } }, { status: 401 })))
    await store.initialize(); expect(store.recoveryPending).toBe(false); expect(store.ready).toBe(true); expect(values.has('photo-nini.session')).toBe(false)
  })
  it('组织被停用时明确结束恢复，不继续恢复已失效空间', async () => {
    seed(); vi.stubGlobal('fetch', vi.fn(async () => Response.json({ error: { code: 'environment_unavailable' } }, { status: 403 })))
    const store = await freshStore(); await store.initialize(); expect(store.recoveryPending).toBe(false); expect(values.has('photo-nini.session')).toBe(false)
  })
  it('显式退出清除凭据并结束恢复状态', async () => {
    seed(); vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('offline') }))
    const store = await freshStore(); await store.initialize(); await expect(store.signOut()).rejects.toMatchObject({ code: 'network' }); expect(store.recoveryPending).toBe(false); expect(values.has('photo-nini.session')).toBe(false)
  })
})
