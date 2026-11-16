import { api, getSessionInfo } from '../api/client'
import type { InspirationRecord } from '../api/types'
import { createThumbnailCache } from './thumbnailCache'
import { createDetailPreloadQueue } from './detailPreloadQueue'
import { parseEditorDocument } from '../../../../packages/editor-core/src'
import { decodeImagePreview, releaseCanvas, renderDocumentPreview } from '../../../../packages/image-processing/src'

/** 缓存身份包含账号会话、空间与业务代次，不使用令牌作为键。 */
function identity(): string { const info = getSessionInfo(); return info ? `${info.session_id}:${info.orgid}:${info.session_version}` : '' }
const limits = { concurrency: 4, entries: 256, bytes: 128 * 1024 * 1024 }
const images = createThumbnailCache(key => api.download(key), identity, limits)
const managedImages = createThumbnailCache(key => {
  const [id, storekey, orgid] = JSON.parse(key) as [string, string, number | null]
  return api.managementImage(id, storekey, orgid ?? undefined)
}, identity, limits)
/** 交付预览保留原有公私授权分流；分享令牌和访问码只在内存键中使用，不持久化、不输出日志。 */
const deliveryImages = createThumbnailCache(key => {
  const [task, photo, token, code, thumbnail] = JSON.parse(key) as [string, string, string, string, boolean]
  return token ? api.deliveryPublicFile(token, code, photo, false, thumbnail) : api.deliveryFile(task, photo, false, thumbnail)
}, identity, limits)
export const loadDeliveryImage = (task: string, photo: string, token = '', code = '', thumbnail = false) => deliveryImages.get(JSON.stringify([task, photo, token, code, thumbnail]))
/** 普通列表与详情共用工作图 Blob，缓存有界，资源 key 变化自然失效。 */
export const loadDetailImage = (key: string, version = '') => images.get(key, version)
/** 管理入口使用独立授权下载与独立缓存，平台选中组织和原记录 ID 均进入键，绝不复用普通私有接口。 */
export const loadManagedDetailImage = (id: string, key: string, orgid?: number) => managedImages.get(JSON.stringify([id, key, orgid ?? null]))

export interface PreparedInspirationDetail { record: InspirationRecord; image: HTMLImageElement; url: string }
/** 仅将当前列表需要的预览保留为压缩对象地址；中间原始画布在后台任务结束即释放。 */
export const inspirationDetails = createDetailPreloadQueue<PreparedInspirationDetail>(identity, value => {
  value.image.src = ''; URL.revokeObjectURL(value.url)
})
export const inspirationDetailKey = (record: InspirationRecord) => JSON.stringify([record.business_id, record.updated_at, record.image_key, record.file_hash, record.data])

/** 列表一返回即准备所有条目，包括屏幕外条目；下载合并、解码并发限制，避免同时保留大量原图画布。 */
export function preloadInspirationDetail(item: InspirationRecord): Promise<PreparedInspirationDetail> {
  return inspirationDetails.preload(inspirationDetailKey(item), async () => {
    const expected = identity()
    const record = await api.inspiration(item.business_id)
    if (expected !== identity()) throw new Error('space_changed')
    const blob = await loadDetailImage(record.image_key, record.file_hash || '')
    let source: HTMLCanvasElement | null = null, output: HTMLCanvasElement | null = null, url = ''
    try {
      // 动画使用详情编辑结果而非裁切缩略图；长边限制1600，完整详情仍从缓存工作图解码。
      source = (await decodeImagePreview(blob, 1600)).canvas
      output = renderDocumentPreview(source, parseEditorDocument(record.data))
      const preview = await new Promise<Blob>((resolve, reject) => output!.toBlob(value => value ? resolve(value) : reject(new Error('preview_encode_failed')), 'image/png'))
      url = URL.createObjectURL(preview)
      const image = new Image(); image.src = url
      await image.decode()
      return { record, image, url }
    } catch (error) { if (url) URL.revokeObjectURL(url); throw error }
    finally { releaseCanvas(source); releaseCanvas(output) }
  })
}

let handoff: { scope: string; expires: number; record: InspirationRecord } | null = null
/** 仅向紧接着打开的同空间详情交接已预取记录，避免再请求一次元数据；不跨路由长期使用旧记录。 */
export function prepareDetailRecord(record: InspirationRecord): void { handoff = { scope: identity(), expires: Date.now() + 5000, record } }
export function takeDetailRecord(id: string): InspirationRecord | null {
  const value = handoff; handoff = null
  return value?.scope === identity() && value.expires > Date.now() && value.record.business_id === id ? value.record : null
}
let previous = identity()
function clearChangedScope() {
  if (identity() === previous) return
  previous = identity(); images.clear(); managedImages.clear(); deliveryImages.clear(); inspirationDetails.clear(); handoff = null
}
window.addEventListener('photo-nini-session-updated', clearChangedScope)
window.addEventListener('storage', event => { if (event.key === 'photo-nini.session') clearChangedScope() })
