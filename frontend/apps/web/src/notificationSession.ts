import { api, getSessionInfo } from './api/client'
import { nativeShell, supportsNotificationMonitor } from '../../../packages/platform-adapters'

let bound = ''
let flight: { stamp: string; promise: Promise<void> } | null = null
function stamp(): string { const session = getSessionInfo(); return session ? `${session.session_id}:${session.orgid}:${session.session_version}` : '' }

/** 登录页面与业务路由从不等待此函数。独立申请失败仅影响通知；同代次合并，迟到结果不能跨账号或空间绑定。 */
export async function ensureNotificationSession(): Promise<void> {
  const expected = stamp()
  if (!expected || bound === expected) return
  if (flight?.stamp === expected) return flight.promise
  const promise = (async () => {
    if (!await supportsNotificationMonitor() || stamp() !== expected) return
    const { tokens } = await api.notificationSession()
    if (stamp() !== expected) return
    await nativeShell.startNotificationMonitor({ identity: expected, tokens })
    if (stamp() === expected) bound = expected
  })()
  flight = { stamp: expected, promise }
  try { await promise } finally { if (flight?.promise === promise) flight = null }
}
