/** 浏览器本地高清图缓存名称；版本前缀变化时旧缓存自然失效。 */
const CACHE_NAME = 'lightnote-detail-images-v1'
const CACHE_ORIGIN = 'https://lightnote-cache.invalid'
const MAX_ENTRIES = 96

/** 仅使用会话范围、资源 key 和内容版本生成缓存地址，不写入访问令牌。 */
function cacheRequest(scope: string, key: string): Request {
  return new Request(`${CACHE_ORIGIN}/detail/${encodeURIComponent(scope)}/${encodeURIComponent(key)}`)
}

/** 当前运行环境是否支持持久化 Cache Storage；隐私模式和旧 WebView 安全回退到内存缓存。 */
export function persistentImageCacheAvailable(): boolean {
  return typeof caches !== 'undefined'
}

/**
 * 按图片内容版本读取本地 Blob。版本变化时立即删除旧条目并返回未命中，
 * 页面先继续显示列表预览图，再由现有详情加载队列重新获取高清图。
 */
export async function readPersistentImage(scope: string, key: string, version: string): Promise<Blob | null> {
  if (!version || !persistentImageCacheAvailable()) return null
  try {
    const cache = await caches.open(CACHE_NAME)
    const request = cacheRequest(scope, key)
    const response = await cache.match(request)
    if (!response) return null
    if (response.headers.get('x-lightnote-version') !== version) {
      await cache.delete(request)
      return null
    }
    return await response.blob()
  } catch {
    return null
  }
}

/** 删除最旧条目，避免长期浏览无限占用设备存储。 */
async function trim(cache: Cache): Promise<void> {
  const requests = await cache.keys()
  if (requests.length <= MAX_ENTRIES) return
  const entries = await Promise.all(requests.map(async request => {
    const response = await cache.match(request)
    return { request, cachedAt: Number(response?.headers.get('x-lightnote-cached-at') || 0) }
  }))
  entries.sort((left, right) => left.cachedAt - right.cachedAt)
  for (const entry of entries.slice(0, Math.max(0, entries.length - MAX_ENTRIES))) await cache.delete(entry.request)
}

/** 成功下载后覆盖同一资源的高清图；Cache Storage 不可用时静默跳过，不阻断编辑流程。 */
export async function writePersistentImage(scope: string, key: string, version: string, blob: Blob): Promise<void> {
  if (!version || !persistentImageCacheAvailable()) return
  try {
    const cache = await caches.open(CACHE_NAME)
    await cache.put(cacheRequest(scope, key), new Response(blob, {
      headers: {
        'content-type': blob.type || 'application/octet-stream',
        'content-length': String(blob.size),
        'x-lightnote-version': version,
        'x-lightnote-cached-at': String(Date.now()),
      },
    }))
    await trim(cache)
  } catch {
    // 配额不足或禁用存储时保留当前在线结果；下一次显示仍可重新下载。
  }
}

/** 退出或切换空间时清除全部私有图片缓存，避免下一位设备使用者读取旧内容。 */
export async function clearPersistentImages(): Promise<void> {
  if (!persistentImageCacheAvailable()) return
  try { await caches.delete(CACHE_NAME) } catch { /* 缓存清理失败不应阻断退出流程。 */ }
}
