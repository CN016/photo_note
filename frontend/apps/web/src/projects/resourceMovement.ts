import { computed, onBeforeUnmount, ref } from 'vue'
import type { NodeViewProps } from '@tiptap/vue-3'
import { moveResourceTransaction } from './resourceMovementTransaction'

/** 媒体的键盘/触屏排序：仅交换同一父容器内相邻节点，保留节点属性和单次撤销事务。 */
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
    props.editor.view.focus()
  }
  return { canMoveBefore: computed(() => !!boundary.value?.before), canMoveAfter: computed(() => !!boundary.value?.after), move }
}
