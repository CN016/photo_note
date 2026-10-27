
/** 系统文件面板的文案由共享界面按当前语言传入，适配层不持有业务文案。 */
export interface ExportLabels { title: string; save: string; share: string; cancel: string }
export type ExportResult = 'saved' | 'shared' | 'cancelled'
export interface PlatformAdapter {
  exportFile(blob: Blob, filename: string, labels: ExportLabels): Promise<ExportResult>
}
interface NativeShell {
  exportFile(options: ExportLabels & { data: string; filename: string; mime: string }): Promise<{ status: ExportResult }>
  setTheme(options: { dark: boolean }): Promise<void>
}
interface BrowserRuntime {
  id: string
  sendNativeMessage(app: string, message: { method: string; options: unknown }): Promise<unknown>
  connectNative(app: string): { onMessage: { addListener(callback: (message: { type?: string }) => void): void } }
}
/** APK固定扩展及其受来源校验的本机页面暴露此API；普通网页沿用Web能力。 */
function runtime(): BrowserRuntime | undefined {
  return (globalThis as typeof globalThis & { browser?: { runtime?: BrowserRuntime } }).browser?.runtime
}
export const isAndroid = () => runtime()?.id === 'photo-nini@imcn016.love'
function invoke<T>(method: string, options: unknown): Promise<T> {
  const bridge = runtime()
  if (!isAndroid() || !bridge) return Promise.reject(new Error('native_unavailable'))
  return bridge.sendNativeMessage('photo_nini', { method, options }) as Promise<T>
}
export const nativeShell: NativeShell = {
  exportFile: options => invoke('exportFile', options),
  setTheme: options => invoke('setTheme', options),
}
export const minimizeApp = () => invoke<void>('minimizeApp', {})
/** 长连接只传系统返回事件，业务载荷用带明确响应的一次性消息。 */
export function subscribeNativeBack(callback: () => void): void {
  const port = runtime()?.connectNative('photo_nini')
  port?.onMessage.addListener(message => { if (message.type === 'back') callback() })
}
/** 原生桥会暂存 Base64；限制单次大小，避免大图导出产生不可控的内存峰值。 */
export const MAX_NATIVE_EXPORT_BYTES = 32 * 1024 * 1024

/** 使用 FileReader 避免展开整个字节数组；读取失败必须传回业务界面。 */
function encodeBlob(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(new Error('export_read_failed'))
    reader.onload = () => resolve(String(reader.result).split(',')[1] || '')
    reader.readAsDataURL(blob)
  })
}

/** 普通浏览器沿用下载语义；Android 由系统文件选择器或临时授权分享完成导出。 */
export const platformAdapter: PlatformAdapter = {
  async exportFile(blob, filename, labels) {
    if (isAndroid()) {
      if (blob.size > MAX_NATIVE_EXPORT_BYTES) throw new Error('export_too_large')
      const { status } = await nativeShell.exportFile({ ...labels, filename, mime: blob.type.split(';')[0] || 'application/octet-stream', data: await encodeBlob(blob) })
      return status
    }
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = filename
    anchor.click()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
    return 'saved'
  },
}
