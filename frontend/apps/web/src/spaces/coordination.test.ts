import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const state = vi.hoisted(() => ({ session: { session_id: 'session', session_version: 1, orgid: -1 }, paused: false, switched: vi.fn() }))
vi.mock('../api/client', () => ({
  SPACE_SWITCH_KEY: 'pending',
  getSessionInfo: () => state.session,
  pauseSpaceRequests: (value: boolean) => { state.paused = value },
  drainSpaceRequests: async () => {},
  withRefreshLock: async (action: () => unknown) => action(),
  clearTokens: vi.fn(),
  acceptTokenPair: (tokens: typeof state.session) => { state.session = tokens },
  api: { switchEnvironment: async (orgid: number) => { state.switched(orgid); return { tokens: { ...state.session, orgid, session_version: state.session.session_version + 1 } } } },
}))

let held: number
let queued: Array<{ shared: boolean; action: () => Promise<void> }>
let intervals: ReturnType<typeof setInterval>[]
function flushLocks() {
  while (queued.length && (!held || (held > 0 && queued[0]!.shared))) {
    const job = queued.shift()!
    held += job.shared ? 1 : -1
    void job.action().finally(() => { held += job.shared ? -1 : 1; flushLocks() })
  }
}
beforeEach(() => {
  vi.resetModules()
  state.session = { session_id: 'session', session_version: 1, orgid: -1 }
  state.paused = false
  state.switched.mockClear()
  held = 0; queued = []; intervals = []
  const storage = new Map<string, string>()
  vi.stubGlobal('localStorage', { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value), removeItem: (key: string) => storage.delete(key) })
  const windowStub = new EventTarget()
  Object.assign(windowStub, { setTimeout, clearTimeout, setInterval: (action: () => void, ms: number) => { const id = setInterval(action, ms); intervals.push(id); return id } })
  vi.stubGlobal('window', windowStub)
  vi.stubGlobal('BroadcastChannel', class { addEventListener() {} postMessage() {} })
  vi.stubGlobal('navigator', { locks: { request: (name: string, options: { mode?: string }, action: (lock: object) => Promise<void>) => {
    if (name.includes('coordinator')) return action({})
    return new Promise<void>((resolve, reject) => {
      queued.push({ shared: options.mode === 'shared', action: async () => { try { await action({}); resolve() } catch (error) { reject(error) } } })
      flushLocks()
    })
  } } })
})
afterEach(() => { intervals.forEach(clearInterval); vi.unstubAllGlobals() })

describe('空间同步屏障', () => {
  it('连续提交后重新持有 shared 锁，下一次切换仍经过准备', async () => {
    const coordination = await import('./coordination')
    const prepare = vi.fn(async () => true)
    const committed = vi.fn(async () => {})
    coordination.configureSpaceCoordination({ prepare, committed, loggedOut: async () => {} })
    await coordination.switchSpace(11)
    expect(held).toBe(1)
    await coordination.switchSpace(22)
    expect(held).toBe(1)
    expect(prepare).toHaveBeenCalledTimes(2)
    expect(committed).toHaveBeenCalledTimes(2)
    expect(state.session.orgid).toBe(22)
    expect(state.paused).toBe(false)
  })

  it('准备失败不提交凭证切换并恢复页面请求', async () => {
    const coordination = await import('./coordination')
    coordination.configureSpaceCoordination({ prepare: async () => false, committed: async () => {}, loggedOut: async () => {} })
    // 模拟浏览器 Web Locks 在 AbortSignal 已取消时拒绝 exclusive 请求。
    const request = navigator.locks.request.bind(navigator.locks)
    vi.spyOn(navigator.locks, 'request').mockImplementation(((name: string, options: LockOptions, callback: LockGrantedCallback<unknown>) => {
      if (options.signal?.aborted) return Promise.reject(new DOMException('Aborted', 'AbortError'))
      return request(name, options, callback)
    }) as typeof navigator.locks.request)
    await expect(coordination.switchSpace(11)).rejects.toThrow()
    expect(state.switched).not.toHaveBeenCalled()
    expect(state.session.orgid).toBe(-1)
    expect(state.paused).toBe(false)
    expect(held).toBe(1)
  })
})
