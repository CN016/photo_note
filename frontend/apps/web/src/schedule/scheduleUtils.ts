/** 以 Asia/Shanghai 日期键操作日历，避免浏览器设备时区改变档期所属日期。 */
export function chinaDateKey(value: Date = new Date()): string {
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(value)
  const part = (type: string) => parts.find(item => item.type === type)?.value ?? '00'
  return `${part('year')}-${part('month')}-${part('day')}`
}

/** 日期键只表示日历日期；以 UTC 中午迭代可避免夏令时导致跨日偏移。 */
export function addDateKey(value: string, days: number): string {
  const [year, month, day] = value.split('-').map(Number)
  const date = new Date(Date.UTC(year, month - 1, day + days, 12))
  return `${date.getUTCFullYear()}-${String(date.getUTCMonth() + 1).padStart(2, '0')}-${String(date.getUTCDate()).padStart(2, '0')}`
}

/** 生成以周一开头的六周月历日期键，跨月日期也能用于档期摘要。 */
export function monthCalendarKeys(monthKey: string): string[] {
  const [year, month] = monthKey.split('-').map(Number)
  const first = new Date(Date.UTC(year, month - 1, 1, 12))
  const weekdayOffset = (first.getUTCDay() + 6) % 7
  const firstCell = addDateKey(`${monthKey}-01`, -weekdayOffset)
  return Array.from({ length: 42 }, (_, index) => addDateKey(firstCell, index))
}

/** 将 datetime-local 输入明确标记为 +08:00，服务端不会按主机时区猜测。 */
export function chinaDateTimeInput(value: string): string {
  const parts = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(new Date(value))
  const part = (type: string) => parts.find(item => item.type === type)?.value ?? '00'
  return `${part('year')}-${part('month')}-${part('day')}T${part('hour')}:${part('minute')}`
}

/** 解析 datetime-local 文本并附上中国标准时间偏移。 */
export function toChinaTimestamp(value: string): string {
  if (value.length === 16) return `${value}:00+08:00`
  if (value.length === 19) return `${value}+08:00`
  return value
}

/** 生成页面专用日期显示文本，不受浏览器当前时区影响。 */
export function formatChinaDate(value: string, locale: string, options: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric' }): string {
  return new Intl.DateTimeFormat(locale, { ...options, timeZone: 'Asia/Shanghai' }).format(new Date(`${value}T12:00:00+08:00`))
}

/** 将时刻格式化成中国标准时间的 24 小时时钟。 */
export function formatChinaTime(value: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { timeZone: 'Asia/Shanghai', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(new Date(value))
}

/** 限制自定义日历窗口为 1 至 90 天。 */
export function clampScheduleDays(value: number): number {
  return Math.min(90, Math.max(1, Math.floor(Number.isFinite(value) ? value : 1)))
}

/** 周视图从周一开始，保证月视图和日期范围导航使用相同周定义。 */
export function mondayOf(value: string): string {
  const [year, month, day] = value.split('-').map(Number)
  const weekday = new Date(Date.UTC(year, month - 1, day, 12)).getUTCDay()
  return addDateKey(value, -((weekday + 6) % 7))
}
