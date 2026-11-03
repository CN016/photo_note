import { ElMessage } from 'element-plus'
import { i18n } from '../i18n'
import { clipboardAdapter } from '../../../../packages/platform-adapters'

/** 所有调用源共用简洁失败反馈，不记录剪贴板内容或资源地址。 */
export async function clipboardAction(action: () => Promise<void>): Promise<void> {
  try { await action() } catch { ElMessage.warning(i18n.global.t('contextMenu.clipboardFailed')) }
}

/** 在点击时请求授权并转成通用 PNG；只处理已有授权读取能力提供的 Blob。 */
export function copyImage(load: () => Promise<Blob>): Promise<void> {
  return clipboardAction(() => clipboardAdapter.writeImage((async () => {
    const bitmap = await createImageBitmap(await load())
    try {
      const canvas = document.createElement('canvas')
      canvas.width = bitmap.width; canvas.height = bitmap.height
      try {
        const context = canvas.getContext('2d')
        if (!context) throw new Error('canvas_unavailable')
        context.drawImage(bitmap, 0, 0)
        return await new Promise<Blob>((resolve, reject) => canvas.toBlob(blob => blob ? resolve(blob) : reject(new Error('encode_failed')), 'image/png'))
      } finally { canvas.width = 0; canvas.height = 0 }
    } finally { bitmap.close() }
  })()))
}

/** 快照输入选区，右键浮层获得焦点后仍对原输入框操作；密码不允许复制或剪切。 */
export function defaultEntries(target: HTMLElement) {
  const t = i18n.global.t
  const input = target.closest('input,textarea') as HTMLInputElement | HTMLTextAreaElement | null
  const originalValue = input?.value
  const editable = input && !input.disabled && !input.readOnly
  const start = input?.selectionStart ?? 0
  const end = input?.selectionEnd ?? 0
  const secret = input instanceof HTMLInputElement && input.type === 'password'
  const selected = input ? input.value.slice(start, end) : window.getSelection()?.toString() || ''
  const restore = () => { input?.focus({ preventScroll: true }); try { input?.setSelectionRange(start, end) } catch { /* 非文本输入不支持选区。 */ } }
  const replace = (value: string) => {
    if (!input?.isConnected || !editable || input.disabled || input.readOnly || input.value !== originalValue) return
    restore(); input.setRangeText(value, start, end, 'end')
    input.dispatchEvent(new Event('input', { bubbles: true }))
  }
  const items = [{ label: t('contextMenu.copy'), disabled: secret || !selected || !clipboardAdapter.canWriteText(), run: () => clipboardAction(() => clipboardAdapter.writeText(selected)) }]
  if (input) items.push(
    { label: t('contextMenu.cut'), disabled: secret || !editable || !selected || !clipboardAdapter.canWriteText(), run: () => clipboardAction(async () => { await clipboardAdapter.writeText(selected); replace('') }) },
    { label: t('contextMenu.paste'), disabled: !editable || input.selectionStart === null || !clipboardAdapter.canRead(), run: () => clipboardAction(async () => replace(await clipboardAdapter.readText())) },
    { label: t('contextMenu.selectAll'), disabled: input.disabled, run: async () => { input.focus(); input.select() } },
  )
  return items
}
