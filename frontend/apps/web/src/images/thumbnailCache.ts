/** 有界私有缩略图缓存：同资源合并请求，限制下载并发，切换会话后丢弃旧结果。 */
export function createThumbnailCache(load: (key: string) => Promise<Blob>, identity: () => string, options: { concurrency?: number; entries?: number; bytes?: number; ttl?: number; now?: () => number } = {}) {
  const limit = options.concurrency ?? 4
  const clock = options.now ?? Date.now
  type Entry = { promise: Promise<Blob>; expires: number; size: number; settled: boolean }
  const cache = new Map<string, Entry>()
  let scope = identity(), epoch = 0, active = 0
  const queue: { run: () => void; reject: (error: Error) => void }[] = []
  function clear() {
    epoch += 1; scope = identity(); cache.clear()
    for (const item of queue.splice(0)) item.reject(new Error('space_changed'))
  }
  function pump() { while (active < limit && queue.length) { active += 1; queue.shift()!.run() } }
  function trim() {
    let bytes = [...cache.values()].reduce((sum, item) => sum + item.size, 0)
    for (const [key, entry] of cache) {
      if (cache.size <= (options.entries ?? 48) && bytes <= (options.bytes ?? 16 * 1024 * 1024)) break
      if (entry.settled) { cache.delete(key); bytes -= entry.size }
    }
  }
  function get(key: string, version = ''): Promise<Blob> {
    if (scope !== identity()) clear()
    const cacheKey = `${key}:${version}`
    const previous = cache.get(cacheKey)
    if (previous && (!previous.settled || previous.expires > clock())) { cache.delete(cacheKey); cache.set(cacheKey, previous); return previous.promise }
    cache.delete(cacheKey)
    const generation = epoch
    const expected = scope
    const entry: Entry = { promise: Promise.resolve(new Blob()), expires: 0, size: 0, settled: false }
    entry.promise = new Promise<Blob>((resolve, reject) => {
      queue.push({ reject, run: () => {
        // 排队期间切换空间时不发送请求；在途请求仍由API的会话屏障检查正文。
        if (generation !== epoch || expected !== identity()) { active -= 1; reject(new Error('space_changed')); pump(); return }
        void load(key).then(blob => {
          if (generation !== epoch || expected !== identity()) throw new Error('space_changed')
          entry.size = blob.size; entry.expires = clock() + (options.ttl ?? 300_000)
          resolve(blob)
        }).catch(error => { entry.expires = clock() + 10_000; reject(error) })
          .finally(() => { entry.settled = true; active -= 1; trim(); pump() })
      } })
    })
    cache.set(cacheKey, entry); pump(); return entry.promise
  }
  return { get, clear }
}
