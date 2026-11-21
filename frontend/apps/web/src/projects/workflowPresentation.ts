import type { ProjectShooting, ProjectStatus } from '../api/types'

/** 策划列表按状态展示的固定顺序，数字不参与持久化，仅用于界面归类。 */
export const PROJECT_STATUS_ORDER: ProjectStatus[] = ['unfinished', 'executed', 'completed']

/**
 * 返回卡片上最值得执行的下一步动作文案键。
 * 文案只描述可操作入口，不把状态判断复制到前端。
 */
export function projectNextActionKey(status: ProjectStatus): string {
  if (status === 'executed') return 'projects.nextAction.writeReview'
  if (status === 'completed') return 'projects.nextAction.openReview'
  return 'projects.nextAction.execute'
}

/** 完整的拍摄时间、地点、人物和类型才允许自动建立档期。 */
export function hasCompleteShooting(shooting?: ProjectShooting | null): boolean {
  return Boolean(
    shooting?.start_at &&
    shooting.end_at &&
    shooting.location.trim() &&
    shooting.person.trim() &&
    shooting.kind,
  )
}

/**
 * 用页面当前语言格式化拍摄时段，并固定到产品统一使用的北京时间。
 * 返回值只用于展示；档期接口仍使用原始 ISO 时间，避免时区信息丢失。
 */
export function shootingTimeLabel(shooting: ProjectShooting | undefined, locale: string): string {
  if (!shooting?.start_at) return ''
  const formatter = new Intl.DateTimeFormat(locale, {
    timeZone: 'Asia/Shanghai',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
  const start = formatter.format(new Date(shooting.start_at))
  if (!shooting.end_at) return start
  const timeOnly = new Intl.DateTimeFormat(locale, {
    timeZone: 'Asia/Shanghai',
    hour: '2-digit',
    minute: '2-digit',
  })
  return `${start}–${timeOnly.format(new Date(shooting.end_at))}`
}

/** 预览自动档期名称，格式与服务端保持一致：日期 + 人物 + 拍摄类型。 */
export function scheduleTitlePreview(
  shooting: ProjectShooting | undefined,
  locale: string,
  kindLabel: string,
): string {
  if (!shooting?.start_at || !shooting.person.trim() || !shooting.kind) return ''
  const date = new Intl.DateTimeFormat(locale, {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  }).format(new Date(shooting.start_at))
  return locale.startsWith('zh')
    ? `${date}${shooting.person.trim()}${kindLabel}`
    : `${date} · ${shooting.person.trim()} · ${kindLabel}`
}

/** 生成选片新建页的安全预填值；不包含策划正文、资源 key 或内部标识。 */
export function deliveryPrefill(title: string, shooting: ProjectShooting | undefined, locale = 'zh-CN'): {
  title: string
  customer: string
  description: string
} {
  const description = [shooting?.start_at ? shootingTimeLabel(shooting, locale) : '', shooting?.location]
    .filter(Boolean)
    .join(' · ')
  return {
    title: title.trim().slice(0, 120),
    customer: (shooting?.person || '').trim().slice(0, 120),
    description: description.slice(0, 4000),
  }
}
