import { describe, expect, it } from 'vitest'
import { fileExportFeedbackKey } from './fileExportFeedback'

describe('文件导出错误提示', () => {
  it('只提示用户可操作的明确错误', () => {
    expect(fileExportFeedbackKey('export_too_large')).toBe('common.fileExport.tooLarge')
    expect(fileExportFeedbackKey('album_permission_denied')).toBe('common.fileExport.albumPermissionDenied')
  })

  it('忙碌、读取竞态和未知平台异常不显示通用失败弹窗', () => {
    expect(fileExportFeedbackKey('export_busy')).toBeNull()
    expect(fileExportFeedbackKey('export_read_failed')).toBeNull()
    expect(fileExportFeedbackKey('export_failed')).toBeNull()
    expect(fileExportFeedbackKey('native_timeout')).toBeNull()
  })
})
