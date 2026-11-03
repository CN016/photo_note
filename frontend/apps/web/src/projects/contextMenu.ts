import type { Editor } from '@tiptap/core'
import { ElMessageBox } from 'element-plus'
import { i18n } from '../i18n'
import { openMenu, type MenuEntry } from '../context-menu/menu'
import { clipboardAction } from '../context-menu/clipboard'
import { clipboardAdapter } from '../../../../packages/platform-adapters'

/** 正文菜单只调用现有命令；选区在菜单展开之前固定，异步返回不写入已变化的文档。 */
export function openProjectMenu(editor: Editor, event: MouseEvent, readOnly: boolean, insert: (kind: 'inspiration' | 'media' | 'attachment') => void, pasteFiles: (files: File[]) => void) {
  const t = (key: string) => i18n.global.t(`contextMenu.${key}`)
  const point = editor.view.posAtCoords({ left: event.clientX, top: event.clientY })
  const current = editor.state.selection
  if (point && (point.pos < current.from || point.pos > current.to || current.empty)) editor.commands.setTextSelection(point.pos)
  const selection = editor.state.selection.getBookmark(), document = editor.state.doc
  const restore = () => {
    if (editor.isDestroyed || editor.state.doc !== document) return false
    editor.view.dispatch(editor.state.tr.setSelection(selection.resolve(editor.state.doc)))
    return true
  }
  const action = (key: string, command: () => unknown, enabled = true, active = false): MenuEntry => ({ label: t(key), disabled: !enabled, active, run: () => { if (restore()) command() } })
  const selected = editor.state.doc.textBetween(editor.state.selection.from, editor.state.selection.to, '\n')
  const items: MenuEntry[] = [{ label: t('copy'), disabled: !selected || !clipboardAdapter.canWriteText(), run: () => clipboardAction(() => clipboardAdapter.writeText(selected)) }]
  if (readOnly) {
    const href = String(editor.getAttributes('link').href || '')
    if (/^(https?:\/\/|mailto:)/i.test(href)) items.push({ label: t('openLink'), run: () => { window.open(href, '_blank', 'noopener,noreferrer') } })
    openMenu(event.clientX, event.clientY, items, event.target as HTMLElement); return }
  const paste = () => clipboardAction(async () => {
    if (clipboardAdapter.canReadItems()) {
      const entries = await clipboardAdapter.readItems()
      const files: File[] = []
      let html = '', text = ''
      for (const entry of entries) {
        const image = entry.types.find(type => type.startsWith('image/'))
        if (image) files.push(new File([await entry.getType(image)], `clipboard.${image.split('/')[1]}`, { type: image }))
        else if (entry.types.includes('text/html')) html += await (await entry.getType('text/html')).text()
        else if (entry.types.includes('text/plain')) text += await (await entry.getType('text/plain')).text()
      }
      if (!restore()) return
      if (files.length) pasteFiles(files)
      else if (html) {
        // 外部 HTML 不得认领私有节点或任意 storekey；剩余结构由当前 schema 解析。
        const parsed = new DOMParser().parseFromString(html, 'text/html')
        parsed.querySelectorAll('figure[data-inspiration],figure[data-project-attachment],script,style,iframe,object').forEach(node => node.remove())
        parsed.querySelectorAll('*').forEach(node => {
          for (const attr of Array.from(node.attributes)) {
            const taskAttribute = (attr.name === 'data-type' && ['taskList', 'taskItem'].includes(attr.value)) || (attr.name === 'data-checked' && ['true', 'false'].includes(attr.value))
            if ((attr.name.startsWith('data-') && !taskAttribute) || attr.name.startsWith('on')) node.removeAttribute(attr.name)
          }
        })
        editor.view.pasteHTML(parsed.body.innerHTML)
      } else editor.view.pasteText(text)
    } else {
      const text = await clipboardAdapter.readText()
      if (restore()) editor.view.pasteText(text)
    }
  })
  items.push(
    { label: t('cut'), disabled: !selected || !clipboardAdapter.canWriteText(), run: () => clipboardAction(async () => { await clipboardAdapter.writeText(selected); if (restore()) editor.chain().focus().deleteSelection().run() }) },
    { label: t('paste'), disabled: !clipboardAdapter.canRead() && !clipboardAdapter.canReadItems(), run: paste },
    action('selectAll', () => editor.chain().focus().selectAll().run()),
    action('undo', () => editor.chain().focus().undo().run(), editor.can().undo()),
    action('redo', () => editor.chain().focus().redo().run(), editor.can().redo()),
  )
  const chain = () => editor.chain().focus()
  items.push({ label: t('textStyle'), children: [
    action('bold', () => chain().toggleBold().run(), true, editor.isActive('bold')),
    action('italic', () => chain().toggleItalic().run(), true, editor.isActive('italic')),
    action('underline', () => chain().toggleUnderline().run(), true, editor.isActive('underline')),
    action('strike', () => chain().toggleStrike().run(), true, editor.isActive('strike')),
    { label: t('fontSize'), children: [12, 14, 16, 18, 20, 24, 30, 36, 48].map(size => ({ label: `${size}px`, active: editor.getAttributes('textStyle').fontSize === `${size}px`, run: () => { if (restore()) chain().setFontSize(`${size}px`).run() } })) },
    ...(['color', 'highlight'] as const).map(kind => ({ label: t(kind), children: [
      action('reset', () => kind === 'color' ? chain().unsetColor().run() : chain().unsetHighlight().run()),
      ...(['black', 'red', 'orange', 'green', 'blue', 'purple', 'yellow'] as const).map((color, index) => {
        const value = ['#222222', '#d03030', '#b85c00', '#16803c', '#2563eb', '#7c3aed', '#f5d547'][index]!
        return action(`colors.${color}`, () => kind === 'color' ? chain().setColor(value).run() : chain().setHighlight({ color: value }).run(), true, kind === 'color' ? editor.isActive('textStyle', { color: value }) : editor.isActive('highlight', { color: value }))
      }),
    ] })),
    action('clearFormat', () => chain().unsetAllMarks().clearNodes().run()),
  ] })
  items.push({ label: t('paragraph'), children: [
    action('body', () => chain().setParagraph().run(), true, editor.isActive('paragraph')),
    ...([1, 2, 3, 4, 5, 6] as const).map(level => ({ label: i18n.global.t('contextMenu.heading', { level }), active: editor.isActive('heading', { level }), run: () => { if (restore()) chain().toggleHeading({ level }).run() } })),
    ...(['left', 'center', 'right', 'justify'] as const).map(align => action(align, () => chain().setTextAlign(align).run(), true, editor.isActive({ textAlign: align }))),
    action('bulletList', () => chain().toggleBulletList().run(), true, editor.isActive('bulletList')),
    action('orderedList', () => chain().toggleOrderedList().run(), true, editor.isActive('orderedList')),
    action('taskList', () => chain().toggleTaskList().run(), true, editor.isActive('taskList')),
    action('blockquote', () => chain().toggleBlockquote().run(), true, editor.isActive('blockquote')),
    action('codeBlock', () => chain().toggleCodeBlock().run(), true, editor.isActive('codeBlock')),
    action('horizontalRule', () => chain().setHorizontalRule().run()),
  ] })
  const href = String(editor.getAttributes('link').href || '')
  items.push({ label: t('link'), children: [
    { label: t(href ? 'editLink' : 'addLink'), run: async () => {
      try {
        const result = await ElMessageBox.prompt(t('linkAddress'), t('link'), { inputValue: href, inputPattern: /^(https?:\/\/|mailto:)[^\s]+$/i, inputErrorMessage: t('invalidLink'), confirmButtonText: t('confirm'), cancelButtonText: t('cancel') })
        if (restore()) chain().extendMarkRange('link').setLink({ href: result.value }).run()
      } catch { /* 取消输入保持正文不变。 */ }
    } },
    action('removeLink', () => chain().extendMarkRange('link').unsetLink().run(), Boolean(href)),
    { label: t('openLink'), disabled: !/^(https?:\/\/|mailto:)/i.test(href), run: () => { window.open(href, '_blank', 'noopener,noreferrer') } },
  ] })
  if (editor.isActive('table')) items.push({ label: t('table'), children: [
    action('rowBefore', () => chain().addRowBefore().run(), editor.can().addRowBefore()),
    action('rowAfter', () => chain().addRowAfter().run(), editor.can().addRowAfter()),
    action('columnBefore', () => chain().addColumnBefore().run(), editor.can().addColumnBefore()),
    action('columnAfter', () => chain().addColumnAfter().run(), editor.can().addColumnAfter()),
    action('deleteRow', () => chain().deleteRow().run(), editor.can().deleteRow()),
    action('deleteColumn', () => chain().deleteColumn().run(), editor.can().deleteColumn()),
    action('mergeCells', () => chain().mergeCells().run(), editor.can().mergeCells()),
    action('splitCell', () => chain().splitCell().run(), editor.can().splitCell()),
    action('headerRow', () => chain().toggleHeaderRow().run()),
    action('headerColumn', () => chain().toggleHeaderColumn().run()),
    action('deleteTable', () => chain().deleteTable().run()),
  ] })
  items.push({ label: t('insert'), children: [
    action('insertInspiration', () => insert('inspiration')),
    action('uploadMedia', () => insert('media')),
    action('uploadAttachment', () => insert('attachment')),
    action('insertTable', () => chain().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run()),
  ] })
  openMenu(event.clientX, event.clientY, items, event.target as HTMLElement)
}
