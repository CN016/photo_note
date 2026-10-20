import { ApiError, type ApiEnvelope, type ApiErrorEnvelope, type AuthResponse, type ConsoleConfigRecord, type ConsoleFeedbackRecord, type ConsoleInspirationRecord, type ConsoleUserRecord, type InspirationImageMetadata, type InspirationRecord, type InspirationStatistics, type LedgerFields, type LedgerImportItem, type LedgerImportResult, type LedgerRecord, type NoticeConfig, type NoticeConfigList, type NoticeDelivery, type NotificationPage, type PageResult, type PlanFields, type PlanRecord, type ProjectRecord, type ProjectSaveInput, type ProjectSummary, type ScheduleBooking, type ScheduleBookingReceipt, type ScheduleFields, type ScheduleOccurrence, type SchedulePublicSlot, type ScheduleShare, type TagRecord, type TokenPair, type UserProfile } from './types'
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

/** 二进制文件流保持 Blob 类型、沿用统一的令牌恢复流程，并允许头像读取指定 HTTP 缓存策略。 */
async function requestBlob(path: string, retry = true, cache: RequestCache = 'default'): Promise<Blob> {
  const headers = new Headers()
  if (accessToken) headers.set('Authorization', `Bearer ${accessToken}`)
  let response: Response
  try { response = await fetch(`${API_BASE}${path}`, { headers, cache }) }
  catch { throw new ApiError('network', 'network', 0) }
  if (response.status === 401 && retry) {
    if (await rotateRefreshToken()) return requestBlob(path, false, cache)
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
  /** 每次打开计划表单时绕过缓存，读取管理员当前公开的 emoji 配置。 */
  publicConfigs(keys: string[]) {
    const params = new URLSearchParams({ keys: keys.join(',') })
    return request<{ items: Array<{ key: string; value: string }> }>(`/configs/public?${params.toString()}`, { cache: 'no-store' }, false)
  },
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
  /** 使用服务端分页契约加载灵感图，并把文件名、标签和排序条件交给数据库组合筛选。 */
  inspirations(page: number, pageSize = 20, options: { q?: string; tagIds?: string[]; tagMatch?: 'any' | 'all'; sort?: 'created_desc' | 'created_asc' | 'updated_desc' } = {}) {
    const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
    if (options.q?.trim()) params.set('q', options.q.trim())
    options.tagIds?.forEach(id => params.append('tag_ids', id))
    if (options.tagMatch) params.set('tag_match', options.tagMatch)
    if (options.sort) params.set('sort', options.sort)
    return request<PageResult<InspirationRecord>>(`/inspirations?${params.toString()}`)
  },
  /** 查询当前账号的拍摄参数分布；后台仅返回分类计数，不传输图片记录明细。 */
  inspirationStatistics() { return request<InspirationStatistics>('/inspirations/statistics') },
  /** 按当前用户和可选名称搜索分页标签；page_size 最大为服务端允许的 100 条。 */
  tags(page = 1, pageSize = 20, query = '') {
    const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
    if (query.trim()) params.set('q', query.trim())
    return request<PageResult<TagRecord>>(`/tags?${params.toString()}`)
  },
  /** 读取单张灵感图文档。 */
  inspiration(id: string) { return request<InspirationRecord>(`/inspirations/${encodeURIComponent(id)}`) },
  /** 上传原始图片字节；文件名和类型由浏览器 FormData 提交。 */
  async upload(file: Blob, filename: string): Promise<{ storekey: string }> {
    const body = new FormData()
    body.append('file', file, filename)
    return request<{ storekey: string }>('/files', { method: 'POST', body })
  },
  /** 带鉴权下载文件字节，不将 storekey 拼成公开 URL；可选 cache 用于显式绕过旧头像缓存。 */
  async download(storekey: string, options: { cache?: RequestCache } = {}): Promise<Blob> {
    return requestBlob(`/files/${encodeURIComponent(storekey)}`, true, options.cache)
  },
  /** 获取管理用户列表；角色在后端逐请求校验。 */
  consoleUsers(page: number, pageSize = 20, q = '') {
    const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
    if (q.trim()) params.set('q', q.trim())
    return request<PageResult<ConsoleUserRecord>>(`/atelier-7f3c9a2e/users?${params.toString()}`)
  },
  /** 设置一个目标账号是否具有工作台权限。 */
  consoleSetUserRole(id: string, isAdmin: boolean) {
    return request<{ business_id: string; is_admin: boolean }>(`/atelier-7f3c9a2e/users/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify({ is_admin: isAdmin }) })
  },
  /** 获取配置元数据；秘密值只返回是否已配置。 */
  consoleConfigs() { return request<{ items: ConsoleConfigRecord[] }>('/atelier-7f3c9a2e/configs') },
  /** 新增一项私有或公开配置；秘密项不能公开。 */
  consoleCreateConfig(input: { key: string; value: string; description: string; is_public: boolean; is_secret: boolean }) {
    return request<ConsoleConfigRecord>('/atelier-7f3c9a2e/configs', { method: 'POST', body: JSON.stringify(input) })
  },
  /** 编辑描述、公开状态或配置值；秘密值为空时沿用旧值。 */
  consoleUpdateConfig(key: string, patch: { value?: string; clear_value?: boolean; description?: string; is_public?: boolean }) {
    return request<ConsoleConfigRecord>(`/atelier-7f3c9a2e/configs/${encodeURIComponent(key)}`, { method: 'PATCH', body: JSON.stringify(patch) })
  },
  /** 查询全站灵感图片摘要，结果不包含任何私有编辑文档。 */
  consoleInspirations(page: number, pageSize = 20, q = '', sort: 'created_desc' | 'created_asc' | 'updated_desc' = 'created_desc') {
    const params = new URLSearchParams({ page: String(page), page_size: String(pageSize), sort })
    if (q.trim()) params.set('q', q.trim())
    return request<PageResult<ConsoleInspirationRecord>>(`/atelier-7f3c9a2e/inspirations?${params.toString()}`)
  },
  /** 按处理状态分页读取反馈。 */
  consoleFeedbacks(page: number, pageSize = 20, status: '' | 'new' | 'processed' = '') {
    const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
    if (status) params.set('status', status)
    return request<PageResult<ConsoleFeedbackRecord>>(`/atelier-7f3c9a2e/feedbacks?${params.toString()}`)
  },
  /** 标记反馈状态为新反馈或已处理。 */
  consoleUpdateFeedbackStatus(id: string, status: 'new' | 'processed') {
    return request<{ business_id: string; status: 'new' | 'processed' }>(`/atelier-7f3c9a2e/feedbacks/${encodeURIComponent(id)}/status`, { method: 'PATCH', body: JSON.stringify({ status }) })
  },
  /** 以管理员 multipart 接口提交回复正文、保留的现有图片和新图片文件。 */
  consoleReplyFeedback(id: string, replyContent: string, keptImageKeys: string[], images: File[]) {
    const body = new FormData()
    body.append('reply_content', replyContent)
    body.append('kept_image_keys', JSON.stringify(keptImageKeys))
    images.forEach((image) => body.append('images', image, image.name))
    return request<{ business_id: string; reply_content: string; reply_image_keys: string[] }>(`/atelier-7f3c9a2e/feedbacks/${encodeURIComponent(id)}/reply`, { method: 'PATCH', body })
  },
  /** 按日历日期范围读取正式账目，并恢复当前用户未完成的账本草稿。 */
  ledgerRecords(from: string, to: string) { return request<{ items: LedgerRecord[] }>(`/ledger/records?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`) },
  /** 打开新增记录时立即在服务端建立可恢复草稿。 */
  createLedgerDraft(fields: LedgerFields) { return request<LedgerRecord>('/ledger/records', { method: 'POST', body: JSON.stringify(fields) }) },
  /** 保存整笔草稿快照并用修订号阻止迟到请求覆盖新输入。 */
  saveLedgerDraft(id: string, revision: number, fields: LedgerFields) {
    return request<LedgerRecord>(`/ledger/records/${encodeURIComponent(id)}/draft`, { method: 'PATCH', body: JSON.stringify({ revision, ...fields }) })
  },
  /** 将有效草稿应用为正式账目。 */
  completeLedgerRecord(id: string, revision: number) {
    return request<LedgerRecord>(`/ledger/records/${encodeURIComponent(id)}/complete`, { method: 'POST', body: JSON.stringify({ revision }) })
  },
  /** 删除当前用户指定的账目或尚未完成的草稿。 */
  deleteLedgerRecord(id: string) { return request<void>(`/ledger/records/${encodeURIComponent(id)}`, { method: 'DELETE' }) },
  /** 原子合并已预览的有效导入条目。 */
  importLedgerRecords(records: LedgerImportItem[]) {
    return request<LedgerImportResult>('/ledger/import', { method: 'POST', body: JSON.stringify({ records }) })
  },
  /** 按页读取站内通知和相关 webhook 状态。 */
  notifications(page = 1, pageSize = 20, unreadOnly = false) {
    return request<NotificationPage>(`/notifications?page=${page}&page_size=${pageSize}&unread_only=${unreadOnly}`)
  },
  /** 读取消息中心导航角标。 */
  unreadNotificationCount() { return request<{ unread_count: number }>('/notifications/unread-count') },
  /** 把一条站内通知标为已读。 */
  markNotificationRead(id: string) { return request<{ read: boolean }>(`/notifications/${encodeURIComponent(id)}/read`, { method: 'PATCH' }) },
  /** 一次性标记所有站内通知已读。 */
  markAllNotificationsRead() { return request<{ updated: number }>('/notifications/read-all', { method: 'POST' }) },
  /** 查询 webhook 配置；服务端返回地址遮罩和当前数量上限。 */
  noticeConfigs() { return request<NoticeConfigList>('/notice-configs') },
  /** 新建用户自有 webhook；凭据只提交给服务端，不持久化到客户端存储。 */
  createNoticeConfig(input: { provider: NoticeConfig['provider']; name: string; webhook_url: string; signing_secret: string }) {
    return request<NoticeConfig>('/notice-configs', { method: 'POST', body: JSON.stringify(input) })
  },
  /** 修改配置时空 webhook_url 和 signing_secret 保留服务端已保存值。 */
  updateNoticeConfig(id: string, patch: { provider?: NoticeConfig['provider']; name?: string; webhook_url?: string; signing_secret?: string; clear_signing_secret?: boolean; enabled?: boolean }) {
    return request<NoticeConfig>(`/notice-configs/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(patch) })
  },
  /** 软删除当前用户的 webhook 配置。 */
  deleteNoticeConfig(id: string) { return request<void>(`/notice-configs/${encodeURIComponent(id)}`, { method: 'DELETE' }) },
  /** 测试消息先入队，响应不等待机器人平台。 */
  testNoticeConfig(id: string) { return request<NoticeDelivery>(`/notice-configs/${encodeURIComponent(id)}/test`, { method: 'POST' }) },
  /** 轮询异步测试或通知投递结果。 */
  noticeDelivery(id: string) { return request<NoticeDelivery>(`/notice-deliveries/${encodeURIComponent(id)}`) },
  /** 将最终失败的本人投递任务重置为待发送。 */
  retryNoticeDelivery(id: string) { return request<NoticeDelivery>(`/notice-deliveries/${encodeURIComponent(id)}/retry`, { method: 'POST' }) },
  /** 读取当前用户的日历发生项，日期区间按中国标准时间解释。 */
  scheduleItems(from: string, to: string) { return request<{ items: ScheduleOccurrence[] }>(`/schedule/items?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`) },
  /** 创建工作事项或开放预约时段。 */
  createScheduleItem(fields: ScheduleFields) { return request<ScheduleOccurrence>('/schedule/items', { method: 'POST', body: JSON.stringify(fields) }) },
  /** 更新整个重复系列。 */
  updateScheduleItem(id: string, fields: ScheduleFields) { return request<ScheduleOccurrence>(`/schedule/items/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(fields) }) },
  /** 删除整个档期系列。 */
  deleteScheduleItem(id: string) { return request<void>(`/schedule/items/${encodeURIComponent(id)}`, { method: 'DELETE' }) },
  /** 仅覆盖重复系列的一个日期。 */
  updateScheduleOccurrence(id: string, date: string, fields: ScheduleFields) { return request<{ updated: boolean }>(`/schedule/items/${encodeURIComponent(id)}/occurrences/${encodeURIComponent(date)}`, { method: 'PATCH', body: JSON.stringify({ ...fields, recurrence: 'none', recurrence_end: '' }) }) },
  /** 仅取消重复系列的一个日期。 */
  cancelScheduleOccurrence(id: string, date: string) { return request<{ cancelled: boolean }>(`/schedule/items/${encodeURIComponent(id)}/occurrences/${encodeURIComponent(date)}`, { method: 'DELETE' }) },
  /** 读取当前用户的预约申请队列。 */
  scheduleBookings(status = 'pending') { return request<{ items: ScheduleBooking[] }>(`/schedule/bookings?status=${encodeURIComponent(status)}`) },
  /** 确认或拒绝访客申请。 */
  decideScheduleBooking(id: string, decision: 'confirm' | 'reject') { return request<{ status: string }>(`/schedule/bookings/${encodeURIComponent(id)}/${decision}`, { method: 'POST' }) },
  /** 取消待确认或已确认申请，并释放一个预约名额。 */
  cancelScheduleBooking(id: string) { return request<{ status: string }>(`/schedule/bookings/${encodeURIComponent(id)}/cancel`, { method: 'POST' }) },
  /** 查询当前预约分享链接是否启用。 */
  scheduleShare() { return request<ScheduleShare>('/schedule/share') },
  /** 生成新随机分享地址并使旧链接失效。 */
  createScheduleShare() { return request<ScheduleShare>('/schedule/share', { method: 'POST' }) },
  /** 撤销公开预约分享链接。 */
  revokeScheduleShare() { return request<{ enabled: boolean }>('/schedule/share', { method: 'DELETE' }) },
  /** 游客读取分享用户的开放时段，接口不携带登录令牌。 */
  publicScheduleSlots(token: string, from: string, to: string) { return request<{ items: SchedulePublicSlot[] }>(`/book/${encodeURIComponent(token)}/slots?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`, {}, false) },
  /** 按用户隔离读取计划和当前提醒配置。 */
  plans() { return request<{ items: PlanRecord[] }>('/plans') },
  /** 读取一条本人计划供通知跳转或编辑页面使用。 */
  plan(id: string) { return request<PlanRecord>(`/plans/${encodeURIComponent(id)}`) },
  /** 创建计划；未填写提醒时由服务端设置提前一小时。 */
  createPlan(fields: PlanFields) { return request<PlanRecord>('/plans', { method: 'POST', body: JSON.stringify(fields) }) },
  /** 修改计划字段；未提供 reminder_offsets 时保留当前提醒。 */
  updatePlan(id: string, patch: Partial<PlanFields>) { return request<PlanRecord>(`/plans/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(patch) }) },
  /** 将计划标为完成并取消未发送提醒。 */
  completePlan(id: string) { return request<PlanRecord>(`/plans/${encodeURIComponent(id)}/complete`, { method: 'POST' }) },
  /** 删除计划并取消未发送提醒。 */
  deletePlan(id: string) { return request<void>(`/plans/${encodeURIComponent(id)}`, { method: 'DELETE' }) },
  /** 分页搜索当前用户的项目标题与正文。 */
  projects(page = 1, pageSize = 20, query = '') {
    const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
    if (query.trim()) params.set('q', query.trim())
    return request<PageResult<ProjectSummary>>(`/projects?${params.toString()}`)
  },
  /** 读取项目正文并校验所有资源引用。 */
  project(id: string) { return request<ProjectRecord>(`/projects/${encodeURIComponent(id)}`) },
  /** 创建带初始内容的项目文档。 */
  createProject(input: ProjectSaveInput) { return request<ProjectRecord>('/projects', { method: 'POST', body: JSON.stringify(input) }) },
  /** 保存项目完整快照，revision 防止较旧的自动保存覆盖较新编辑。 */
  saveProject(id: string, input: ProjectSaveInput) { return request<ProjectRecord>(`/projects/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(input) }) },
  /** 软删除项目并保留其灵感图和附件引用关系。 */
  deleteProject(id: string) { return request<void>(`/projects/${encodeURIComponent(id)}`, { method: 'DELETE' }) },
  /** 通过专用入口压缩项目封面并登记为项目资源。 */
  async uploadProjectCover(file: File): Promise<{ storekey: string; file_name: string; file_size: number; content_type: string }> {
    const body = new FormData()
    body.append('file', file, file.name)
    return request('/projects/cover-upload', { method: 'POST', body })
  },
  /** 通过项目和已登记 storekey 读取附件；新文件保存引用前的短暂 404 会按界限重试。 */
  async projectAttachmentContent(projectID: string, storekey: string) {
    const path = `/projects/${encodeURIComponent(projectID)}/attachments/content?storekey=${encodeURIComponent(storekey)}`
    const retryDelays = [150, 300, 600, 900]
    for (let attempt = 0; ; attempt += 1) {
      try { return await requestBlob(path) }
      catch (error) {
        if (!(error instanceof ApiError) || error.status !== 404 || attempt >= retryDelays.length) throw error
        // 上传先完成、项目附件关联后保存；只对这个窗口期的 404 做短暂重试。
        await new Promise(resolve => window.setTimeout(resolve, retryDelays[attempt]))
      }
    }
  },
  /** 游客提交姓名、电话、备注和具体开放发生项。 */
  requestScheduleBooking(token: string, input: { schedule_id: string; start_at: string; name: string; phone: string; note: string }) { return request<ScheduleBookingReceipt>(`/book/${encodeURIComponent(token)}/requests`, { method: 'POST', body: JSON.stringify(input) }, false) },
  /** 创建灵感图；image_key 指向服务端可能压缩的工作副本。 */
  createInspiration(imageKey: string, filename: string, data: Record<string, unknown>, imageMetadata: InspirationImageMetadata) {
    return request<InspirationRecord>('/inspirations', { method: 'POST', body: JSON.stringify({ image_key: imageKey, file_name: filename, data, image_metadata: imageMetadata }) })
  },
  /** 已有灵感图只更新 data，不触发后端对源文件的原位压缩。 */
  updateInspiration(id: string, data: Record<string, unknown>, imageMetadata?: InspirationImageMetadata) {
    const body = imageMetadata ? { data, image_metadata: imageMetadata } : { data }
    return request<InspirationRecord>(`/inspirations/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(body) })
  },
  /** 只提交标签名称并立即替换该灵感图的标签集合；标签 UUID 由服务端解析。 */
  updateInspirationTags(id: string, tagNames: string[]) {
    return request<InspirationRecord>(`/inspirations/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify({ tag_names: tagNames }) })
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
