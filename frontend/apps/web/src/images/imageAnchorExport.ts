/**
 * 灵感图导出时的锚点绘制工具。
 *
 * 预览层使用 CSS 叠层显示锚点，导出层必须把相同信息写入图片像素。这里以
 * 420px 的移动端预览宽度为基准，把标题、描述、落点和文字倍率换算到导出画布；
 * 坐标仍由编辑核心统一映射旋转和裁切，避免导出与预览产生位置偏差。
 */

import { anchorToView, clampAnchorLabelScale, type EditorDocument } from '../../../../packages/editor-core/src'
import {
  ANCHOR_DESCRIPTION_BASE_FONT_SIZE, ANCHOR_LABEL_MARKER_OFFSET, ANCHOR_TITLE_BASE_FONT_SIZE,
  anchorLabelMaxWidth, anchorNoteLongestLine, splitAnchorNote,
} from '../views/editorAnchorLayout'
import { UI_FONT_STACK } from '../uiFont'

/** 与编辑器常见手机预览宽度一致，确保导出文字保持当前视觉比例。 */
export const ANCHOR_EXPORT_REFERENCE_WIDTH = 420
const MARKER_DOT_RATIO = 0.48
const DESCRIPTION_GAP_RATIO = 0.08
const TITLE_LINE_HEIGHT = 1.3
const DESCRIPTION_LINE_HEIGHT = 1.55
const UNDERLINE_RATIO = 0.10

/** 导出锚点所需的尺寸；所有值均已换算为输出图片像素。 */
export interface AnchorExportMetrics {
  layoutScale: number
  titleFontSize: number
  descriptionFontSize: number
  markerDotSize: number
  descriptionGap: number
  markerOffset: number
  underlineThickness: number
}

/** 按导出宽度、全局显示比例和单个锚点文字倍率计算可绘制尺寸。 */
export function anchorExportMetrics(imageWidth: number, displayScale = 1, labelScale = 1): AnchorExportMetrics {
  const width = Number.isFinite(imageWidth) && imageWidth > 0 ? imageWidth : ANCHOR_EXPORT_REFERENCE_WIDTH
  const globalScale = Number.isFinite(displayScale) && displayScale > 0 ? displayScale : 1
  const textScale = clampAnchorLabelScale(labelScale)
  const layoutScale = width / ANCHOR_EXPORT_REFERENCE_WIDTH * globalScale
  const titleFontSize = ANCHOR_TITLE_BASE_FONT_SIZE * layoutScale * textScale
  return {
    layoutScale,
    titleFontSize,
    descriptionFontSize: ANCHOR_DESCRIPTION_BASE_FONT_SIZE * layoutScale * textScale,
    markerDotSize: ANCHOR_TITLE_BASE_FONT_SIZE * layoutScale * textScale * MARKER_DOT_RATIO,
    descriptionGap: ANCHOR_TITLE_BASE_FONT_SIZE * layoutScale * textScale * DESCRIPTION_GAP_RATIO,
    markerOffset: ANCHOR_LABEL_MARKER_OFFSET * layoutScale,
    underlineThickness: Math.max(1 * layoutScale, titleFontSize * UNDERLINE_RATIO),
  }
}

/**
 * 按画布实际可用宽度逐字换行。
 *
 * 浏览器预览使用 `overflow-wrap: anywhere`，因此导出不能只按空格断行，否则连续的
 * 中文、网址或长单词会在图片边缘被裁切。显式换行符始终保留，空行也保留。
 */
export function wrapAnchorText(
  context: Pick<CanvasRenderingContext2D, 'measureText'>,
  text: string,
  maxWidth: number,
): string[] {
  const limit = Number.isFinite(maxWidth) ? Math.max(1, maxWidth) : 1
  const result: string[] = []
  for (const paragraph of String(text ?? '').replace(/\r\n?/g, '\n').split('\n')) {
    if (!paragraph) {
      result.push('')
      continue
    }
    let line = ''
    for (const character of Array.from(paragraph)) {
      const candidate = line + character
      if (line && context.measureText(candidate).width > limit) {
        result.push(line)
        line = character
      } else {
        line = candidate
      }
    }
    result.push(line)
  }
  return result.length ? result : ['']
}

/** 将当前文档中可见的锚点写入已渲染的导出画布，不修改文档数据。 */
export function drawDocumentAnchors(
  context: CanvasRenderingContext2D,
  document: EditorDocument,
  width: number,
  height: number,
  displayScale = 1,
): void {
  if (!document.anchors.length || width <= 0 || height <= 0) return

  for (const anchor of document.anchors) {
    const position = anchorToView(anchor, document)
    if (!position.visible) continue
    const labelScale = clampAnchorLabelScale(anchor.label_scale ?? 1)
    const metrics = anchorExportMetrics(width, displayScale, labelScale)
    const markerX = position.x * width
    const markerY = position.y * height
    const availableRight = Math.max(1, width - markerX - metrics.markerOffset)
    const availableCanvasWidth = Math.max(1, width - markerX)
    const labelMaxWidth = Math.min(
      availableCanvasWidth,
      anchorLabelMaxWidth(anchorNoteLongestLine(anchor.note), availableRight, metrics.layoutScale, labelScale),
    )
    const { title, description } = splitAnchorNote(anchor.note)

    context.save()
    context.textAlign = 'left'
    context.shadowColor = 'rgba(0, 0, 0, 0.55)'
    context.shadowBlur = Math.max(1, metrics.layoutScale * 3)
    context.shadowOffsetY = Math.max(1, metrics.layoutScale)
    context.fillStyle = '#ffffff'
    context.font = `500 ${metrics.titleFontSize}px ${UI_FONT_STACK}`

    const titleLines = title ? wrapAnchorText(context, title, labelMaxWidth) : []
    const titleLineHeight = metrics.titleFontSize * TITLE_LINE_HEIGHT
    context.textBaseline = 'bottom'
    for (let index = 0; index < titleLines.length; index += 1) {
      const line = titleLines[index]!
      const lineBottom = markerY - (titleLines.length - 1 - index) * titleLineHeight
      context.fillText(line, markerX, lineBottom)
    }

    // 标题下划线与预览层一致；每一行都有黄色下划线，标题换行后不会失去落点提示。
    context.shadowBlur = Math.max(1, metrics.layoutScale * 2)
    context.fillStyle = '#f3e600'
    for (let index = 0; index < titleLines.length; index += 1) {
      const line = titleLines[index]!
      const lineWidth = Math.min(labelMaxWidth, context.measureText(line).width)
      const lineBottom = markerY - (titleLines.length - 1 - index) * titleLineHeight
      context.fillRect(markerX, lineBottom - metrics.underlineThickness, lineWidth, metrics.underlineThickness)
    }

    // 黄色圆点以锚点坐标为圆心，白色描边和阴影保证在明暗背景上都可辨认。
    context.shadowColor = 'rgba(0, 0, 0, 0.38)'
    context.shadowBlur = Math.max(1, metrics.layoutScale * 4)
    context.shadowOffsetY = Math.max(1, metrics.layoutScale)
    context.beginPath()
    context.arc(markerX, markerY, metrics.markerDotSize / 2, 0, Math.PI * 2)
    context.fillStyle = '#f3e600'
    context.fill()
    context.strokeStyle = 'rgba(255, 255, 255, 0.65)'
    context.lineWidth = Math.max(1, metrics.layoutScale)
    context.stroke()

    if (description) {
      context.shadowColor = 'rgba(0, 0, 0, 0.55)'
      context.shadowBlur = Math.max(1, metrics.layoutScale * 3)
      context.shadowOffsetY = Math.max(1, metrics.layoutScale)
      context.fillStyle = 'rgba(255, 255, 255, 0.80)'
      context.font = `${metrics.descriptionFontSize}px ${UI_FONT_STACK}`
      context.textBaseline = 'top'
      const descriptionLines = wrapAnchorText(context, description, labelMaxWidth)
      const descriptionLineHeight = metrics.descriptionFontSize * DESCRIPTION_LINE_HEIGHT
      const descriptionTop = markerY + metrics.markerDotSize / 2 + metrics.descriptionGap
      for (let index = 0; index < descriptionLines.length; index += 1) {
        context.fillText(descriptionLines[index]!, markerX, descriptionTop + index * descriptionLineHeight)
      }
    }
    context.restore()
  }
}
