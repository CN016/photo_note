import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const nativeExport = vi.fn()
import { MAX_NATIVE_EXPORT_BYTES, platformAdapter } from './index'
const labels = { title: 'Export', save: 'Save', share: 'Share', cancel: 'Cancel' }

describe('平台文件导出边界', () => {
  beforeEach(() => {
    vi.stubGlobal('browser', { runtime: { id: 'photo-nini@imcn016.love', sendNativeMessage: nativeExport } })
  })
  afterEach(() => { vi.unstubAllGlobals(); nativeExport.mockReset() })

  it('超过原生上限的 Blob 在读取前拒绝，避免额外内存分配', async () => {
    const read = vi.fn()
    vi.stubGlobal('FileReader', read)
    await expect(platformAdapter.exportFile({ size: MAX_NATIVE_EXPORT_BYTES + 1 } as Blob, 'large.png', labels)).rejects.toThrow('export_too_large')
    expect(read).not.toHaveBeenCalled()
    expect(nativeExport).not.toHaveBeenCalled()
  })

  it('传递当前语言、文件类型和内容，取消保持为取消状态', async () => {
    vi.stubGlobal('FileReader', class {
      result = 'data:application/json;base64,e30='
      onload?: () => void
      readAsDataURL() { this.onload?.() }
    })
    nativeExport.mockResolvedValue({ status: 'cancelled' })
    expect(await platformAdapter.exportFile(new Blob(['{}'], { type: 'application/json;charset=utf-8' }), 'ledger.json', labels)).toBe('cancelled')
    expect(nativeExport).toHaveBeenCalledWith('photo_nini', { method: 'exportFile', options: { ...labels, filename: 'ledger.json', mime: 'application/json', data: 'e30=' } })
  })

  it('文件读取失败传回界面，不打开原生面板', async () => {
    vi.stubGlobal('FileReader', class { onerror?: () => void; readAsDataURL() { this.onerror?.() } })
    await expect(platformAdapter.exportFile(new Blob(['x']), 'file.txt', labels)).rejects.toThrow('export_read_failed')
    expect(nativeExport).not.toHaveBeenCalled()
  })
})
