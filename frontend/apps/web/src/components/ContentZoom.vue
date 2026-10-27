<script setup lang="ts">
import { ref, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { isAndroid } from '../../../../packages/platform-adapters'

/** 只缩放插槽中的页面主体；导航、页脚及挂到 body 的弹层不进入缩放坐标系。 */
// 编辑器可指定内部滚动容器，让已有工具栏留在缩放边界之外，不搬动第三方组件 DOM。
const props = defineProps<{ selector?: string }>()
const viewport = ref<HTMLElement>()
const surface = ref<HTMLElement>()
const enabled = ref(false)
let container: HTMLElement | undefined
let content: HTMLElement | undefined
let originalStyles: [string | null, string | null] | undefined
let scale = 1
let width = 0
let gesture: { distance: number; scale: number; x: number; y: number; scroll: number; page: number } | null = null
let observer: ResizeObserver | undefined
const distance = (touches: TouchList) => Math.hypot(touches[0]!.clientX - touches[1]!.clientX, touches[0]!.clientY - touches[1]!.clientY)

/** 每次手势保留两指中心对应的内容点，缩回原尺寸后消除横向滚动。 */
function start(event: TouchEvent) {
  if (!enabled.value || event.touches.length !== 2 || !container) return
  // 裁剪、标注和资源手柄拥有独立坐标与拖动状态，不能被页面手势抢占。
  if ((event.target as Element | null)?.closest('[data-content-zoom-ignore], .resource-resize-handle, .resource-drag-handle, .image-anchor')) return
  const rect = container.getBoundingClientRect()
  const x = (event.touches[0]!.clientX + event.touches[1]!.clientX) / 2
  const y = (event.touches[0]!.clientY + event.touches[1]!.clientY) / 2
  gesture = { distance: distance(event.touches), scale, x: x - rect.left, y: y - rect.top, scroll: container.scrollLeft, page: window.scrollY }
  event.preventDefault()
}
function move(event: TouchEvent) {
  if (!gesture || event.touches.length !== 2 || !container || !content) return
  event.preventDefault()
  const next = Math.min(3, Math.max(1, gesture.scale * distance(event.touches) / Math.max(1, gesture.distance)))
  content.style.zoom = String(next)
  container.scrollLeft = (gesture.scroll + gesture.x) * next / gesture.scale - gesture.x
  window.scrollTo(window.scrollX, gesture.page + gesture.y * (next / gesture.scale - 1))
  scale = next
}
function end() { gesture = null }
onMounted(async () => {
  enabled.value = isAndroid()
  // 先等待原尺寸容器从 display:contents 切换为实体布局，避免首次观察回调重置刚开始的手势。
  await nextTick()
  if (!enabled.value || !viewport.value || !surface.value) return
  container = props.selector ? viewport.value.querySelector<HTMLElement>(props.selector) ?? undefined : viewport.value
  content = props.selector ? container?.firstElementChild as HTMLElement | undefined : surface.value
  if (!container || !content) return
  if (props.selector) {
    originalStyles = [container.getAttribute('style'), content.getAttribute('style')]
    container.classList.add('content-zoom-viewport'); content.classList.add('content-zoom-surface')
    // 第三方编辑器原有 overflow:visible 的选择器更具体，在指定容器上明确保留主体内滚动。
    container.style.overflowX = 'auto'; container.style.overflowY = 'clip'
  }
  const resize = () => {
    const next = container!.clientWidth
    if (next === width || !next) return
    width = next
    content!.style.width = `${width}px`
    // 旋转屏幕或分屏后恢复可预测的布局，不沿用旧坐标。
    content!.style.zoom = '1'; scale = 1; gesture = null
  }
  resize()
  observer = new ResizeObserver(resize); observer.observe(container)
  container.addEventListener('touchstart', start, { passive: false })
  container.addEventListener('touchmove', move, { passive: false })
  container.addEventListener('touchend', end)
  container.addEventListener('touchcancel', end)
})
onBeforeUnmount(() => {
  observer?.disconnect()
  container?.removeEventListener('touchstart', start)
  container?.removeEventListener('touchmove', move)
  container?.removeEventListener('touchend', end)
  container?.removeEventListener('touchcancel', end)
  if (props.selector && container && content && originalStyles) {
    container.classList.remove('content-zoom-viewport'); content.classList.remove('content-zoom-surface')
    for (const [node, style] of [[container, originalStyles[0]], [content, originalStyles[1]]] as const) {
      if (style === null) node.removeAttribute('style'); else node.setAttribute('style', style)
    }
  }
})
</script>

<template>
  <div ref="viewport" :class="enabled && !props.selector ? 'content-zoom-viewport' : 'content-zoom-passthrough'">
    <div ref="surface" :class="enabled && !props.selector ? 'content-zoom-surface' : 'content-zoom-passthrough'"><slot /></div>
  </div>
</template>

<style>
.content-zoom-passthrough { display: contents; }
/* 横向浏览限定在主体，避免把整个页面及顶栏拖出屏幕。纵向仍由页面统一滚动。 */
.content-zoom-viewport { width: 100%; min-width: 0; overflow-x: auto; overflow-y: clip; touch-action: pan-x pan-y; }
.content-zoom-surface { display: flow-root; transform-origin: top left; }
</style>
