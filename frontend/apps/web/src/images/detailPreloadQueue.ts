/** 列表详情后台队列：合并同资源任务，限制并发，取消过期条目并释放已准备的像素资源。 */
export function createDetailPreloadQueue<T>(identity: () => string, dispose: (value: T) => void, concurrency = 2) {
  type Entry = { promise: Promise<T>; value?: T; cancelled: boolean; run: () => void; reject: (error: Error) => void }
  const entries = new Map<string, Entry>()
  const queue: Entry[] = []
  let scope = identity(), active = 0
  function remove(key: string, entry: Entry) {
    entries.delete(key); entry.cancelled = true
    entry.reject(new Error('preload_cancelled'))
    if (entry.value !== undefined) dispose(entry.value)
  }
  function clear() { for (const [key, entry] of entries) remove(key, entry); queue.length = 0; scope = identity() }
  function ensureScope() { if (scope !== identity()) clear() }
  function pump() {
    while (active < concurrency && queue.length) {
      const entry = queue.shift()!
      if (!entry.cancelled) { active++; entry.run() }
    }
  }
  /** 仅在列表数据到达时调用；点击只读取 peek，不创建新下载。 */
  function preload(key: string, load: () => Promise<T>): Promise<T> {
    ensureScope()
    const previous = entries.get(key)
    if (previous) return previous.promise
    const expected = scope
    let resolve!: (value: T) => void, reject!: (error: Error) => void
    const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
    const entry: Entry = { promise, cancelled: false, reject, run: () => {
      void Promise.resolve().then(() => {
        if (entry.cancelled || expected !== identity()) throw new Error('preload_cancelled')
        return load()
      }).then(value => {
        if (entry.cancelled || expected !== identity()) { dispose(value); throw new Error('preload_cancelled') }
        entry.value = value; resolve(value)
      }).catch(error => {
        if (entries.get(key) === entry) entries.delete(key)
        reject(error)
      }).finally(() => { active--; pump() })
    } }
    // 后台失败不产生未处理拒绝；点击等待同一个 promise 时仍收到真实错误。
    void promise.catch(() => {})
    entries.set(key, entry); queue.push(entry); pump()
    return promise
  }
  function peek(key: string): Promise<T> | undefined { ensureScope(); return entries.get(key)?.promise }
  function retain(keys: Set<string>) { ensureScope(); for (const [key, entry] of entries) if (!keys.has(key)) remove(key, entry) }
  return { preload, peek, retain, clear }
}
