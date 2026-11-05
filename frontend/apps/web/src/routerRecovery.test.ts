import { beforeEach, describe, expect, it, vi } from 'vitest'

const fixture = vi.hoisted(() => ({
  guard: null as null | ((to: { name: string; meta: { public?: boolean } }) => Promise<unknown>),
  auth: { recoveryPending: true, user: null, initialize: vi.fn(async () => {}) },
}))
vi.mock('vue-router', () => ({ createWebHistory: () => ({}), createRouter: () => ({ beforeEach: (guard: typeof fixture.guard) => { fixture.guard = guard } }) }))
vi.mock('./backendHealth', () => ({ checkBackendAvailability: async () => true }))
vi.mock('./stores/auth', () => ({ useAuthStore: () => fixture.auth }))
vi.mock('./api/client', () => ({ api: {} }))

describe('启动失败后的恢复页导航', () => {
  beforeEach(async () => { vi.resetModules(); fixture.auth.initialize.mockClear(); fixture.auth.recoveryPending = true; await import('./router') })
  it('失败后进入登录恢复页不再重复等待一轮认证', async () => {
    await expect(fixture.guard!({ name: 'login', meta: { public: true } })).resolves.toBe(true)
    expect(fixture.auth.initialize).not.toHaveBeenCalled()
  })
  it('首次进入受保护页面仍必须等待鉴权，失败后转恢复页', async () => {
    fixture.auth.recoveryPending = false
    await expect(fixture.guard!({ name: 'messages', meta: {} })).resolves.toBe('/login')
    expect(fixture.auth.initialize).toHaveBeenCalledTimes(1)
  })
})
