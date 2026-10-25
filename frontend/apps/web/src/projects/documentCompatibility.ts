import type { Schema } from '@tiptap/pm/model'

/** 检查 V1 JSON 的节点、标记、属性与内容结构，遇到未知内容时拒绝编辑，避免静默丢失。 */
export function isDocumentCompatible(document: Record<string, unknown>, schema: Schema): boolean {
  try {
    function check(value: unknown): void {
      if (!value || typeof value !== 'object') throw new Error('invalid_node')
      const node = value as { type: string; attrs?: Record<string, unknown>; marks?: { type: string; attrs?: Record<string, unknown> }[]; content?: unknown[] }
      const type = schema.nodes[node.type]
      if (!type) throw new Error('unknown_node')
      for (const key of Object.keys(node.attrs || {})) if (!(key in (type.spec.attrs || {}))) throw new Error('unknown_attribute')
      for (const mark of node.marks || []) {
        const markType = schema.marks[mark.type]
        if (!markType) throw new Error('unknown_mark')
        for (const key of Object.keys(mark.attrs || {})) if (!(key in (markType.spec.attrs || {}))) throw new Error('unknown_mark_attribute')
      }
      for (const child of node.content || []) check(child)
    }
    check(document)
    schema.nodeFromJSON(document).check()
    return true
  } catch { return false }
}
