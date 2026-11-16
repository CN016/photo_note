import { beforeEach, describe, expect, it, vi } from 'vitest'
const fixture = vi.hoisted(() => ({
  scope: { session_id: 'synthetic-user', orgid: -1, session_version: 1 },
  record: { business_id: 'image-a', image_key: 'work-a', file_hash: 'hash-a', updated_at: '1', data: {} },
  api: { download: vi.fn(), inspiration: vi.fn(), managementImage: vi.fn(), deliveryFile: vi.fn(), deliveryPublicFile: vi.fn() },
  release: vi.fn(), decode: vi.fn(), render: vi.fn(),
}))
vi.mock('../api/client', () => ({ api: fixture.api, getSessionInfo: () => fixture.scope }))
vi.mock('../../../../packages/editor-core/src', () => ({ parseEditorDocument: (data: unknown) => data }))
vi.mock('../../../../packages/image-processing/src', () => ({ decodeImagePreview: fixture.decode, renderDocumentPreview: fixture.render, releaseCanvas: fixture.release }))
let details: typeof import('./detailImages')
beforeEach(async () => {
  vi.resetModules(); vi.clearAllMocks()
  fixture.scope = { session_id: 'synthetic-user', orgid: -1, session_version: 1 }
  for (const fn of [fixture.api.download, fixture.api.managementImage, fixture.api.deliveryFile, fixture.api.deliveryPublicFile]) fn.mockResolvedValue(new Blob(['synthetic']))
  fixture.api.inspiration.mockResolvedValue(fixture.record)
  fixture.decode.mockResolvedValue({ canvas: { width: 100, height: 80 } })
  fixture.render.mockReturnValue({ toBlob: (done: (blob: Blob) => void) => done(new Blob(['preview'])) })
  vi.stubGlobal('window', new EventTarget())
  vi.stubGlobal('Image', class { src = ''; decode() { return Promise.resolve() } })
  vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:synthetic')
  vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
  details = await import('./detailImages')
})

describe('详情图预加载与交接', () => {
  it('列表后台完成记录、完整工作图和编辑预览；点击只复用，详情不再次下载', async () => {
    const task = details.preloadInspirationDetail(fixture.record as any)
    const key = details.inspirationDetailKey(fixture.record as any)
    expect(details.inspirationDetails.peek(key)).toBe(task)
    const value = await task
    expect(fixture.api.inspiration).toHaveBeenCalledTimes(1)
    expect(fixture.api.download).toHaveBeenCalledExactlyOnceWith('work-a')
    expect(fixture.render).toHaveBeenCalledOnce(); expect(fixture.release).toHaveBeenCalledTimes(2)
    details.prepareDetailRecord(value.record)
    expect(details.takeDetailRecord('image-a')).toEqual(value.record)
    expect(details.takeDetailRecord('image-a')).toBeNull()
    await details.loadDetailImage('work-a', 'hash-a')
    expect(fixture.api.download).toHaveBeenCalledTimes(1)
    details.inspirationDetails.clear(); expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:synthetic')
  })
  it('普通、组织管理、平台管理与不同组织的相同资源不合并授权请求', async () => {
    await details.loadDetailImage('same')
    await details.loadManagedDetailImage('a', 'same')
    await details.loadManagedDetailImage('a', 'same', 10)
    await details.loadManagedDetailImage('a', 'same', 20)
    expect(fixture.api.download).toHaveBeenCalledTimes(1)
    expect(fixture.api.managementImage.mock.calls).toEqual([['a', 'same', undefined], ['a', 'same', 10], ['a', 'same', 20]])
  })
  it('交付完整预览可从列表复用，但不混用缩略图、私有资源和其他公开访问码', async () => {
    await details.loadDeliveryImage('task', 'photo')
    await details.loadDeliveryImage('task', 'photo')
    await details.loadDeliveryImage('task', 'photo', '', '', true)
    await details.loadDeliveryImage('task', 'photo', 'share', 'code-a')
    await details.loadDeliveryImage('task', 'photo', 'share', 'code-b')
    expect(fixture.api.deliveryFile).toHaveBeenCalledTimes(2)
    expect(fixture.api.deliveryPublicFile.mock.calls).toEqual([['share', 'code-a', 'photo', false, false], ['share', 'code-b', 'photo', false, false]])
  })
  it('记录响应跨空间迟到时不启动旧资源下载，也不交接旧记录', async () => {
    let finish!: (value: unknown) => void
    fixture.api.inspiration.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const promise = details.preloadInspirationDetail(fixture.record as any)
    await new Promise(resolve => setTimeout(resolve, 0))
    details.prepareDetailRecord(fixture.record as any)
    fixture.scope = { ...fixture.scope, orgid: 20, session_version: 2 }
    expect(details.takeDetailRecord('image-a')).toBeNull()
    finish(fixture.record)
    await expect(promise).rejects.toThrow('space_changed')
    expect(fixture.api.download).not.toHaveBeenCalled()
  })
})
