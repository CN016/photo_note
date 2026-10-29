import { computed, onBeforeUnmount, ref } from 'vue'
import type { NodeViewProps } from '@tiptap/vue-3'
import { NodeSelection } from '@tiptap/pm/state'
import { useI18n } from 'vue-i18n'
import { resourceDropSide, usesTouchResourceDrag } from './resourceTouchInteraction'
import { moveResourceTransaction, moveResourceToPositionTransaction } from './resourceMovementTransaction'

/** 媒体的鼠标、键盘与触屏排序：仅在同一父容器内移动完整节点，保留属性与单次撤销事务。 */
export function useResourceMovement(props: NodeViewProps) {
  const { t } = useI18n({ useScope: 'global' })
  const revision = ref(0)
  const refresh = () => { revision.value += 1 }
  props.editor.on('transaction', refresh)
  onBeforeUnmount(() => props.editor.off('transaction', refresh))
  const boundary = computed(() => {
    void revision.value
    const pos = props.getPos()
    if (typeof pos !== 'number') return null
    const resolved = props.editor.state.doc.resolve(pos)
    return { pos, before: resolved.nodeBefore, after: resolved.parent.maybeChild(resolved.index() + 1) }
  })
  function move(direction: -1 | 1) {
    // Vue 节点视图中的 node/getPos 会随渲染更新；事务必须从同一份当前 state 取完整节点快照。
    const pos = props.getPos()
    if (typeof pos !== 'number') return
    const transaction = moveResourceTransaction(props.editor.state, pos, direction)
    if (!transaction) return
    props.editor.view.dispatch(transaction)
    if (!touchDevice) props.editor.view.focus()
  }
  let hold: ReturnType<typeof setTimeout> | undefined
  const dragging = ref(false)
  const touchDevice = usesTouchResourceDrag()
  let touchID: number | null = null
  let startX = 0
  let startY = 0
  let lastX = 0
  let lastY = 0
  let targetPosition: number | null = null
  let sourceElement: HTMLElement | null = null
  let ghost: HTMLElement | null = null
  let indicator: HTMLElement | null = null
  let scrollFrame = 0
  let mouseActive = false
  let suppressClick = false

  /** 同步撤去正文焦点，不调用会异步清空 DOM 选区的 blur 命令，避免节点选择闪退。 */
  function blurText() { props.editor.view.dom.blur() }
  function selectResource(event: Event) {
    if (event.type === 'click' && suppressClick) { suppressClick = false; event.preventDefault(); event.stopPropagation(); return }
    if ((event.target as HTMLElement).closest('button,input,video,.resource-controls,.resource-resize-handle')) return
    if (event.cancelable) event.preventDefault()
    event.stopPropagation()
    const pos = props.getPos()
    if (typeof pos !== 'number') return
    blurText()
    props.editor.view.dispatch(props.editor.state.tr.setSelection(NodeSelection.create(props.editor.state.doc, pos)))
  }

  /** 每次移动都从当前文档的同层节点几何计算落点，不把光标命中当成文本插入位置。 */
  function previewDrop(x: number, y: number) {
    const view = props.editor.view
    const source = props.getPos()
    if (typeof source !== 'number') return
    // 编辑区域以外释放不应误命中最近块，避免拖到侧栏时意外改序。
    const editorRect = view.dom.getBoundingClientRect()
    if (x < editorRect.left || x > editorRect.right || y < editorRect.top || y > editorRect.bottom) {
      targetPosition = null
      if (indicator) indicator.hidden = true
      return
    }
    const origin = view.state.doc.resolve(source)
    const parentStart = origin.start()
    let offset = 0
    let nearest: { position: number; rect: DOMRect; distance: number; nodeSize: number; inline: boolean } | null = null
    origin.parent.forEach((node) => {
      const position = parentStart + offset
      offset += node.nodeSize
      const element = view.nodeDOM(position)
      if (!(element instanceof HTMLElement)) return
      const rect = element.getBoundingClientRect()
      if (!rect.width || !rect.height) return
      const dx = Math.max(rect.left - x, 0, x - rect.right)
      const dy = Math.max(rect.top - y, 0, y - rect.bottom)
      const distance = dx * dx + dy * dy
      const inline = element.classList.contains('project-resource-node') && rect.width < view.dom.clientWidth * .7
      if (!nearest || distance < nearest.distance) nearest = { position, rect, distance, nodeSize: node.nodeSize, inline }
    })
    // forEach 的闭包赋值由运行时完成，单独缩窄可视候选的类型。
    const hit = nearest as { position: number; rect: DOMRect; distance: number; nodeSize: number; inline: boolean } | null
    targetPosition = null
    if (!hit || !indicator) return
    const side = resourceDropSide(hit.rect, x, y, hit.inline)
    const target = hit.position + (side === 'after' ? hit.nodeSize : 0)
    if (!moveResourceToPositionTransaction(view.state, source, target)) { indicator.hidden = true; return }
    targetPosition = target
    indicator.hidden = false
    indicator.textContent = t('projects.dropHere')
    const rect = hit.rect
    Object.assign(indicator.style, hit.inline ? {
      left: `${side === 'before' ? rect.left : rect.right}px`, top: `${rect.top}px`, width: '3px', height: `${rect.height}px`,
    } : {
      left: `${rect.left}px`, top: `${side === 'before' ? rect.top : rect.bottom}px`, width: `${rect.width}px`, height: '3px',
    })
  }
  function updateDrag() {
    if (ghost) { ghost.style.left = `${lastX + 12}px`; ghost.style.top = `${lastY + 12}px` }
    previewDrop(lastX, lastY)
  }
  /** 拖到视口边缘时持续滚页，并在滚动后的新几何上刷新落点。 */
  function autoScroll() {
    if (!dragging.value) return
    const viewport = window.visualViewport
    const top = viewport?.offsetTop ?? 0
    const bottom = top + (viewport?.height ?? innerHeight) - 80
    const delta = lastY < top + 60 ? -12 : lastY > bottom - 60 ? 12 : 0
    if (delta) { window.scrollBy(0, delta); updateDrag() }
    scrollFrame = requestAnimationFrame(autoScroll)
  }
  function activateDrag() {
    if (!sourceElement || (touchID === null && !mouseActive)) return
    blurText()
    dragging.value = true
    const rect = sourceElement.getBoundingClientRect()
    ghost = sourceElement.cloneNode(true) as HTMLElement
    ghost.querySelectorAll('.resource-controls,.resource-resize-handle').forEach(element => element.remove())
    ghost.querySelectorAll('[id]').forEach(element => element.removeAttribute('id'))
    ghost.removeAttribute('id')
    ghost.className = 'project-resource-drag-ghost'
    ghost.setAttribute('aria-hidden', 'true')
    Object.assign(ghost.style, { width: `${Math.min(220, rect.width)}px`, maxHeight: '160px', overflow: 'hidden' })
    document.body.appendChild(ghost)
    indicator = document.createElement('div')
    indicator.className = 'project-resource-drop-indicator'
    indicator.setAttribute('role', 'status')
    indicator.hidden = true
    document.body.appendChild(indicator)
    updateDrag()
    autoScroll()
  }
  /** 文档级触摸监听不依赖图片节点的重绘；手势期间屏蔽原生图片拖放和兼容鼠标聚焦。 */
  function beginTouch(event: TouchEvent, handle: boolean) {
    const target = event.target as HTMLElement
    if (!handle && target.closest('button,input,video,.resource-controls,.resource-resize-handle')) return
    if (event.touches.length !== 1) { cancelTouchDrag(); return }
    cancelTouchDrag()
    event.stopPropagation()
    if (handle && event.cancelable) event.preventDefault()
    const touch = event.touches[0]!
    touchID = touch.identifier
    startX = lastX = touch.clientX; startY = lastY = touch.clientY
    sourceElement = target.closest('.project-resource-node')
    blurText()
    const pos = props.getPos()
    if (typeof pos === 'number') props.editor.view.dispatch(props.editor.state.tr.setSelection(NodeSelection.create(props.editor.state.doc, pos)))
    document.addEventListener('touchmove', moveTouch, { capture: true, passive: false })
    document.addEventListener('touchend', endTouch, { capture: true, passive: false })
    document.addEventListener('touchcancel', cancelTouchDrag, true)
    window.addEventListener('blur', cancelTouchDrag)
    hold = setTimeout(activateDrag, 350)
  }
  function startBlockTouch(event: TouchEvent) { beginTouch(event, false) }
  /** PC 左键按住媒体后移动超过阈值才进入拖动，短点击和双击仍使用原有选择与查看逻辑。 */
  function startMouseDrag(event: MouseEvent) {
    if (event.button !== 0 || !props.editor.isEditable) return
    const target = event.target as HTMLElement
    if (target.closest('button,input,video,.resource-controls,.resource-resize-handle')) return
    cancelTouchDrag()
    selectResource(event)
    sourceElement = target.closest('.project-resource-node')
    if (!sourceElement) return
    mouseActive = true
    startX = lastX = event.clientX; startY = lastY = event.clientY
    document.addEventListener('mousemove', moveMouse, true)
    document.addEventListener('mouseup', endMouse, true)
    window.addEventListener('blur', cancelTouchDrag)
  }
  function moveMouse(event: MouseEvent) {
    if (!mouseActive) return
    lastX = event.clientX; lastY = event.clientY
    if (!dragging.value && Math.hypot(lastX - startX, lastY - startY) >= 5) activateDrag()
    if (dragging.value) { event.preventDefault(); event.stopPropagation(); updateDrag() }
  }
  function endMouse(event: MouseEvent) {
    if (!mouseActive || event.button !== 0) return
    lastX = event.clientX; lastY = event.clientY
    if (dragging.value) updateDrag()
    const source = props.getPos(); const target = targetPosition; const moved = dragging.value
    cancelTouchDrag()
    if (!moved) return
    event.preventDefault(); event.stopPropagation()
    suppressClick = true
    // 若浏览器没有派发 click，不影响下一次正常选择。
    window.setTimeout(() => { suppressClick = false }, 0)
    if (typeof source === 'number' && target !== null) {
      const tr = moveResourceToPositionTransaction(props.editor.state, source, target)
      if (tr) props.editor.view.dispatch(tr)
    }
  }
  function moveTouch(event: TouchEvent) {
    const touch = Array.from(event.touches).find(item => item.identifier === touchID)
    if (!touch) { cancelTouchDrag(); return }
    lastX = touch.clientX; lastY = touch.clientY
    if (!dragging.value) {
      if (Math.hypot(lastX - startX, lastY - startY) > 10) cancelTouchDrag()
      return
    }
    if (event.cancelable) event.preventDefault()
    event.stopPropagation()
    updateDrag()
  }
  function endTouch(event: TouchEvent) {
    const touch = Array.from(event.changedTouches).find(item => item.identifier === touchID)
    if (!touch) return
    // 即使没有拖动，也阻止 touchend 合成 mousedown 后再次聚焦编辑器。
    if (event.cancelable) event.preventDefault()
    event.stopPropagation()
    blurText()
    const source = props.getPos()
    const target = targetPosition
    const wasDragging = dragging.value
    cancelTouchDrag()
    if (wasDragging && typeof source === 'number' && target !== null) {
      const tr = moveResourceToPositionTransaction(props.editor.state, source, target)
      if (tr) props.editor.view.dispatch(tr)
    }
  }
  function cancelTouchDrag() {
    clearTimeout(hold)
    cancelAnimationFrame(scrollFrame)
    document.removeEventListener('mousemove', moveMouse, true)
    document.removeEventListener('mouseup', endMouse, true)
    mouseActive = false
    document.removeEventListener('touchmove', moveTouch, true)
    document.removeEventListener('touchend', endTouch, true)
    document.removeEventListener('touchcancel', cancelTouchDrag, true)
    window.removeEventListener('blur', cancelTouchDrag)
    ghost?.remove(); indicator?.remove()
    ghost = null; indicator = null; targetPosition = null; touchID = null; sourceElement = null
    dragging.value = false
  }
  onBeforeUnmount(cancelTouchDrag)
  return { touchDevice, touchDragging: dragging, startBlockTouch, startMouseDrag, selectResource,
    canMoveBefore: computed(() => !!boundary.value?.before), canMoveAfter: computed(() => !!boundary.value?.after), move }
}
