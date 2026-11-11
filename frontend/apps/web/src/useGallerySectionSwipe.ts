import { onBeforeUnmount, onMounted, type Ref } from 'vue'
import { useRouter } from 'vue-router'

/** 列表与统计共用单指横滑导航；表单、图表、横向容器及双指缩放保留原手势。 */
export function useGallerySectionSwipe(root: Ref<HTMLElement | undefined>, section: 'list' | 'statistics') {
  const router = useRouter()
  let start: { x: number; y: number } | null = null
  const cancel = () => { start = null }
  const begin = (event: TouchEvent) => {
    cancel()
    if (event.touches.length !== 1) return
    const target = event.target as HTMLElement
    if (target.closest('input,button,select,textarea,[role="combobox"],.activity-scroll,.statistics-chart,[data-content-zoom-ignore]')) return
    // 已缩放的安卓主体需要横向平移图片，不能把平移误判为切换页面。
    const surface = target.closest<HTMLElement>('.content-zoom-surface')
    if (surface && Number(surface.style.zoom || 1) > 1) return
    start = { x: event.touches[0]!.clientX, y: event.touches[0]!.clientY }
  }
  const move = (event: TouchEvent) => {
    if (event.touches.length !== 1 || (start && Math.abs(event.touches[0]!.clientY - start.y) > 35)) cancel()
  }
  const end = (event: TouchEvent) => {
    const point = start
    cancel()
    if (!point || event.touches.length || event.changedTouches.length !== 1) return
    const dx = event.changedTouches[0]!.clientX - point.x
    const dy = event.changedTouches[0]!.clientY - point.y
    if (Math.abs(dx) < 70 || Math.abs(dy) > 35 || Math.abs(dx) < Math.abs(dy) * 2) return
    if (section === 'list' && dx < 0) void router.push('/inspirations/statistics')
    if (section === 'statistics' && dx > 0) void router.push('/inspirations')
  }
  onMounted(() => {
    root.value?.addEventListener('touchstart', begin, { passive: true })
    root.value?.addEventListener('touchmove', move, { passive: true })
    root.value?.addEventListener('touchend', end, { passive: true })
    root.value?.addEventListener('touchcancel', cancel, { passive: true })
  })
  onBeforeUnmount(() => {
    root.value?.removeEventListener('touchstart', begin)
    root.value?.removeEventListener('touchmove', move)
    root.value?.removeEventListener('touchend', end)
    root.value?.removeEventListener('touchcancel', cancel)
  })
}
