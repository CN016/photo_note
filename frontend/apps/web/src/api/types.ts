/** 后端统一成功封套；业务数据仅放在 data 字段中。 */
export interface ApiEnvelope<T> {
  data: T
  request_id: string
}

/** 后端统一错误封套；details 只用于展示字段级错误，不记录敏感数据。 */
export interface ApiErrorEnvelope {
  error: { code: string; message: string; details?: Record<string, unknown> | null }
  request_id?: string
}

export interface UserProfile {
  business_id: string
  nickname: string
  phone: string
  avatar_key: string | null
  created_at: string
  updated_at: string
}

export interface TokenPair {
  access_token: string
  refresh_token: string
  token_type: 'Bearer'
  expires_in: number
  refresh_expires_in: number
}

export interface AuthResponse {
  user: UserProfile
  tokens: TokenPair
}

export interface InspirationRecord {
  business_id: string
  tag_ids: string[]
  image_key: string
  file_name: string
  file_hash: string | null
  /** 后端压缩后编辑副本的实际字节数；旧服务或历史记录可能未返回。 */
  file_size?: number | null
  data: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface PageResult<T> {
  items: T[]
  page: number
  page_size: number
  total: number
}

/** 账本草稿和正式记录共用的完整编辑快照；金额以两位小数字符串传输。 */
export interface LedgerFields {
  date: string
  time: string
  type: 'income' | 'expense'
  amount: string
  category: string
  payment_method: string
  flag: '' | 'important' | 'anomaly' | 'pending_check'
  note: string
}

/** 当前登录用户的收支记录；draft 仅在编辑中时存在，正式字段在完成前保持不变。 */
export interface LedgerRecord extends LedgerFields {
  business_id: string
  status: 'draft' | 'complete'
  draft: LedgerFields | null
  draft_version: number
  created_at: string
  updated_at: string
}

/** 导入支持导出文件中的业务 UUID；旧营收日历文件没有该字段时由服务端分配新 UUID。 */
export interface LedgerImportItem extends LedgerFields {
  business_id?: string
}

export interface LedgerImportResult {
  created: number
  updated: number
}

/** 档期主记录字段；重复规则日期使用中国标准时间语义。 */
export interface ScheduleFields {
  kind: 'work' | 'availability'
  title: string
  content: string
  start_at: string
  end_at: string
  recurrence: 'none' | 'daily' | 'weekly' | 'monthly'
  weekdays: number[]
  recurrence_end: string
  capacity: number
}

/** 服务端展开后的单次日历发生项，不含用户内部标识。 */
export interface ScheduleOccurrence {
  business_id: string
  kind: 'work' | 'availability'
  title: string
  content: string
  start_at: string
  end_at: string
  capacity: number
  recurrence: string
  weekdays: number[]
  recurrence_end: string
  series_start_at: string
  series_end_at: string
  series_date: string
}

/** 分享页公开开放时段只含预约必需信息和剩余名额。 */
export interface SchedulePublicSlot extends ScheduleOccurrence {
  remaining: number
}

/** 仅拥有者可见的客户预约申请。 */
export interface ScheduleBooking {
  business_id: string
  schedule_id: string
  start_at: string
  end_at: string
  guest_name: string
  guest_phone: string
  note: string
  status: 'pending' | 'confirmed' | 'rejected' | 'expired' | 'cancelled'
  request_code: string
  expires_at: string | null
  created_at: string
}

export interface ScheduleShare {
  enabled: boolean
  url?: string
}

export interface ScheduleBookingReceipt {
  request_code: string
  status: 'pending'
  expires_at: string | null
}

/** 可按接口 code 选择本地化提示的请求错误类型。 */
export class ApiError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly status: number,
    public readonly details?: Record<string, unknown> | null,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}
