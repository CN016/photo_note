import { nextTick, onBeforeUnmount, onMounted } from 'vue'
import type { NodeViewProps } from '@tiptap/vue-3'
import { ElMessageBox } from 'element-plus'
import { registerMenu, openMenu, type MenuEntry } from '../context-menu/menu'
import { copyImage } from '../context-menu/clipboard'
import { clipboardAdapter, platformAdapter } from '../../../../packages/platform-adapters'
import { i18n } from '../i18n'

interface ResourceActions {
  readOnly: () => boolean
  image: () => boolean
  load: () => Promise<Blob>
  view: () => void
  details?: () => void
  filename: () => string
  size: (small: boolean) => void
}

/** 每个节点直接登记实际 DOM，根捕获入口即可优先命中，避免右键冒泡被编辑器截断。 */
export function useResourceContextMenu(props: NodeViewProps, options: ResourceActions) {
  let dispose: (() => void) | undefined
  let alive = true
  onMounted(async () => {
    await nextTick()
    const position = props.getPos()
    if (!alive || typeof position !== 'number') return
    const element = props.editor.view.nodeDOM(position)
    if (!(element instanceof HTMLElement)) return
    dispose = registerMenu(element, event => {
      const position = props.getPos()
      if (typeof position !== 'number') return false
      props.editor.commands.setNodeSelection(position)
      const node = props.node
      const valid = () => !props.editor.isDestroyed && element.isConnected && props.node === node
      const t = (key: string) => i18n.global.t(`contextMenu.${key}`)
      const items: MenuEntry[] = [{ label: t(options.image() ? 'viewImage' : 'preview'), run: () => { if (valid()) options.view() } }]
      if (options.details) items.push({ label: t('details'), run: () => { if (valid()) options.details?.() } })
      if (options.image()) items.push({ label: t('copyImage'), disabled: !clipboardAdapter.canWriteImage(), run: () => copyImage(options.load) })
      items.push({ label: t('download'), run: async () => {
        const blob = await options.load()
        if (valid()) await platformAdapter.exportFile(blob, options.filename(), { title: t('download'), save: t('download'), share: t('share'), cancel: t('cancel') })
      } })
      if (!options.readOnly()) {
        items.push({ label: t('size'), children: [
          { label: t('small'), active: Number(node.attrs.width) < 34, run: () => { if (valid()) options.size(true) } },
          { label: t('fullWidth'), active: Number(node.attrs.width) === 100, run: () => { if (valid()) options.size(false) } },
          { label: t('customWidth'), run: async () => {
            try {
              const result = await ElMessageBox.prompt(t('widthRange'), t('customWidth'), { inputValue: String(Math.round(Number(node.attrs.width) || 100)), inputPattern: /^(?:[3-9]\d|100)$/, inputErrorMessage: t('widthRange'), confirmButtonText: t('confirm'), cancelButtonText: t('cancel') })
              if (valid() && !options.readOnly()) props.updateAttributes({ width: Number(result.value), layout: Number(result.value) === 100 ? 'original' : 'small' })
            } catch { /* 取消尺寸输入不写正文。 */ }
          } },
        ] })
        items.push({ label: t('removeReference'), danger: true, run: () => { if (valid() && !options.readOnly()) props.deleteNode() } })
      }
      openMenu(event.clientX, event.clientY, items, element)
      return true
    })
  })
  onBeforeUnmount(() => { alive = false; dispose?.() })
}
