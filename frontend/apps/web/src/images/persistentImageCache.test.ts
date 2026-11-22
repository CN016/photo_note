import { afterEach, describe, expect, it, vi } from 'vitest'
import { clearPersistentImages, persistentImageCacheAvailable, readPersistentImage, writePersistentImage } from './persistentImageCache'

function cacheStorageFixture() {
  const entries = new Map<string, Response>()
  const store = {
    open: vi.fn(async () => ({
      match: vi.fn(async (request: Request) => entries.get(request.url)?.clone()),
      put: vi.fn(async (request: Request, response: Response) => { entries.set(request.url, response.clone()) }),
      delete: vi.fn(async (request: Request) => entries.delete(request.url)),
      keys: vi.fn(async () => [...entries.keys()].map(url => new Request(url))),
    })),
    delete: vi.fn(async () => entries.clear()),
  }
  return { store, entries }
}

afterEach(() => vi.unstubAllGlobals())

describe('本地高清图缓存', () => {
  it('内容版本未变化时直接复用本地 Blob', async () => {
    const fixture = cacheStorageFixture()
    vi.stubGlobal('caches', fixture.store)
    expect(persistentImageCacheAvailable()).toBe(true)
    await writePersistentImage('scope', 'image-key', 'hash-a', new Blob(['detail'], { type: 'image/jpeg' }))
    await expect(readPersistentImage('scope', 'image-key', 'hash-a').then(blob => blob?.text())).resolves.toBe('detail')
  })

  it('内容版本变化时删除旧图并返回未命中，由详情队列重新获取', async () => {
    const fixture = cacheStorageFixture()
    vi.stubGlobal('caches', fixture.store)
    await writePersistentImage('scope', 'image-key', 'hash-a', new Blob(['old']))
    await expect(readPersistentImage('scope', 'image-key', 'hash-b')).resolves.toBeNull()
    expect(fixture.entries.size).toBe(0)
  })

  it('退出时清除全部私有图片缓存', async () => {
    const fixture = cacheStorageFixture()
    vi.stubGlobal('caches', fixture.store)
    await writePersistentImage('scope', 'image-key', 'hash-a', new Blob(['private']))
    await clearPersistentImages()
    expect(fixture.entries.size).toBe(0)
  })
})
