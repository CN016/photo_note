import { beforeEach, expect, it, vi } from 'vitest'
const state = vi.hoisted(() => ({ session: { session_id: 'web', orgid: 17, session_version: 1 }, mint: vi.fn(), start: vi.fn(), supports: vi.fn() }))
vi.mock('./api/client', () => ({ getSessionInfo: () => state.session, api: { notificationSession: state.mint } }))
vi.mock('../../../packages/platform-adapters', () => ({ supportsNotificationMonitor: state.supports, nativeShell: { startNotificationMonitor: state.start } }))
beforeEach(() => { vi.resetModules(); vi.clearAllMocks(); state.session = { session_id: 'web', orgid: 17, session_version: 1 }; state.supports.mockResolvedValue(true); state.start.mockResolvedValue({}) })
it('合并同会话初始化，通知令牌只交给通知桥接', async () => {
  const tokens = { access_token: 'notice', refresh_token: 'notice-refresh' }
  state.mint.mockResolvedValue({ tokens })
  const { ensureNotificationSession } = await import('./notificationSession')
  await Promise.all([ensureNotificationSession(), ensureNotificationSession()]); await ensureNotificationSession()
  expect(state.mint).toHaveBeenCalledTimes(1)
  expect(state.start).toHaveBeenCalledWith({ identity: 'web:17:1', tokens })
})
it('通知初始化失败后可重试，不改变Web会话', async () => {
  state.mint.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ tokens: {} })
  const { ensureNotificationSession } = await import('./notificationSession')
  await expect(ensureNotificationSession()).rejects.toThrow('offline'); await ensureNotificationSession()
  expect(state.session.session_id).toBe('web'); expect(state.mint).toHaveBeenCalledTimes(2)
})
it('空间切换后的迟到通知令牌不得绑定', async () => {
  let done!: (value: unknown) => void
  state.mint.mockImplementation(() => new Promise(resolve => { done = resolve }))
  const { ensureNotificationSession } = await import('./notificationSession')
  const pending = ensureNotificationSession(); await vi.waitFor(() => expect(state.mint).toHaveBeenCalledTimes(1))
  state.session = { session_id: 'web', orgid: 23, session_version: 2 }; done({ tokens: {} }); await pending
  expect(state.start).not.toHaveBeenCalled()
})
