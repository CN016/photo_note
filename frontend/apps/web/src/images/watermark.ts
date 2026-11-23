/**
 * 灵感图导出水印的几何与绘制工具。
 *
 * 水印位置和大小使用与画布相同的归一化坐标：位置 0–1 表示图片内的相对点，
 * 倍率只影响字号。预览层和最终导出都调用同一组计算，保证所见即所得。
 */

/** 水印相对位置与字号倍率；位置按旋转、裁切后的图片归一化坐标保存。 */
export interface WatermarkPlacement {
  x: number
  y: number
  scale: number
}

export const MIN_WATERMARK_SCALE = 0.5
export const MAX_WATERMARK_SCALE = 3
export const DEFAULT_WATERMARK_SCALE = 1
/** 字号按图片高度的固定比例计算；100% 时约为图片高度的 4.5%。 */
export const WATERMARK_FONT_RATIO = 0.045

/** 将倍率限制在可操作范围并按 5% 对齐，避免拖动滑杆产生浮点噪声。 */
export function clampWatermarkScale(value: number): number {
  if (!Number.isFinite(value)) return DEFAULT_WATERMARK_SCALE
  return Math.round(Math.max(MIN_WATERMARK_SCALE, Math.min(MAX_WATERMARK_SCALE, value)) * 20) / 20
}

/** 将水印中心限制在图片范围内；允许贴边但不允许被拖出画布。 */
export function clampWatermarkPoint(value: number): number {
  if (!Number.isFinite(value)) return 0.5
  return Math.max(0.02, Math.min(0.98, value))
}

/**
 * 按水印文字实际尺寸限制中心点，保证整段文字都落在导出画布内。
 * halfWidthRatio / halfHeightRatio 是文字半宽、半高相对预览画布的归一化比例。
 * 文字宽于画布时只能在水平方向居中，仍会溢出，但不会因为拖到边缘而只导出半个词。
 */
export function clampWatermarkPlacement(
  x: number,
  y: number,
  halfWidthRatio: number,
  halfHeightRatio: number,
): { x: number; y: number } {
  const clampAxis = (value: number, half: number): number => {
    if (!Number.isFinite(value)) return 0.5
    if (!Number.isFinite(half) || half <= 0) return Math.max(0, Math.min(1, value))
    if (half >= 0.5) return 0.5
    return Math.max(half, Math.min(1 - half, value))
  }
  return { x: clampAxis(x, halfWidthRatio), y: clampAxis(y, halfHeightRatio) }
}

/** 计算导出画布上的水印字号，至少保留 10 像素以保证小图也能辨认。 */
export function watermarkFontSize(imageHeight: number, scale: number): number {
  const height = Math.max(1, Number.isFinite(imageHeight) ? imageHeight : 1)
  return Math.max(10, Math.round(height * WATERMARK_FONT_RATIO * clampWatermarkScale(scale)))
}

/** 计算浏览器预览层的字号；与导出使用同一比例，仅设置更高的最小可读值。 */
export function watermarkPreviewFontSize(displayHeight: number, scale: number): number {
  const height = Math.max(1, Number.isFinite(displayHeight) ? displayHeight : 1)
  return Math.max(12, Math.round(height * WATERMARK_FONT_RATIO * clampWatermarkScale(scale)))
}

/** 在尺寸已确定的画布上绘制白字品牌水印，位置与大小和预览层保持一致。 */
export function drawWatermark(
  context: CanvasRenderingContext2D,
  text: string,
  placement: WatermarkPlacement,
  width: number,
  height: number,
): void {
  const fontSize = watermarkFontSize(height, placement.scale)
  context.save()
  context.font = `700 ${fontSize}px "Manrope", "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif`
  context.textAlign = 'center'
  context.textBaseline = 'middle'
  // 半透明白字配合轻微阴影，既能压在浅色天空上，也不会像实心贴纸一样遮住主体。
  context.shadowColor = 'rgba(0, 0, 0, 0.45)'
  context.shadowBlur = Math.max(2, fontSize * 0.35)
  context.shadowOffsetY = Math.max(1, fontSize * 0.06)
  context.fillStyle = 'rgba(255, 255, 255, 0.86)'
  context.fillText(text, placement.x * width, placement.y * height)
  context.restore()
}
