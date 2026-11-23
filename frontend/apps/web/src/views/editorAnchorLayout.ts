/**
 * 灵感图详情锚点文字块的排版计算。
 *
 * 锚点文字始终从锚点位置向右展开，并允许自动换行；这里只计算文字块的最大可用宽度，
 * 不读取 DOM 或响应式状态，便于在单元测试中稳定验证窄屏右边缘和文字倍率场景。
 */

/** 标签在画布上至少保留的宽度；即使文字贴近右边缘，也避免内容缩成一条竖线。 */
export const ANCHOR_LABEL_MIN_WIDTH = 72
/** 标签的绝对宽度上限，防止长备注在大屏上横向铺满整张图片。 */
export const ANCHOR_LABEL_MAX_WIDTH = 280
/** 文字块起点到画布右边缘之间预留的避让距离，按锚点展示比例缩放。 */
export const ANCHOR_LABEL_MARKER_OFFSET = 8

/**
 * 拆分锚点文字：第一行是带下划线的标题，其余行是更小的详细描述。
 * 兼容只有一行文字的旧锚点；开头空行会被忽略，描述内部的换行保持不变。
 */
export function splitAnchorNote(note: string): { title: string; description: string } {
  const lines = String(note ?? '').split(/\r?\n/)
  while (lines.length && !lines[0]!.trim()) lines.shift()
  const title = (lines.shift() ?? '').trim()
  const description = lines.join('\n').trim()
  return { title, description }
}

/** 列表和可访问标签只需要标题，避免把多行描述压成难读的长文本。 */
export function anchorNoteTitle(note: string): string {
  return splitAnchorNote(note).title
}

/** 文字块宽度按最长一行估算，避免多行描述把短标题撑得过宽。 */
export function anchorNoteLongestLine(note: string): string {
  return String(note ?? '').split(/\r?\n/).reduce((longest, line) => (Array.from(line).length > Array.from(longest).length ? line : longest), '')
}

/**
 * 计算锚点文字标签的最大宽度（屏幕像素）。
 *
 * @param note 锚点文字；按字符个数估算单行理想宽度，中文与英文使用同一估算口径。
 * @param availableRight 锚点位置右侧到画布边缘的可用像素宽度。
 * @param displayScale 用户设置的全局锚点展示比例（滑杆值的百分比）。
 * @param labelScale 单个锚点保存的文字倍率，缺省为 1。
 * @returns 标签最大宽度；内容较短时保持紧凑，内容较长或空间不足时按该宽度换行。
 */
export function anchorLabelMaxWidth(note: string, availableRight: number, displayScale: number, labelScale = 1): number {
  const scale = Number.isFinite(displayScale) && displayScale > 0 ? displayScale : 1
  const textScale = Number.isFinite(labelScale) && labelScale > 0 ? labelScale : 1
  const characters = Math.max(1, Array.from(note).length)
  // 单行理想宽度按标题字号（14px/字符）估算，再叠加用户文字倍率，避免标题因低估而提前换行。
  const desired = Math.min(
    ANCHOR_LABEL_MAX_WIDTH * scale * textScale,
    Math.max(96 * scale, characters * 14 * scale * textScale + 24 * scale),
  )
  const available = Math.max(0, Number.isFinite(availableRight) ? availableRight : 0)
  // 右侧没有可用空间时退回最小宽度，避免长备注按完整理想宽度展开后被画布边缘裁掉大半。
  return Math.max(ANCHOR_LABEL_MIN_WIDTH * scale, Math.min(desired, available))
}
