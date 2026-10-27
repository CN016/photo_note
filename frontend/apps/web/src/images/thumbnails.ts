import { api, getSessionInfo } from '../api/client'
import { createThumbnailCache } from './thumbnailCache'
import { ApiError } from '../api/types'
/** 会话ID同时隔离账号，组织与代次隔离空间；不把令牌纳入缓存键。 */
function identity() { const scope = getSessionInfo(); return scope ? `${scope.session_id}:${scope.orgid}:${scope.session_version}` : '' }
/** 历史HEIC等Go解码器尚不支持的格式保留原兼容路径，转换同样由共享缓存合并。 */
async function downloadThumbnail(key: string): Promise<Blob> {
  try { return await api.thumbnail(key) }
  catch (error) {
    if (!(error instanceof ApiError) || error.code !== 'thumbnail_format_unsupported') throw error
    const original = await api.download(key)
    const { previewObjectUrl } = await import('../../../../packages/image-processing/src')
    const url = await previewObjectUrl(original, 384)
    try { return await (await fetch(url)).blob() } finally { URL.revokeObjectURL(url) }
  }
}
const cache = createThumbnailCache(downloadThumbnail, identity)
let previousIdentity = identity()
window.addEventListener('photo-nini-session-updated', () => { const current = identity(); if(current !== previousIdentity) { previousIdentity = current; cache.clear() } })
window.addEventListener('storage', event => { if (event.key === 'photo-nini.session' && event.newValue === null) cache.clear() })
/** 常规格式直接复用服务器JPEG；特殊格式的兼容转换在缓存周期内也只执行一次。 */
export const loadThumbnail = (key: string, version?: string) => cache.get(key, version)
