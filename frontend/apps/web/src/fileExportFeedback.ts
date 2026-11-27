/** 文件导出时可向用户展示的明确错误类型；其余平台异常不弹通用失败提示。 */
export type FileExportFeedbackKey = 'common.fileExport.tooLarge' | 'common.fileExport.albumPermissionDenied'

/**
 * 将平台错误映射为可操作提示。
 *
 * `export_busy`、`export_read_failed`、`export_failed` 等未知或竞态错误可能是
 * 重复点击、页面切换或原生异步回包造成，不能据此断言用户文件没有保存，因此
 * 返回 null，不向当前页面弹出误导性的通用失败提示。
 */
export function fileExportFeedbackKey(message: string): FileExportFeedbackKey | null {
  if (message.includes('export_too_large')) return 'common.fileExport.tooLarge'
  if (message.includes('album_permission_denied')) return 'common.fileExport.albumPermissionDenied'
  return null
}
