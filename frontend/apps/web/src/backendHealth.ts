import type { Router } from 'vue-router'
import { reactive } from 'vue'
import { API_REQUEST_ORIGIN } from './api/config'

const RETURN_PATH_KEY = 'photo-nini.maintenance-return-path'
const HEALTH_URL = `${API_REQUEST_ORIGIN}/healthz`
let lastCheckedAt = 0
let currentAvailability: boolean | null = null
let activeCheck: Promise<boolean> | null = null
let routerRef: Router | null = null
let wakeTimer = 0
export const backendHealthState = reactive({
  checkedAt: null as Date | null,
  available: null as boolean | null,
  version: null as string | null,
})

/** 从成功封套中提取 readiness 数据，避免把代理返回的任意 2xx 页面当作后端可用。 */
function readHealthData(payload: unknown): { status?: unknown; version?: unknown } | null {
  if (typeof payload !== 'object' || payload === null || !('data' in payload)) return null
  const data = payload.data
  return typeof data === 'object' && data !== null ? data as { status?: unknown; version?: unknown } : null
}

/** 探测后端 readiness；该端点公开且同时检查 API 依赖的数据库和缓存。 */
export async function checkBackendAvailability(force = false): Promise<boolean> {
  if (activeCheck) return activeCheck
  if (!force && currentAvailability !== null && Date.now() - lastCheckedAt < 4000) return currentAvailability
  activeCheck = (async () => {
    const controller = new AbortController()
    const timeout = window.setTimeout(() => controller.abort(), 5000)
    try {
      const response = await fetch(HEALTH_URL, { method: 'GET', cache: 'no-store', signal: controller.signal })
      const payload: unknown = response.ok ? await response.json() : null
      const healthData = readHealthData(payload)
      currentAvailability = response.ok && healthData?.status === 'ok'
      if (currentAvailability) backendHealthState.version = typeof healthData?.version === 'string' ? healthData.version : null
    } catch { currentAvailability = false }
    finally {
      window.clearTimeout(timeout)
      lastCheckedAt = Date.now()
      backendHealthState.available = currentAvailability
      backendHealthState.checkedAt = new Date(lastCheckedAt)
    }
    return currentAvailability === true
  })().finally(() => { activeCheck = null })
  return activeCheck
}

/** 保存同源返回路径，在维护页打开时不丢失用户原本想访问的页面。 */
function rememberReturnPath(path: string): void {
  if (path && path !== '/maintenance') sessionStorage.setItem(RETURN_PATH_KEY, path)
}

function returnPath(): string {
  const path = sessionStorage.getItem(RETURN_PATH_KEY)
  sessionStorage.removeItem(RETURN_PATH_KEY)
  return path?.startsWith('/') && !path.startsWith('//') && path !== '/maintenance' ? path : '/login'
}

/** 健康检查失败时转到维护说明；恢复时回到用户先前的页面。 */
async function navigateForAvailability(): Promise<void> {
  if (!routerRef) return
  const available = await checkBackendAvailability(true)
  // 检查期间可能发生路由切换，因此探测结束后再读取当前位置，避免保存过期返回地址。
  const current = routerRef.currentRoute.value
  if (!available && current.name !== 'maintenance') {
    rememberReturnPath(current.fullPath)
    await routerRef.replace({ name: 'maintenance' })
  } else if (available && current.name === 'maintenance') {
    await routerRef.replace(returnPath())
  }
}

/** 绑定页面载入、恢复显示和窗口焦点检查；连续系统事件复用同一探测请求。 */
export function installBackendHealthMonitor(router: Router): void {
  routerRef = router
  const wake = () => {
    if (wakeTimer) window.clearTimeout(wakeTimer)
    wakeTimer = window.setTimeout(() => { wakeTimer = 0; void navigateForAvailability() }, 160)
  }
  window.addEventListener('focus', wake)
  window.addEventListener('pageshow', wake)
  window.addEventListener('load', wake)
  document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible') wake() })
}

/** 维护页重试按钮调用后，如果服务恢复则由同一返回路径逻辑退出维护页。 */
export async function retryBackendAvailability(): Promise<boolean> {
  const available = await checkBackendAvailability(true)
  if (available && routerRef?.currentRoute.value.name === 'maintenance') await routerRef.replace(returnPath())
  return available
}
