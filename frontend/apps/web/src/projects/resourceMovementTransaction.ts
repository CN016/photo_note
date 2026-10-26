import { NodeSelection, type EditorState, type Transaction } from '@tiptap/pm/state'

/** 从同一份文档快照交换媒体与同层相邻块；边界或非媒体入口不产生事务。 */
export function moveResourceTransaction(state: EditorState, pos: number, direction: -1 | 1): Transaction | null {
  if (pos < 0 || pos >= state.doc.content.size) return null
  const resolved = state.doc.resolve(pos)
  const node = resolved.nodeAfter
  if (!node || !['inspirationImage', 'projectAttachment'].includes(node.type.name)) return null
  const adjacent = direction === -1 ? resolved.nodeBefore : resolved.parent.maybeChild(resolved.index() + 1)
  if (!adjacent) return null
  const from = direction === -1 ? pos - adjacent.nodeSize : pos
  const to = direction === -1 ? pos + node.nodeSize : pos + node.nodeSize + adjacent.nodeSize
  const content = direction === -1 ? [node, adjacent] : [adjacent, node]
  const transaction = state.tr.replaceWith(from, to, content)
  const nextPosition = direction === -1 ? from : from + adjacent.nodeSize
  transaction.setSelection(NodeSelection.create(transaction.doc, nextPosition)).scrollIntoView()
  return transaction
}

/** 长按拖动只接受同父级块边界，整体搬移媒体并保留撤销和节点选择。 */
export function moveResourceToPositionTransaction(state: EditorState, source: number, target: number): Transaction | null {
  if (source < 0 || target < 0 || source >= state.doc.content.size || target > state.doc.content.size) return null
  const origin = state.doc.resolve(source)
  const destination = state.doc.resolve(target)
  const node = origin.nodeAfter
  if (!node || !['inspirationImage', 'projectAttachment'].includes(node.type.name)
    || origin.parent !== destination.parent || destination.textOffset !== 0
    || (target >= source && target <= source + node.nodeSize)) return null
  const tr = state.tr.delete(source, source + node.nodeSize)
  const insertAt = tr.mapping.map(target)
  return tr.insert(insertAt, node).setSelection(NodeSelection.create(tr.doc, insertAt))
}
