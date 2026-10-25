import type { LedgerFields, LedgerImportItem, LedgerRecord } from '../api/types'

const businessIdPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i

/** 将日历日期生成 YYYY-MM-DD，完全使用本地年月日而不经过 UTC 偏移。 */
export function toDateKey(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

/** 按日历日期构造本地 Date，避免 ISO UTC 解析在西半球偏到前一天。 */
export function fromDateKey(value: string): Date {
  const [year, month, day] = value.split('-').map(Number)
  return new Date(year, month - 1, day)
}

/** 将年月固定格式化为页面选择器和 API 查询可复用的 YYYY-MM。 */
export function toMonthKey(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}`
}

/** 严格解析非负十进制金额为整数分；不经过二进制浮点计算。 */
export function amountToFen(value: string | number): number | null {
  const text = String(value).trim()
  if (!/^\d{1,12}(?:\.\d{1,2})?$/.test(text)) return null
  const [wholeText, fractionText = ''] = text.split('.')
  const whole = Number(wholeText)
  const fraction = Number((fractionText + '00').slice(0, 2))
  const fen = whole * 100 + fraction
  return Number.isSafeInteger(fen) ? fen : null
}

/** 按当前语言和人民币单位格式化整数分，仅在显示层转换为浮点数。 */
export function formatFen(fen: number, locale: string): string {
  return new Intl.NumberFormat(locale, { style: 'currency', currency: 'CNY', minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(fen / 100)
}

/** 返回用于编辑器的当前草稿字段；无待编辑草稿时读取正式记录字段。 */
export function editableFields(record: LedgerRecord): LedgerFields {
  if (record.status === 'draft' && record.draft) return { ...record.draft }
  if (record.draft) return { ...record.draft }
  return {
    date: record.date,
    time: record.time,
    type: record.type,
    amount: record.amount,
    category: record.category,
    payment_method: record.payment_method,
    flag: record.flag,
    note: record.note,
  }
}

/** 统计和筛选始终只读取已完成的正式字段，不受待应用草稿影响。 */
export function formalFields(record: LedgerRecord): LedgerFields {
  return {
    date: record.date,
    time: record.time,
    type: record.type,
    amount: record.amount,
    category: record.category,
    payment_method: record.payment_method,
    flag: record.flag,
    note: record.note,
  }
}

/** 统计新建草稿和正式记录上的待应用编辑草稿数量。 */
export function hasPendingDraft(record: LedgerRecord): boolean {
  return record.status === 'draft' || record.draft !== null
}

/** 导入解析结果区分完整条目和无法安全写入的原始行。 */
export interface LedgerParseResult {
  records: LedgerImportItem[]
  invalid: string[]
}

/** 兼容本应用导出记录数组、旧营收 JSON 日期对象与旧版单日数字结构。 */
export function parseLedgerJSON(text: string): LedgerParseResult {
  const root: unknown = JSON.parse(text)
  const values: Array<{ item: Record<string, unknown>; date?: string }> = []
  if (Array.isArray(root)) {
    for (const item of root) if (isObject(item)) values.push({ item })
  } else if (isObject(root) && Array.isArray(root.records)) {
    for (const item of root.records) if (isObject(item)) values.push({ item })
  } else if (isObject(root)) {
    for (const [date, raw] of Object.entries(root)) {
      if (date === 'formatVersion' || date === 'currency' || date === 'exported_at') continue
      if (Array.isArray(raw)) {
        for (const item of raw) if (isObject(item)) values.push({ item, date })
      } else if (typeof raw === 'number' || typeof raw === 'string') {
        values.push({ item: { amount: raw, category: '未分类' }, date })
      } else if (isObject(raw)) {
        values.push({ item: raw, date })
      }
    }
  } else {
    throw new Error('unsupported_json')
  }
  return normalizeImportRows(values)
}

/** 读取带引号、逗号和换行的 CSV 单元格，兼容旧版 id/method 表头。 */
export function parseLedgerCSV(text: string): LedgerParseResult {
  const rows = parseCsvRows(text.replace(/^\uFEFF/, ''))
  if (rows.length < 2) return { records: [], invalid: [] }
  const headers = rows[0].map(header => canonicalHeader(header.trim().toLowerCase()))
  const values = rows.slice(1).filter(row => row.some(cell => cell.trim() !== '')).map(row => {
    const item: Record<string, unknown> = {}
    headers.forEach((header, index) => { item[header] = row[index] ?? '' })
    return { item }
  })
  return normalizeImportRows(values)
}

/** 将旧字段别名、中文支付方式和旧 JSON 金额转为当前 API 输入结构。 */
function normalizeImportRows(values: Array<{ item: Record<string, unknown>; date?: string }>): LedgerParseResult {
  const records: LedgerImportItem[] = []
  const invalid: string[] = []
  values.forEach(({ item, date: parentDate }, index) => {
    if (item.status === 'draft' || item.record_status === 'draft') return
    const recordDate = stringValue(item.date || item.record_date || item['日期'] || parentDate)
    const rawAmount = item.amount ?? (typeof item.amount_fen === 'number' ? item.amount_fen / 100 : undefined)
    const amount = normalizeAmount(rawAmount)
    const paymentMethod = canonicalMethod(stringValue(item.payment_method || item.method || item['支付方式'] || item['收款方式'] || item['方式']))
    const fields: LedgerFields = {
      date: recordDate,
      time: stringValue(item.time || item.record_time || item['时间']),
      type: normalizeType(stringValue(item.type || item.record_type || item['类型'] || item['收支类型'])),
      amount,
      category: stringValue(item.category || item['分类'] || item['类别']) || '未分类',
      payment_method: paymentMethod || 'other',
      flag: canonicalFlag(stringValue(item.flag || item['标记'])),
      note: stringValue(item.note || item['备注']),
    }
    if (!validDate(fields.date) || amount === '' || amountToFen(amount) === null || !validTime(fields.time) || fields.note.length > 2000 || fields.category.length > 64 || fields.payment_method.length > 64) {
      invalid.push(`row-${index + 1}`)
      return
    }
    const businessID = stringValue(item.business_id)
    records.push({ ...(businessIdPattern.test(businessID) ? { business_id: businessID } : {}), ...fields })
  })
  return { records, invalid }
}

/** 将旧版 CSV 中文表头映射到统一字段名。 */
function canonicalHeader(value: string): string {
  const aliases: Record<string, string> = {
    '日期': 'date', '时间': 'time', '类型': 'type', '收支类型': 'type', '金额': 'amount',
    '分类': 'category', '类别': 'category', '方式': 'payment_method', '支付方式': 'payment_method',
    '收款方式': 'payment_method', '标记': 'flag', '备注': 'note', '业务id': 'business_id',
    '业务编号': 'business_id', '流水号': 'business_id',
  }
  return aliases[value] ?? value
}

/** 允许旧数据留空时间，并严格检查非空的 24 小时制时间。 */
function validTime(value: string): boolean {
  if (!value) return true
  const match = /^(\d{2}):(\d{2})$/.exec(value)
  return Boolean(match && Number(match[1]) < 24 && Number(match[2]) < 60)
}

/** 兼容旧字段中文值，并将旧营收类型映射为默认收入。 */
function normalizeType(value: string): LedgerFields['type'] {
  return value === 'expense' || value === '支出' ? 'expense' : 'income'
}

/** 将已知旧中文支付方式稳定映射为跨语言代码，保留未知自定义值。 */
function canonicalMethod(value: string): string {
  const known: Record<string, string> = {
    '微信': 'wechat', '微信支付': 'wechat', '支付宝': 'alipay', '现金': 'cash', '银行卡': 'bank_card', '其他': 'other',
    wechat: 'wechat', alipay: 'alipay', cash: 'cash', bank_card: 'bank_card', other: 'other',
  }
  return known[value.trim()] ?? value.trim()
}

/** 将旧中文标记归一为不受界面语言影响的服务端枚举。 */
function canonicalFlag(value: string): LedgerFields['flag'] {
  const known: Record<string, LedgerFields['flag']> = {
    '': '', important: 'important', '重点': 'important', anomaly: 'anomaly', '异常': 'anomaly',
    pending_check: 'pending_check', '待核对': 'pending_check',
  }
  return known[value.trim()] ?? ''
}

/** 兼容 CSV/旧 JSON 中数值或文本金额并拒绝多于两位小数。 */
function normalizeAmount(value: unknown): string {
  if (typeof value !== 'string' && typeof value !== 'number') return ''
  const amount = String(value).trim()
  return amountToFen(amount) === null ? '' : amount
}

/** 校验 YYYY-MM-DD 并回读年月日，拒绝浏览器自动纠正的无效日期。 */
function validDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  const date = new Date(`${value}T12:00:00`)
  return !Number.isNaN(date.getTime()) && toDateKey(date) === value
}

/** 只对非数组 JSON 对象开放字段读取。 */
function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/** 避免 JSON 中非字符串值隐式成为 undefined 或对象文本。 */
function stringValue(value: unknown): string {
  return typeof value === 'string' || typeof value === 'number' ? String(value) : ''
}

/** 按 RFC 4180 基本规则读取逗号分隔字段并保留被引号包裹的换行。 */
function parseCsvRows(text: string): string[][] {
  const rows: string[][] = []
  let row: string[] = []
  let field = ''
  let quoted = false
  for (let index = 0; index < text.length; index += 1) {
    const character = text[index]
    if (quoted) {
      if (character === '"' && text[index + 1] === '"') { field += '"'; index += 1 }
      else if (character === '"') quoted = false
      else field += character
    } else if (character === '"' && field === '') quoted = true
    else if (character === ',') { row.push(field); field = '' }
    else if (character === '\n') { row.push(field.replace(/\r$/, '')); rows.push(row); row = []; field = '' }
    else field += character
  }
  if (quoted) throw new Error('unterminated_csv_quote')
  if (field !== '' || row.length > 0) { row.push(field.replace(/\r$/, '')); rows.push(row) }
  return rows
}

/** 计算内容级潜在重复指纹；仅用于导入预览，不会自动跳过相同金额的合法多笔交易。 */
export function ledgerFingerprint(fields: LedgerFields): string {
  return [fields.date, fields.time, fields.type, fields.amount, fields.category.trim(), fields.payment_method, fields.flag, fields.note.trim()].join('\u001f')
}

/** 转义 CSV 中含逗号、引号或换行的文本，保持 JSON/CSV 可反复导入。 */
export function csvCell(value: string): string {
  return /[",\r\n]/.test(value) ? `"${value.replace(/"/g, '""')}"` : value
}
