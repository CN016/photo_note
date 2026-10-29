import { Extension, type Editor, type JSONContent } from '@tiptap/core'
import { Plugin, PluginKey } from '@tiptap/pm/state'
import { Decoration, DecorationSet } from '@tiptap/pm/view'

type Change = { add?: { id: string; position: number; element: HTMLElement; onDiscard: () => void }[]; remove?: string }
const key = new PluginKey<DecorationSet>('project-upload-placeholders')

/** 临时上传块只存在于编辑器装饰层；事务映射位置，不进入正文 JSON 或历史。 */
export const ProjectUploadPlaceholders = Extension.create({
  name: 'projectUploadPlaceholders',
  addProseMirrorPlugins() {
    return [new Plugin<DecorationSet>({
      key,
      state: {
        init: () => DecorationSet.empty,
        apply(transaction, previous) {
          let set = previous.map(transaction.mapping, transaction.doc, { onRemove: spec => { if (typeof spec.onDiscard === 'function') queueMicrotask(spec.onDiscard) } })
          const change = transaction.getMeta(key) as Change | undefined
          if (change?.remove) set = set.remove(set.find(undefined, undefined, spec => spec.id === change.remove))
          if (change?.add?.length) set = set.add(transaction.doc, change.add.map(item => Decoration.widget(item.position, item.element, { key: item.id, id: item.id, onDiscard: item.onDiscard, side: 1, ignoreSelection: true, stopEvent: () => true })))
          return set
        },
      },
      props: { decorations: state => key.getState(state) },
    })]
  },
})

/** 删除选区后同步显示全部占位块；后续正文编辑由 ProseMirror 映射块的位置。 */
export function addUploadPlaceholders(editor: Editor, items: { id: string; element: HTMLElement; onDiscard: () => void }[]): void {
  const transaction = editor.state.tr.deleteSelection()
  const position = transaction.selection.from
  transaction.setMeta(key, { add: items.map(item => ({ ...item, position })) } satisfies Change)
  editor.view.dispatch(transaction)
}

/** 失败块可移除；完成后在映射位置插入媒体并移除对应装饰。 */
export function removeUploadPlaceholder(editor: Editor, id: string): number | null {
  if (editor.isDestroyed) return null
  const decoration = key.getState(editor.state)?.find(undefined, undefined, spec => spec.id === id)[0]
  if (!decoration) return null
  editor.view.dispatch(editor.state.tr.setMeta('addToHistory', false).setMeta(key, { remove: id } satisfies Change))
  return decoration.from
}
export function hasUploadPlaceholder(editor: Editor, id: string): boolean {
  return !editor.isDestroyed && Boolean(key.getState(editor.state)?.find(undefined, undefined, spec => spec.id === id).length)
}
export function replaceUploadPlaceholder(editor: Editor, id: string, content: JSONContent): boolean {
  if (editor.isDestroyed) return false
  const decoration = key.getState(editor.state)?.find(undefined, undefined, spec => spec.id === id)[0]
  if (!decoration || !editor.commands.insertContentAt(decoration.from, content)) return false
  removeUploadPlaceholder(editor, id)
  return true
}
