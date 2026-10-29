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
  extra: { statistics_chart_modes?: Record<string, string>; [key: string]: unknown }
  is_admin: boolean
  orgid: number
  organization_role: 'personal' | 'admin' | 'member'
  created_at: string
  updated_at: string
}

/** 当前账号可维护的灵感标签，只含业务 UUID 和展示名称。 */
export interface TagRecord {
  business_id: string
  content: string
  created_at: string
  updated_at: string
}

export interface TokenPair {
  access_token: string
  refresh_token: string
  token_type: 'Bearer'
  expires_in: number
  refresh_expires_in: number
  orgid: number
  session_id: string
  session_version: number
}

/** 空间列表只包含当前账号可进入的个人空间和有效组织。 */
export interface Environment {
  orgid: number
  business_id: string | null
  name: string
  role: 'personal' | 'admin' | 'member'
  status: 'active' | 'disabled'
}

/** 组织编号是独立业务编号，数据库内部自增 ID 不出现在接口中。 */
export interface Organization {
  business_id: string
  orgid: number
  name: string
  status: 'active' | 'disabled'
  created_at: string
  updated_at: string
}

export interface OrganizationMember {
  business_id: string
  user_id: string
  nickname: string
  role: 'admin' | 'member'
  status: 'active' | 'removed'
}

export interface AuthResponse {
  user: UserProfile
  tokens: TokenPair
}

export interface InspirationRecord {
  people_count?: number | null
  is_empty_shot?: boolean | null
  dominant_color?: string | null
  dominant_hue?: number | null
  color_bucket?: string | null
  analysis_status?: string
  analyzed_at?: string | null
  business_id: string
  tag_ids: string[]
  /** 对应 tag_ids 的展示名称；旧服务端响应可能暂时不包含该字段。 */
  tag_names?: string[]
  image_key: string
  file_name: string
  file_hash: string | null
  /** 后端压缩后编辑副本的实际字节数；旧服务或历史记录可能未返回。 */
  file_size?: number | null
  /** 图片属性仅在创建、详情和更新响应中返回；列表为减小负载会省略。 */
  image_metadata?: InspirationImageMetadata
  data: Record<string, unknown>
  created_at: string
  updated_at: string
}

/** 图库扩展筛选不包含资源归属；空间身份统一由请求客户端附加。 */
export interface InspirationLibraryFilters { date_from?: string; date_to?: string; project?: string; people?: string; empty_shot?: string; color?: string; status?: string }
/** 每日数量是成功创建的历史计数，不能以当前列表条数替代。 */
export interface InspirationDay { day: string; saved_count: number; target: number | null }
export interface InspirationGoalStatus { goal: { enabled: boolean; target: number; reminder_time: string }; today: InspirationDay; days?: InspirationDay[]; reminder_pending: boolean }
export interface InspirationFacet { key: string; name?: string; count: number; previews?: { business_id: string; image_key: string; file_name: string; file_hash?: string | null }[] }

/** 从原图读取的标准属性；extra 保留带命名空间且通过大小限制的其他标签。 */
export interface InspirationImageMetadata {
  image_id?: string
  source_file_size?: number
  source_last_modified_at?: string
  image_width?: number
  image_height?: number
  horizontal_resolution_dpi?: number
  vertical_resolution_dpi?: number
  bit_depth?: number
  color_space?: string
  compression?: string
  title?: string
  subject?: string
  rating?: number
  tags?: string[]
  author?: string
  comments?: string
  copyright?: string
  software?: string
  date_acquired?: string
  date_time_original?: string
  date_time_digitized?: string
  camera_make?: string
  camera_model?: string
  camera_serial_number?: string
  lens_make?: string
  lens_model?: string
  lens_serial_number?: string
  flash_make?: string
  flash_model?: string
  flash_serial_number?: string
  aperture_f_number?: number
  maximum_aperture_f_number?: number
  exposure_time_seconds?: number
  iso_speed?: number
  exposure_compensation_ev?: number
  exposure_program?: string
  flash_fired?: boolean
  flash_mode?: string
  flash_energy_bcps?: number
  focal_length_mm?: number
  focal_length_35mm_mm?: number
  subject_distance_m?: number
  metering_mode?: string
  light_source?: string
  white_balance?: string
  contrast?: string
  brightness_ev?: number
  saturation?: string
  sharpness?: string
  digital_zoom_ratio?: number
  compression_bits_per_pixel?: number
  exif_version?: string
  gps_latitude?: number
  gps_longitude?: number
  gps_altitude_m?: number
  gps_timestamp?: string
  extra: Record<string, unknown>
}

/** 当前账号的灵感图拍摄属性汇总；不含位置、原图地址或设备序列号。 */
export interface InspirationStatisticBucket {
  value: string
  count: number
}

export interface InspirationStatistics {
  total_images: number
  images_with_shooting_metadata: number
  collection_months: InspirationStatisticBucket[]
  camera_models: InspirationStatisticBucket[]
  lens_models: InspirationStatisticBucket[]
  focal_lengths: InspirationStatisticBucket[]
  apertures: InspirationStatisticBucket[]
  iso_speeds: InspirationStatisticBucket[]
  exposure_times: InspirationStatisticBucket[]
  flash_usage: InspirationStatisticBucket[]
  aspect_ratios: InspirationStatisticBucket[]
  megapixels: InspirationStatisticBucket[]
  color_spaces: InspirationStatisticBucket[]
}

export interface PageResult<T> {
  items: T[]
  page: number
  page_size: number
  total: number
}

/** 管理工作台中的用户最小资料与权限标记。 */
export interface ConsoleUserRecord {
  business_id: string
  nickname: string
  phone: string
  is_admin: boolean
  created_at: string
}

/** 全站灵感图只读摘要，不包含用户私有编辑文档 data。 */
export interface ConsoleInspirationRecord {
  business_id: string
  owner_nickname: string
  tag_ids: string[]
  image_key: string
  file_name: string
  created_at: string
  updated_at: string
}

/** 反馈中心允许查看正文、附件、管理员回复并更新处理状态。 */
export interface ConsoleFeedbackRecord {
  business_id: string
  nickname: string
  content: string
  image_keys: string[]
  reply_content: string
  reply_image_keys: string[]
  status: 'new' | 'processed'
  created_at: string
  updated_at: string
}

/** 管理配置中，秘密原值不返回，仅可读取是否已设置。 */
export interface ConsoleConfigRecord {
  business_id: string
  key: string
  description: string
  value?: string
  is_public: boolean
  is_secret: boolean
  is_configured: boolean
  updated_at: string
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

/** 计划创建和更新使用的业务字段；starts_at 为含时区的 RFC3339 时间戳。 */
export interface PlanFields {
  title: string
  description: string
  starts_at: string
  cover_emoji: string
  cover_image_key: string
  reminder_offsets: number[]
}

/** 当前用户可见的一次性计划，不包含用户 UUID、数据库主键或提醒任务标识。 */
export interface PlanRecord extends PlanFields {
  business_id: string
  status: 'active' | 'completed'
  completed_at: string | null
  created_at: string
  updated_at: string
}

/** 项目是独立文档资源，正文使用版本化 ProseMirror JSON，附件由正文节点引用。 */
export interface ProjectRecord {
  /** 默认阅读模式，仅为界面偏好，不影响访问权限。 */
  default_read_only: boolean
  business_id: string
  title: string
  cover_emoji: string
  cover_image_key: string
  cover_file_name: string
  document_schema_version: number
  document: Record<string, unknown>
  revision: number
  created_at: string
  updated_at: string
}

/** 项目列表只返回卡片元数据，不包含完整正文 JSON。 */
export type ProjectSummary = Omit<ProjectRecord, 'document' | 'document_schema_version'>

/** 项目自动保存提交完整快照，并携带当前修订号执行乐观并发控制。 */
export interface ProjectSaveInput {
  default_read_only?: boolean
  title: string
  cover_emoji: string
  cover_image_key: string
  cover_file_name: string
  document_schema_version: number
  document: Record<string, unknown>
  revision: number
}

/** webhook 单渠道的异步投递状态；错误码不含远端响应正文或连接地址。 */
export interface NoticeDelivery {
  business_id: string
  provider: 'wecom' | 'dingtalk' | 'feishu'
  config_name: string
  part_index: number
  part_count: number
  status: 'pending' | 'sending' | 'retry' | 'delivered' | 'failed' | 'cancelled'
  attempt_count: number
  last_error_code: string
  can_retry: boolean
  updated_at: string
}

/** 消息中心只暴露关联业务 UUID、翻译键所需事件类型和每渠道结果。 */
export interface NoticeMessage {
  business_id: string
  event_type: string
  resource_type: string
  resource_business_id: string | null
  payload: {
    day?: string
    saved_count?: number
    target?: number
    result?: string
    async_version?: number
    message?: string
    error_code?: string
    source_file?: string
    source_line?: number
    text?: string
    markdown?: string
    image_urls?: string[]
    title?: string
    starts_at?: string
    reporter?: {
      nickname: string
      phone?: string
      guest?: boolean
    }
  }
  read_at: string | null
  created_at: string
  deliveries: NoticeDelivery[]
}

export interface NotificationPage {
  items: NoticeMessage[]
  page: number
  page_size: number
  total: number
  unread_count: number
}

/** webhook 地址和签名不回显，只提供脱敏地址与是否已设置签名。 */
export interface NoticeConfig {
  business_id: string
  provider: 'wecom' | 'dingtalk' | 'feishu'
  name: string
  webhook_masked: string
  secret_configured: boolean
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface NoticeConfigList {
  items: NoticeConfig[]
  maximum: number
  count: number
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

/** 独立作品集及公开作品照片的稳定接口字段。 */
export interface PortfolioRecord { business_id: string; title: string; description: string; tag_ids: string[]; share_enabled: boolean; cover_image_key?: string }
export interface PortfolioPhoto { business_id: string; image_key: string; title: string; tag_ids: string[]; sort_order: number; image_metadata: InspirationImageMetadata }
export interface PortfolioInput { title: string; description: string; tag_ids: string[]; project_ids: string[] }
