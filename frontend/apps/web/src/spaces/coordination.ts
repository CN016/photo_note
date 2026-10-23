import { ref } from 'vue'
import { acceptTokenPair, api, clearTokens, drainSpaceRequests, getSessionInfo, pauseSpaceRequests, SPACE_SWITCH_KEY, withRefreshLock } from '../api/client'

/** 同步切换先排空所有存活标签页，任何页面未保存成功都保留原空间。 */
export const preparingSpaceSwitch = ref(false)
type PendingSwitch = { id: string; session: string; version: number; orgid: number; deadline: number }
type Hooks = { prepare: () => Promise<boolean>; committed: () => Promise<void>; loggedOut: () => Promise<void> }
const channel = typeof BroadcastChannel !== 'undefined' ? new BroadcastChannel('photo-nini-spaces') : null
let hooks: Hooks | null = null
let releaseBarrier: (() => void) | null = null
let heldSession = ''
let preparingID = ''
let joining: Promise<void> | null = null
let cancelCurrent: (() => void) | null = null
let configured = false

export function supportsSynchronizedSpaces(): boolean { return Boolean(channel && navigator.locks) }
function pending(): PendingSwitch | null {
  try { return JSON.parse(localStorage.getItem(SPACE_SWITCH_KEY) || 'null') as PendingSwitch | null } catch { return null }
}

/** shared 锁由每个业务标签页持有；切换者只有等全部页面准备完成才能取得 exclusive 锁。 */
async function joinBarrier(afterFinish = false): Promise<void> {
  const session = getSessionInfo()?.session_id
  if (heldSession && heldSession !== session) { releaseBarrier?.(); await Promise.resolve() }
  if (!session || !navigator.locks || (!afterFinish && pending()) || heldSession === session || joining) return joining || undefined
  joining = new Promise<void>(resolve => {
    void navigator.locks.request(`photo-nini-space-barrier:${session}`, { mode: 'shared' }, async () => {
      if (!afterFinish && pending()) { resolve(); return }
      heldSession = session
      await new Promise<void>(release => {
        // 先同步清除持锁标记，再释放锁。提交回调可能紧接着重新加入，
        // 不能让旧锁的异步清理覆盖新锁，也不能误以为旧 shared 锁仍有效。
        releaseBarrier = () => { heldSession = ''; releaseBarrier = null; release() }
        resolve()
      })
    })
  })
  try { await joining } finally { joining = null }
}

async function prepare(operation: PendingSwitch): Promise<void> {
  if (preparingID === operation.id || getSessionInfo()?.session_id !== operation.session) return
  preparingID = operation.id
  preparingSpaceSwitch.value = true
  try {
    if (!hooks || !await hooks.prepare()) throw new Error('space_save_failed')
    if (pending()?.id !== operation.id || Date.now() >= operation.deadline) throw new Error('space_switch_expired')
    await drainSpaceRequests(operation.deadline)
    if (pending()?.id !== operation.id) throw new Error('space_switch_expired')
    pauseSpaceRequests(true)
    releaseBarrier?.()
  } catch {
    channel?.postMessage({ type: 'rejected', id: operation.id })
    cancelCurrent?.()
  }
}

async function finish(committed: boolean): Promise<void> {
  preparingID = ''
  pauseSpaceRequests(false)
  preparingSpaceSwitch.value = false
  // 已接收终态的页面必须重新持锁，不能因上一操作的 pending 快照跳过。
  await joinBarrier(true)
  if (committed) await hooks?.committed()
}

/** App 只注册一次，避免随页面卸载丢失切换与保存协调。 */
export function configureSpaceCoordination(value: Hooks): void {
  hooks = value
  if (configured) return
  configured = true
  window.addEventListener('photo-nini-session-updated', () => { if (!getSessionInfo()) releaseBarrier?.(); else if (!pending()) void joinBarrier() })
  channel?.addEventListener('message', (event: MessageEvent<{ type: string; id?: string; session?: string; operation?: PendingSwitch }>) => {
    if (event.data.type === 'prepare' && event.data.operation) void prepare(event.data.operation)
    if (event.data.type === 'rejected' && event.data.id === pending()?.id) cancelCurrent?.()
    if (event.data.type === 'finished' && event.data.id === preparingID) void finish(true)
    if (event.data.type === 'cancelled' && event.data.id === preparingID) void finish(false)
    if (event.data.type === 'uncertain' && event.data.session === getSessionInfo()?.session_id) { clearTokens(); void hooks?.loggedOut() }
  })
  window.addEventListener('pagehide', () => { releaseBarrier?.() })
  window.addEventListener('pageshow', event => {
    // BFCache 恢复的页面可能错过广播，重新挂载后才允许编辑，不能把旧画布套到新会话。
    if (event.persisted) { pauseSpaceRequests(true); window.location.reload(); return }
    if (pending()) void prepare(pending()!); else void joinBarrier()
  })
  // 协调页异常关闭后，其他页面在超过有限等待时间时清理本机会话，避免猜测服务端是否已提交。
  window.setInterval(() => {
    const operation = pending()
    if (operation && Date.now() > operation.deadline + 10000) {
      localStorage.removeItem(SPACE_SWITCH_KEY)
      if (operation.session === getSessionInfo()?.session_id) {
        clearTokens()
        channel?.postMessage({ type: 'uncertain', session: operation.session, id: operation.id })
        void hooks?.loggedOut()
      }
      if (preparingID === operation.id) void finish(false)
    }
  }, 1000)
  void joinBarrier()
}

/** 刷新与提交切换共享凭证锁；prepare 期间不持该锁，使旧空间保存仍能正常刷新。 */
export async function switchSpace(orgid: number): Promise<void> {
  if (!supportsSynchronizedSpaces()) throw new Error('space_switch_unsupported')
  const session = getSessionInfo()
  if (!session || session.orgid === orgid) return
  await navigator.locks.request(`photo-nini-space-coordinator:${session.session_id}`, { ifAvailable: true }, async lock => {
    if (!lock || pending()) throw new Error('space_switch_busy')
    await joinBarrier()
    const operation: PendingSwitch = { id: crypto.randomUUID(), session: session.session_id, version: session.session_version, orgid, deadline: Date.now() + 30000 }
    const controller = new AbortController()
    cancelCurrent = () => controller.abort()
    const timer = window.setTimeout(() => controller.abort(), 30000)
    let submitted = false
    let committed = false
    localStorage.setItem(SPACE_SWITCH_KEY, JSON.stringify(operation))
    channel!.postMessage({ type: 'prepare', operation })
    try {
      await prepare(operation)
      await navigator.locks.request(`photo-nini-space-barrier:${session.session_id}`, { mode: 'exclusive', signal: controller.signal }, async () => {
        await withRefreshLock(async () => {
          const current = getSessionInfo()
          if (controller.signal.aborted || current?.session_id !== session.session_id || current.session_version !== session.session_version) throw new Error('space_changed')
          submitted = true
          const result = await api.switchEnvironment(orgid, session.session_version, operation.id)
          acceptTokenPair(result.tokens)
          committed = true
        })
      })
    } catch (error) {
      if (submitted && !committed && getSessionInfo()?.session_id === operation.session) {
        // 网络结果未知时不把旧令牌当成仍有效；各页已先保存，重新登录不会丢弃未保存内容。
        clearTokens()
        channel!.postMessage({ type: 'uncertain', session: operation.session, id: operation.id })
        await hooks?.loggedOut()
      }
      throw error
    } finally {
      window.clearTimeout(timer)
      cancelCurrent = null
      if (pending()?.id === operation.id) localStorage.removeItem(SPACE_SWITCH_KEY)
      channel!.postMessage({ type: committed ? 'finished' : 'cancelled', id: operation.id })
      if (preparingID === operation.id) await finish(committed)
    }
  })
}
