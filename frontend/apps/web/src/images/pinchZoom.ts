/**
 * 双指查看器的几何参数只依赖视口和画布边界。
 * 独立成纯函数后可以在不启动浏览器的情况下验证跟手比例、居中进度和缩放上限。
 */
export interface ZoomRect {
  left: number
  top: number
  width: number
  height: number
}

export interface ZoomViewport {
  width: number
  height: number
}

export interface PinchPoints {
  distance: number
  x: number
  y: number
}

export interface PinchFrame {
  scale: number
  x: number
  y: number
  progress: number
  targetScale: number
}

/** 让常见竖图和横图都能产生可感知的放大过程。 */
export const MIN_PINCH_ZOOM_SCALE = 1.35
/** 手指继续分开时允许超过全屏阈值，但仍限制单帧几何规模，避免异常触控数据撑爆画布。 */
export const MAX_PINCH_ZOOM_SCALE = 4
export const PINCH_FULLSCREEN_THRESHOLD = 0.96

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.max(minimum, Math.min(maximum, Number.isFinite(value) ? value : minimum))
}

/** 双指距离为 0 时返回 0，由调用方忽略无效手势，避免除零。 */
export function pointDistance(first: { x: number; y: number }, second: { x: number; y: number }): number {
  return Math.hypot(first.x - second.x, first.y - second.y)
}

/** 以源画布实际栅格边界计算视觉全屏比例；短边也至少提供一次清晰放大。 */
export function pinchTargetScale(source: ZoomRect, viewport: ZoomViewport): number {
  const fit = Math.min(viewport.width / Math.max(1, source.width), viewport.height / Math.max(1, source.height))
  return clamp(fit, MIN_PINCH_ZOOM_SCALE, MAX_PINCH_ZOOM_SCALE)
}

/**
 * 图片按双指距离连续缩放，并跟随双指中心移动；接近全屏时逐步把图片中心移到视口中心。
 * 返回的进度只决定背景和居中程度，scale 本身始终直接对应手指距离，因此不会在阈值处突跳。
 */
export function pinchZoomFrame(
  source: ZoomRect,
  viewport: ZoomViewport,
  start: PinchPoints,
  current: PinchPoints,
): PinchFrame {
  const targetScale = pinchTargetScale(source, viewport)
  const scale = clamp(current.distance / Math.max(1, start.distance), 1, MAX_PINCH_ZOOM_SCALE)
  const progress = targetScale <= 1 ? 1 : clamp((scale - 1) / (targetScale - 1), 0, 1)
  const sourceCenterX = source.left + source.width / 2
  const sourceCenterY = source.top + source.height / 2

  // 把起始双指中心换算成图片本地坐标。缩放后让该点贴近当前双指中心，再按进度移向屏幕中心。
  const localPinchX = start.x - sourceCenterX
  const localPinchY = start.y - sourceCenterY
  const followX = current.x - sourceCenterX - localPinchX * scale
  const followY = current.y - sourceCenterY - localPinchY * scale
  const centeredX = viewport.width / 2 - sourceCenterX
  const centeredY = viewport.height / 2 - sourceCenterY

  return {
    scale,
    x: followX + (centeredX - followX) * progress,
    y: followY + (centeredY - followY) * progress,
    progress,
    targetScale,
  }
}
