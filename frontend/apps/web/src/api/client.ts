import { ApiError, type ApiEnvelope, type ApiErrorEnvelope, type AuthResponse, type InspirationRecord, type PageResult, type TokenPair, type UserProfile } from './types'
import { API_REQUEST_ORIGIN } from './config'

/** V1 业务路由统一加版本前缀；环境变量只提供服务主机。 */
const API_BASE = `${API_REQUEST_ORIGIN}/api/v1`
const REFRESH_KEY = 'photo-nini.refresh-token'
const LOCK_KEY = 'photo-nini.refresh-lock'
const refreshChannel = typeof BroadcastChannel !== 'undefined' ? new BroadcastChannel('photo-nini-auth') : null
let accessToken: string | null = null
let refreshFlight: Promise<string | null> | null = null
let authLossHandler: (() => void) | null = null

/** Access Token 只驻留在当前页面内存；Refresh Token 才写入本地持久存储。 */
export function setAccessToken(token: string | null): void { accessToken = token }
export function getRefreshToken(): string | null { return localStorage.getItem(REFRESH_KEY) }
export function setAuthLossHandler(handler: (() => void) | null): void { authLossHandler = handler }

refreshChannel?.addEventListener('message', (event: MessageEvent<{ type?: string; accessToken?: string }>) => {
  if (event.data?.type === 'rotated' && typeof event.data.accessToken === 'string') accessToken = event.data.accessToken
  if (event.data?.type === 'logged-out') { accessToken = null; localStorage.removeItem(REFRESH_KEY) }
})

window.addEventListener('storage', event => {
  if (event.key === REFRESH_KEY && event.newValue === null) {
    accessToken = null
    authLossHandler?.()
  }
})

/** 将后端错误码转换为可由页面翻译资源处理的 ApiError。 */
async function parseFailure(response: Response): Promise<ApiError> {
  let body: ApiErrorEnvelope | undefined
  try { body = await response.json() as ApiErrorEnvelope } catch { /* 非 JSON 错误仍以 HTTP 状态表达。 */ }
  return new ApiError(body?.error?.code ?? `http_${response.status}`, body?.error?.message ?? response.statusText, response.status, body?.error?.details)
}

/** 网络错误保留 Refresh Token，只有服务端明确返回未授权才会清除会话。 */
async function exchangeRefreshToken(refreshToken: string): Promise<Response> {
  try {
    return await fetch(`${API_BASE}/auth/refresh`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ refresh_token: refreshToken }),
    })
  } catch { throw new ApiError('network', 'network', 0) }
}

/** 通过 Web Locks 串行刷新；不支持该 API 的浏览器使用短租约并在失败时等待另一标签页轮换完成。 */
async function withRefreshLock<T>(action: () => Promise<T>): Promise<T> {
  if ('locks' in navigator && navigator.locks) return navigator.locks.request('photo-nini-refresh', { mode: 'exclusive' }, action)
  const owner = `${Date.now()}-${Math.random().toString(36).slice(2)}`
  const deadline = Date.now() + 8000
  while (Date.now() < deadline) {
    const current = localStorage.getItem(LOCK_KEY)
    if (!current || Number(current.split(':')[1]) < Date.now()) {
      localStorage.setItem(LOCK_KEY, `${owner}:${Date.now() + 5000}`)
      if (localStorage.getItem(LOCK_KEY)?.startsWith(`${owner}:`)) {
        try { return await action() } finally { if (localStorage.getItem(LOCK_KEY)?.startsWith(`${owner}:`)) localStorage.removeItem(LOCK_KEY) }
      }
    }
    await new Promise(resolve => window.setTimeout(resolve, 100))
  }
  throw new Error('refresh_lock_timeout')
}

/** Refresh Token 单次消费；失败的并发标签页等待已获胜页面持久化的新 token。 */
async function rotateRefreshToken(): Promise<string | null> {
  if (refreshFlight) return refreshFlight
  refreshFlight = withRefreshLock(async () => {
    let token = getRefreshToken()
    if (!token) return null
    for (let attempt = 0; attempt < 4; attempt += 1) {
      const response = await exchangeRefreshToken(token)
      if (response.ok) {
        const envelope = await response.json() as ApiEnvelope<{ tokens: TokenPair }>
        localStorage.setItem(REFRESH_KEY, envelope.data.tokens.refresh_token)
        accessToken = envelope.data.tokens.access_token
        refreshChannel?.postMessage({ type: 'rotated', accessToken })
        return accessToken
      }
      if (response.status !== 401) throw await parseFailure(response)
      let replacement = getRefreshToken()
      if (replacement === token && !navigator.locks) replacement = await waitForRefreshReplacement(token)
      if (!replacement || replacement === token) break
      token = replacement
    }
    localStorage.removeItem(REFRESH_KEY)
    accessToken = null
    authLossHandler?.()
    return null
  }).finally(() => { refreshFlight = null })
  return refreshFlight
}

/** 不支持 Web Locks 的旧浏览器等待竞争标签页将新 Refresh Token 写入本机。 */
async function waitForRefreshReplacement(previousToken: string): Promise<string | null> {
  const deadline = Date.now() + 1600
  while (Date.now() < deadline) {
    await new Promise(resolve => window.setTimeout(resolve, 100))
    const current = getRefreshToken()
    if (current && current !== previousToken) return current
  }
  return getRefreshToken()
}

/** 统一处理 JSON 封套、Bearer 鉴权以及过期 Access Token 的单次重试。 */
async function request<T>(path: string, init: RequestInit = {}, auth = true, retry = true, apiBase = API_BASE): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  if (auth && accessToken) headers.set('Authorization', `Bearer ${accessToken}`)
  let response: Response
  try { response = await fetch(`${apiBase}${path}`, { ...init, headers }) }
  catch { throw new ApiError('network', 'network', 0) }
  if (response.status === 401 && auth && retry) {
    const nextToken = await rotateRefreshToken()
    if (nextToken) return request<T>(path, init, auth, false, apiBase)
    throw new ApiError('unauthorized', 'unauthorized', 401)
  }
  if (!response.ok) throw await parseFailure(response)
  if (response.status === 204) return undefined as T
  const envelope = await response.json() as ApiEnvelope<T>
  return envelope.data
}

/** 二进制文件流保持 Blob 类型，并沿用统一的过期令牌恢复流程。 */
async function requestBlob(path: string, retry = true): Promise<Blob> {
  const headers = new Headers()
  if (accessToken) headers.set('Authorization', `Bearer ${accessToken}`)
  let response: Response
  try { response = await fetch(`${API_BASE}${path}`, { headers }) }
  catch { throw new ApiError('network', 'network', 0) }
  if (response.status === 401 && retry) {
    if (await rotateRefreshToken()) return requestBlob(path, false)
    throw new ApiError('unauthorized', 'unauthorized', 401)
  }
  if (!response.ok) throw await parseFailure(response)
  return response.blob()
}

export const api = {
  /** 登录后端当前手机号密码认证入口。 */
  login(phone: string, password: string) { return request<AuthResponse>('/auth/login', { method: 'POST', body: JSON.stringify({ phone, password }) }, false) },
  /** 注册必须上传头像；multipart 请求由服务端验证文件内容后创建账号。 */
  register(phone: string, password: string, nickname: string, avatar: Blob, filename: string) {
    const body = new FormData()
    body.append('phone', phone)
    body.append('password', password)
    body.append('nickname', nickname)
    body.append('avatar', avatar, filename)
    return request<AuthResponse>('/auth/register', { method: 'POST', body }, false, true, `${API_REQUEST_ORIGIN}/api/v2`)
  },
  /** 当前 refresh token 轮换结果仅含新 token pair。 */
  refresh() { return rotateRefreshToken() },
  /** 撤销当前服务端会话。 */
  logout() { return request<void>('/auth/logout', { method: 'POST' }) },
  /** 读取当前登录用户资料。 */
  currentUser() { return request<UserProfile>('/users/me') },
  /** 只提交资料中发生变化的字段。 */
  updateProfile(patch: { nickname?: string; avatar_key?: string }) { return request<UserProfile>('/users/me', { method: 'PATCH', body: JSON.stringify(patch) }) },
  /** 更新 Argon2id 凭据；服务端保留当前会话并撤销同一用户其他设备。 */
  changePassword(currentPassword: string, newPassword: string) {
    return request<void>('/users/me/password', { method: 'POST', body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }) })
  },
  /** 游客反馈无需登录；已有 bearer token 时由服务端关联提交用户。 */
  feedback(content: string, images: Blob[], filenames: string[]) {
    const body = new FormData()
    body.append('content', content)
    images.forEach((image, index) => body.append('images', image, filenames[index] || `feedback-${index + 1}.jpg`))
    return request<void>('/feedback', { method: 'POST', body }, Boolean(accessToken))
  },
  /** 使用服务端分页契约加载灵感图。 */
  inspirations(page: number, pageSize = 20) { return request<PageResult<InspirationRecord>>(`/inspirations?page=${page}&page_size=${pageSize}`) },
  /** 读取单张灵感图文档。 */
  inspiration(id: string) { return request<InspirationRecord>(`/inspirations/${encodeURIComponent(id)}`) },
  /** 上传原始图片字节；文件名和类型由浏览器 FormData 提交。 */
  async upload(file: Blob, filename: string): Promise<{ storekey: string }> {
    const body = new FormData()
    body.append('file', file, filename)
    return request<{ storekey: string }>('/files', { method: 'POST', body })
  },
  /** 带鉴权下载原始图片字节，不将 storekey 拼接为公开可访问 URL。 */
  async download(storekey: string): Promise<Blob> {
    return requestBlob(`/files/${encodeURIComponent(storekey)}`)
  },
  /** 创建灵感图；image_key 指向服务端可能压缩的工作副本。 */
  createInspiration(imageKey: string, filename: string, data: Record<string, unknown>) {
    return request<InspirationRecord>('/inspirations', { method: 'POST', body: JSON.stringify({ image_key: imageKey, file_name: filename, data }) })
  },
  /** 已有灵感图只更新 data，不触发后端对源文件的原位压缩。 */
  updateInspiration(id: string, data: Record<string, unknown>) {
    return request<InspirationRecord>(`/inspirations/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify({ data }) })
  },
}

/** 初始化流程中写入登录签发的 Refresh Token，并只把 Access Token 留在内存。 */
export function acceptTokenPair(tokens: TokenPair): void {
  accessToken = tokens.access_token
  localStorage.setItem(REFRESH_KEY, tokens.refresh_token)
  refreshChannel?.postMessage({ type: 'rotated', accessToken })
}

/** 退出或鉴权彻底失效时清空当前页与同源标签页的登录态。 */
export function clearTokens(): void {
  accessToken = null
  localStorage.removeItem(REFRESH_KEY)
  refreshChannel?.postMessage({ type: 'logged-out' })
}
