import type { Router } from 'vue-router'
import { ElMessage } from 'element-plus'
import { isAndroid, nativeShell, platformAdapter, minimizeApp, subscribeNativeBack, type ExportResult } from '../../../packages/platform-adapters'
import { i18n } from './i18n'

let exporting = false
/** 标记已向用户反馈的导出错误，业务页只补充自己的加载错误，避免双重提示。 */
export class FileExportError extends Error {}

/** 导出状态覆盖整个系统面板生命周期，避免重复点击同时持有多份大文件。 */
export async function exportFile(blob: Blob, filename: string): Promise<ExportResult> {
  const t = i18n.global.t
  if (exporting) { ElMessage.info(t('common.fileExport.busy')); return 'cancelled' }
  exporting = true
  try {
    const result = await platformAdapter.exportFile(blob, filename, {
      title: t('common.fileExport.title'), save: t('common.fileExport.save'),
      share: t('common.fileExport.share'), cancel: t('common.fileExport.cancel'),
    })
    if (result === 'saved' && isAndroid()) ElMessage.success(t('common.fileExport.saved'))
    return result
  } catch (error) {
    ElMessage.error(t(error instanceof Error && error.message === 'export_too_large' ? 'common.fileExport.tooLarge' : 'common.fileExport.failed'))
    throw new FileExportError('export_failed')
  } finally { exporting = false }
}

/** 返回键先关闭最上层可关闭浮层，再走路由守卫；首页回到系统而不销毁登录态。 */
export function closeTopOverlay(): boolean {
  const overlays = [...document.querySelectorAll<HTMLElement>('.el-overlay, .el-image-viewer__wrapper')]
    .filter(node => node.getClientRects().length > 0)
  const top = overlays.sort((a, b) => Number(getComputedStyle(a).zIndex) - Number(getComputedStyle(b).zIndex)).at(-1)
  if (!top) return false
  const close = top.querySelector<HTMLElement>('.el-message-box__headerbtn, .el-dialog__headerbtn, .el-drawer__close-btn, .el-image-viewer__close, .preview-heading button')
  if (close && getComputedStyle(close).display !== 'none') close.click()
  else document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', code: 'Escape', bubbles: true }))
  return true
}

/** 平台壳只接管系统能力，权限和业务数据仍由共享 Web 与后端决定。 */
export function initializePlatform(router: Router): void {
  if (!isAndroid()) return
  document.documentElement.classList.add('platform-android')
  const syncTheme = () => { void nativeShell.setTheme({ dark: document.documentElement.dataset.theme === 'dark' }).catch(() => {}) }
  syncTheme()
  new MutationObserver(syncTheme).observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
  subscribeNativeBack(() => {
    if (closeTopOverlay()) return
    const active = document.activeElement
    if (active instanceof HTMLElement && active.matches('input, textarea, [contenteditable="true"]')) { active.blur(); return }
    if (['/workbench', '/inspirations', '/login', '/maintenance'].includes(router.currentRoute.value.path)) { void minimizeApp(); return }
    if (router.options.history.state.back) router.back()
    else void router.replace('/workbench')
  })
}
