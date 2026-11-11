import { gsap } from 'gsap'

/** 只保留本次导航所需的可见像素，不导出数据URL、不持久化私有图片；长边限制1600像素。 */
export function copyVisibleImage(source: HTMLCanvasElement | HTMLImageElement): HTMLCanvasElement | null {
  const width = source instanceof HTMLImageElement ? source.naturalWidth : source.width
  const height = source instanceof HTMLImageElement ? source.naturalHeight : source.height
  if (!width || !height) return null
  const scale = Math.min(1, 1600 / Math.max(width, height))
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(1, Math.round(width * scale)); canvas.height = Math.max(1, Math.round(height * scale))
  try {
    const context = canvas.getContext('2d')
    if (!context) return null
    context.drawImage(source, 0, 0, canvas.width, canvas.height)
    return canvas
  } catch { canvas.width = 0; canvas.height = 0; return null }
}
interface ImageLayer { layer: HTMLDivElement; pixels: HTMLCanvasElement; ready: HTMLCanvasElement | null; opened: boolean; closing: boolean }
const layers = new Map<HTMLElement, ImageLayer>()
let pendingPreview: HTMLCanvasElement | null = null

/** 点击列表时同步复制已经解码的缩略图，首帧不等待详情网络或再次解码。 */
export function captureListImage(marker: HTMLElement): boolean {
  discardPendingImage()
  const image = marker.querySelector<HTMLImageElement>('.thumbnail-frame img')
  if (image?.complete && image.naturalWidth) pendingPreview = copyVisibleImage(image)
  return pendingPreview !== null
}
export function discardPendingImage(): void {
  if (pendingPreview) { pendingPreview.width = 0; pendingPreview.height = 0; pendingPreview = null }
}

/** 将快照放入同一GSAP缩放根节点，图片会随窗口一起展开和倒放。 */
function attach(root: HTMLElement, pixels: HTMLCanvasElement, kind: string): ImageLayer {
  removeMotionImage(root)
  const layer = document.createElement('div')
  layer.dataset.motionImage = kind; layer.setAttribute('aria-hidden', 'true')
  Object.assign(layer.style, { position: 'absolute', inset: '0', zIndex: '1', pointerEvents: 'none', overflow: 'hidden', background: 'var(--surface)' })
  Object.assign(pixels.style, { position: 'absolute', left: '0', top: '0', width: '100%', height: '100%', objectFit: 'cover' })
  layer.appendChild(pixels); root.appendChild(layer)
  const state = { layer, pixels, ready: null, opened: false, closing: false }
  layers.set(root, state)
  return state
}
export function attachListImage(root: HTMLElement): void {
  if (!pendingPreview) return
  const image = pendingPreview; pendingPreview = null
  attach(root, image, 'preview')
}

/** 详情真正渲染出像素后再交接，慢网时继续显示列表预览，不在动画结束时切成空白。 */
export function detailImageReady(canvas: HTMLCanvasElement): void {
  const root = canvas.closest<HTMLElement>('.route-page-frame')
  const state = root && layers.get(root)
  if (!root || !state || state.closing) return
  state.ready = canvas
  if (state.opened) reveal(root, state)
}
function reveal(root: HTMLElement, state: ImageLayer): void {
  if (!state.ready || state.closing || !state.ready.isConnected) return
  gsap.killTweensOf([state.layer, state.pixels])
  const target = state.ready.getBoundingClientRect(), frame = root.getBoundingClientRect()
  // 从展开预览平滑对齐到真实画布，再交接显示；不导出或重新请求图片。
  gsap.to(state.pixels, { left: target.left - frame.left, top: target.top - frame.top, width: target.width, height: target.height, objectFit: 'contain', duration: 0.18, ease: 'power2.out' })
  gsap.to(state.layer, { opacity: 0, duration: 0.12, delay: 0.16, onComplete: () => { if (layers.get(root) === state && !state.closing) removeMotionImage(root) } })
}
export function completeImageOpen(root: HTMLElement): void {
  const state = layers.get(root)
  if (!state) return
  state.opened = true
  if (state.ready) reveal(root, state)
}

/** Vue卸载会清空原canvas，必须先同步复制当前真实详情图（包括旋转、裁切和颜色）。 */
export function captureDetailImage(canvas: HTMLCanvasElement | null): void {
  const root = canvas?.closest<HTMLElement>('.route-page-frame')
  if (!root || !canvas) return
  const existing = layers.get(root)
  if (existing && !existing.opened) { existing.closing = true; return }
  const pixels = copyVisibleImage(canvas)
  if (!pixels) { if (existing) existing.closing = true; return }
  const bounds = canvas.getBoundingClientRect(), frame = root.getBoundingClientRect()
  const sx = frame.width / Math.max(1, root.offsetWidth), sy = frame.height / Math.max(1, root.offsetHeight)
  const state = attach(root, pixels, 'detail')
  // 中途返回仍保持当前根节点缩放，快照使用根节点局部坐标，避免出现完整窗口的跳变。
  Object.assign(pixels.style, { left: `${(bounds.left - frame.left) / sx}px`, top: `${(bounds.top - frame.top) / sy}px`, width: `${bounds.width / sx}px`, height: `${bounds.height / sy}px`, objectFit: 'contain' })
  state.closing = true
}
export function removeMotionImage(root: HTMLElement): void {
  const state = layers.get(root)
  if (!state) return
  gsap.killTweensOf([state.layer, state.pixels]); state.layer.remove()
  state.pixels.width = 0; state.pixels.height = 0
  layers.delete(root)
}
export function resetMotionImages(): void {
  discardPendingImage()
  for (const root of [...layers.keys()]) removeMotionImage(root)
}
