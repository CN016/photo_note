import { api } from '../api/client'

export const fallbackProjectUploadLimit = 50 * 1024 * 1024

/** 上传前读取公开文件上限；读取失败时仍由后端强制校验，页面使用既有默认值预检。 */
export async function projectUploadLimit(): Promise<number> {
  try {
    const result = await api.publicConfigs(['files.max_upload_bytes'])
    const configured = Number(result.items.find(item => item.key === 'files.max_upload_bytes')?.value)
    return Number.isFinite(configured) && configured > 0 ? configured : fallbackProjectUploadLimit
  } catch { return fallbackProjectUploadLimit }
}
