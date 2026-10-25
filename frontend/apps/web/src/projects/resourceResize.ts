import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { NodeViewProps } from '@tiptap/vue-3'

/** 共用媒体尺寸交互：预览仅改本地状态，松手后写一次文档，取消/卸载时丢弃草稿。 */
export function useResourceResize(props: NodeViewProps) {
  const { locale } = useI18n({ useScope: 'global' })
  const width = computed(() => Math.max(30, Math.min(100, Number(props.node.attrs.width) || 100)))
  const draftWidth = ref(width.value)
  const resizing = ref(false)
  const cornerResizing = ref(false)
  const displayWidth = computed(() => resizing.value ? draftWidth.value : width.value)
  const widthLabel = computed(() => new Intl.NumberFormat(locale.value, { style: 'percent', maximumFractionDigits: 1 }).format(displayWidth.value / 100))
  const smallMode = computed(() => Math.abs(displayWidth.value - 100 / 3) < .01)
  const originalMode = computed(() => displayWidth.value === 100)
  let lastCommittedWidth = width.value
  let pointer: { id: number; target: HTMLElement; x: number; y: number; pixels: number; percent: number; ratio: number; container: number; moved: boolean } | null = null
  function beginResize() { draftWidth.value = width.value; resizing.value = true }
  function previewWidth(event: Event) { draftWidth.value = Math.max(30, Math.min(100, Number((event.target as HTMLInputElement).value))) }
  function commitWidth() {
    const next = draftWidth.value
    resizing.value = false
    if (next === lastCommittedWidth) return
    lastCommittedWidth = next
    props.updateAttributes({ width: next, layout: next === 100 ? 'original' : 'small' })
  }
  /** 释放捕获前先清空会话，防止 lostpointercapture 再次提交或撤销本次操作。 */
  function release() {
    const previous = pointer
    pointer = null
    cornerResizing.value = false
    window.removeEventListener('keydown', escapeResize, true)
    if (previous?.target.hasPointerCapture(previous.id)) previous.target.releasePointerCapture(previous.id)
  }
  function cancelCornerResize() {
    if (!pointer) return
    release()
    draftWidth.value = width.value
    resizing.value = false
  }
  function escapeResize(event: KeyboardEvent) {
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); cancelCornerResize() }
  }
  /** 以起始节点尺寸和父容器宽度计算百分比；投影到原始对角线，使横向/纵向拖动都保持比例。 */
  function startCornerResize(event: PointerEvent) {
    if (!event.isPrimary || event.button !== 0) return
    const target = event.currentTarget as HTMLElement
    const block = target.closest('.project-resource-node') as HTMLElement | null
    if (!block) return
    const rect = block.getBoundingClientRect()
    const parent = block.parentElement
    if (!parent) return
    const style = getComputedStyle(parent)
    const container = parent.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight)
    if (!rect.width || !container) return
    beginResize()
    cornerResizing.value = true
    pointer = { id: event.pointerId, target, x: event.clientX, y: event.clientY, pixels: rect.width, percent: width.value, ratio: rect.height / rect.width, container, moved: false }
    target.setPointerCapture(event.pointerId)
    window.addEventListener('keydown', escapeResize, true)
  }
  function moveCornerResize(event: PointerEvent) {
    if (!pointer || pointer.id !== event.pointerId) return
    const dx = event.clientX - pointer.x
    const dy = event.clientY - pointer.y
    if (!pointer.moved && Math.hypot(dx, dy) < 2) return
    pointer.moved = true
    const delta = (dx + dy * pointer.ratio) / (1 + pointer.ratio ** 2)
    draftWidth.value = Math.max(30, Math.min(100, (Math.abs(delta) < .5 ? pointer.percent : Math.round(pointer.percent + delta / pointer.container * 100))))
  }
  function finishCornerResize(event: PointerEvent) {
    if (!pointer || pointer.id !== event.pointerId) return
    moveCornerResize(event)
    release()
    commitWidth()
  }
  /** 两个快捷尺寸沿用百分比文档字段，三等分使用精确分数，保证三张媒体可同排。 */
  function setSizePreset(small: boolean) {
    cancelCornerResize()
    draftWidth.value = small ? 100 / 3 : 100
    commitWidth()
  }
  /** 键盘每次按键为一次独立尺寸命令，首尾键分别跳至最小/最大宽度。 */
  function resizeWithKeyboard(event: KeyboardEvent) {
    const changes: Record<string, number> = { ArrowLeft: -1, ArrowDown: -1, ArrowRight: 1, ArrowUp: 1 }
    if (!(event.key in changes) && !['Home', 'End'].includes(event.key)) return
    event.preventDefault()
    draftWidth.value = event.key === 'Home' ? 30 : event.key === 'End' ? 100 : Math.max(30, Math.min(100, width.value + changes[event.key]! * (event.shiftKey ? 5 : 1)))
    commitWidth()
  }
  watch(width, value => { lastCommittedWidth = value; if (!resizing.value) draftWidth.value = value })
  onBeforeUnmount(release)
  return { widthLabel, smallMode, originalMode, setSizePreset, draftWidth, resizing, cornerResizing, displayWidth, beginResize, previewWidth, commitWidth, startCornerResize, moveCornerResize, finishCornerResize, cancelCornerResize, resizeWithKeyboard }
}
