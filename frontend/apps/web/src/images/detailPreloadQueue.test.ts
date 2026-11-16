import { describe, expect, it, vi } from 'vitest'
import { createDetailPreloadQueue } from './detailPreloadQueue'

const turn = () => new Promise(resolve => setTimeout(resolve, 0))
describe('列表详情预加载队列', () => {
  it('列表创建即启动全部任务，限制并发；点击读取不新建请求', async () => {
    const finish: ((value: string) => void)[] = []
    const load = vi.fn(() => new Promise<string>(resolve => finish.push(resolve)))
    const queue = createDetailPreloadQueue<string>(() => 'personal', vi.fn(), 2)
    const jobs = ['one', 'two', 'three'].map(key => queue.preload(key, load))
    await turn(); expect(load).toHaveBeenCalledTimes(2)
    expect(queue.peek('one')).toBe(jobs[0]); expect(queue.preload('one', load)).toBe(jobs[0])
    finish[0]!('one'); await turn(); expect(load).toHaveBeenCalledTimes(3)
    finish[1]!('two'); finish[2]!('three')
    await expect(Promise.all(jobs)).resolves.toEqual(['one', 'two', 'three'])
    expect(queue.peek('missing')).toBeUndefined(); expect(load).toHaveBeenCalledTimes(3)
  })
  it('筛选移除条目时释放资源，排队条目不再下载', async () => {
    const dispose = vi.fn(), removed = vi.fn(async () => 'removed')
    const queue = createDetailPreloadQueue<string>(() => 'scope', dispose, 1)
    let finish!: (value: string) => void
    const active = queue.preload('active', () => new Promise(resolve => { finish = resolve }))
    queue.preload('removed', removed)
    await turn(); queue.retain(new Set(['active']))
    finish('pixels'); await active; await turn()
    expect(removed).not.toHaveBeenCalled()
    queue.clear(); expect(dispose).toHaveBeenCalledExactlyOnceWith('pixels')
    expect(queue.peek('active')).toBeUndefined()
  })
  it('在途任务跨空间返回时拒绝复用，释放迟到资源并取消旧空间队列', async () => {
    let scope = 'org-a', finish!: (value: string) => void
    const dispose = vi.fn(), later = vi.fn(async () => 'later')
    const queue = createDetailPreloadQueue<string>(() => scope, dispose, 1)
    const first = queue.preload('first', () => new Promise(resolve => { finish = resolve }))
    queue.preload('later', later)
    const rejected = expect(first).rejects.toThrow('preload_cancelled')
    await turn(); scope = 'org-b'; expect(queue.peek('first')).toBeUndefined()
    finish('private-pixels'); await rejected; await turn()
    expect(later).not.toHaveBeenCalled(); expect(dispose).toHaveBeenCalledWith('private-pixels')
  })
  it('失败不会留下永久挂起任务，列表重试可以重新预取', async () => {
    const queue = createDetailPreloadQueue<string>(() => 'scope', vi.fn())
    await expect(queue.preload('one', async () => { throw new Error('network') })).rejects.toThrow('network')
    expect(queue.peek('one')).toBeUndefined()
    await expect(queue.preload('one', async () => 'ready')).resolves.toBe('ready')
  })
})
