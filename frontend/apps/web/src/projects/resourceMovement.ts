import { computed, onBeforeUnmount, ref } from 'vue'
import type { NodeViewProps } from '@tiptap/vue-3'
import { NodeSelection } from '@tiptap/pm/state'
import { moveResourceTransaction, moveResourceToPositionTransaction } from './resourceMovementTransaction'

/** 媒体的键盘与触屏排序：仅在同一父容器内移动完整节点，保留属性与单次撤销事务。 */
export function useResourceMovement(props: NodeViewProps) {
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
    if (!window.matchMedia('(pointer: coarse)').matches) props.editor.view.focus()
  }
  let hold: ReturnType<typeof setTimeout> | undefined
  const dragging = ref(false)
  let startX = 0
  let startY = 0
  /** 媒体只改变节点选择；主动收起正文焦点，避免原子节点点击唤起输入法。 */
  function selectResource(event: PointerEvent) {
    if ((event.target as HTMLElement).closest('button,input,video,.resource-controls,.resource-resize-handle')) return
    const pos = props.getPos()
    if (typeof pos !== 'number') return
    event.preventDefault()
    props.editor.commands.blur()
    props.editor.view.dispatch(props.editor.state.tr.setSelection(NodeSelection.create(props.editor.state.doc, pos)))
  }
  /** 手柄保留指针捕获，长按后只移动完整节点；取消或离开组件时清理计时器。 */
  function startTouchDrag(event: PointerEvent) {
    if (event.pointerType === 'mouse') return
    event.preventDefault()
    startX = event.clientX; startY = event.clientY
    const handle = event.currentTarget as HTMLElement
    handle.setPointerCapture(event.pointerId)
    hold = setTimeout(() => { dragging.value = true; props.editor.commands.blur() }, 350)
  }
  function finishTouchDrag(event: Pick<PointerEvent, 'clientX' | 'clientY' | 'preventDefault'>) {
    clearTimeout(hold)
    if (!dragging.value) return
    dragging.value = false
    event.preventDefault()
    const view = props.editor.view
    const source = props.getPos()
    const hit = view.posAtCoords({ left: event.clientX, top: event.clientY })
    if (typeof source !== 'number' || !hit || Math.hypot(event.clientX - startX, event.clientY - startY) < 10) return
    const resolved = view.state.doc.resolve(hit.pos)
    // 仅在同一父级移动，避免拖入列表或表格时破坏节点结构。
    const origin = view.state.doc.resolve(source)
    if (resolved.depth < origin.depth || resolved.node(origin.depth) !== origin.parent) return
    let target = resolved.depth > origin.depth ? resolved.before(origin.depth + 1) : hit.pos
    const targetNode = view.state.doc.nodeAt(target)
    const targetDOM = view.nodeDOM(target)
    if (targetNode && targetDOM instanceof HTMLElement) {
      const rect = targetDOM.getBoundingClientRect()
      if (event.clientY > (rect.top + rect.bottom) / 2) target += targetNode.nodeSize
    }
    const tr = moveResourceToPositionTransaction(view.state, source, target)
    if (tr) view.dispatch(tr)
  }
  /** 整个媒体表面也支持长按：短滑动继续滚页，长按成立后才阻止原生滚动。 */
  function startBlockTouch(event: TouchEvent) {
    if ((event.target as HTMLElement).closest('button,input,video,.resource-controls,.resource-resize-handle')) return
    if (event.touches.length !== 1) return
    const touch = event.touches[0]!
    startX = touch.clientX; startY = touch.clientY
    hold = setTimeout(() => { dragging.value = true; props.editor.commands.blur() }, 350)
  }
  function moveBlockTouch(event: TouchEvent) {
    const touch = event.touches[0]
    if (!touch) return
    if (dragging.value) event.preventDefault()
    else if (Math.hypot(touch.clientX - startX, touch.clientY - startY) > 10) clearTimeout(hold)
  }
  function finishBlockTouch(event: TouchEvent) {
    const touch = event.changedTouches[0]
    if (touch) finishTouchDrag({ clientX: touch.clientX, clientY: touch.clientY, preventDefault: () => event.preventDefault() })
    else cancelTouchDrag()
  }
  function cancelTouchDrag() { clearTimeout(hold); dragging.value = false }
  onBeforeUnmount(cancelTouchDrag)
  return { touchDragging: dragging, startBlockTouch, moveBlockTouch, finishBlockTouch, selectResource, startTouchDrag, finishTouchDrag, cancelTouchDrag, canMoveBefore: computed(() => !!boundary.value?.before), canMoveAfter: computed(() => !!boundary.value?.after), move }
}
