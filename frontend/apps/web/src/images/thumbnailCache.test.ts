import { describe, expect, it, vi } from 'vitest'
import { createThumbnailCache } from './thumbnailCache'
const turn = () => new Promise(resolve => setTimeout(resolve, 0))
describe('私有缩略图缓存', () => {
  it('日历和列表合并下载，重新进入可复用，图片代次变化重新读取', async () => {
    const load = vi.fn(async () => new Blob(['image']))
    const cache = createThumbnailCache(load, () => 'user:org:1')
    const [a,b] = await Promise.all([cache.get('key','hash1'),cache.get('key','hash1')])
    expect(a).toBe(b); expect(load).toHaveBeenCalledTimes(1)
    await cache.get('key','hash1');expect(load).toHaveBeenCalledTimes(1)
    await cache.get('key','hash2');expect(load).toHaveBeenCalledTimes(2)
  })
  it('最多四个下载，避免日历请求淹没列表', async () => {
    const resolvers: ((blob: Blob) => void)[] = []
    const load = vi.fn(() => new Promise<Blob>(resolve => resolvers.push(resolve)))
    const cache = createThumbnailCache(load, () => 'scope')
    const tasks = Array.from({length:8},(_,i) => cache.get(String(i)))
    expect(load).toHaveBeenCalledTimes(4)
    resolvers.slice(0,4).forEach(resolve => resolve(new Blob(['x'])));await turn()
    expect(load).toHaveBeenCalledTimes(8)
    resolvers.slice(4).forEach(resolve => resolve(new Blob(['x'])));await Promise.all(tasks)
  })
  it('空间切换丢弃迟到正文，旧缓存不会被新空间复用', async () => {
    let scope='org1';let finish: (blob: Blob) => void = () => {}
    const cache=createThumbnailCache(() => new Promise(resolve => {finish=resolve}),() => scope)
    const pending=cache.get('key');const rejected=expect(pending).rejects.toThrow('space_changed')
    scope='org2';finish(new Blob(['private']));await rejected
  })
  it('失败短期合并，过期后允许重试', async () => {
    let now=0;const load=vi.fn(async () => {throw new Error('404')})
    const cache=createThumbnailCache(load,()=>'scope',{now:()=>now})
    await expect(cache.get('missing')).rejects.toThrow('404');await turn()
    await expect(cache.get('missing')).rejects.toThrow('404');expect(load).toHaveBeenCalledTimes(1)
    now=10001;await expect(cache.get('missing')).rejects.toThrow('404');expect(load).toHaveBeenCalledTimes(2)
  })
  it('限制缓存条目并保留最近使用项', async () => {
    const load=vi.fn(async () => new Blob(['x']))
    const cache=createThumbnailCache(load,()=> 'scope',{entries:1})
    await cache.get('a');await turn();await cache.get('b');await turn()
    await cache.get('b');expect(load).toHaveBeenCalledTimes(2)
    await cache.get('a');expect(load).toHaveBeenCalledTimes(3)
  })
  it('清理缓存时取消尚未发出的旧空间排队请求', async () => {
    let finish: (blob: Blob) => void = () => {}
    const load=vi.fn(() => new Promise<Blob>(resolve => {finish=resolve}))
    const cache=createThumbnailCache(load,()=> 'scope',{concurrency:1})
    const first=cache.get('a'), queued=cache.get('b')
    const firstRejected=expect(first).rejects.toThrow('space_changed')
    const queuedRejected=expect(queued).rejects.toThrow('space_changed')
    cache.clear();finish(new Blob(['x']))
    await Promise.all([firstRejected,queuedRejected]);expect(load).toHaveBeenCalledTimes(1)
  })

})
