import { describe, expect, it } from 'vitest'
import { clearGallerySnapshot, gallerySnapshot, saveGallerySnapshot, type GallerySnapshot } from './galleryNavigation'

describe('图库来源隔离', () => {
  const snapshot = { scope: 'account:org:version', key: 'origin', items: [], page: 3, scroll: 1600 } as unknown as GallerySnapshot
  it('只恢复同一会话和来源，空间切换后旧快照不可再次取得', () => {
    saveGallerySnapshot(snapshot)
    expect(gallerySnapshot(snapshot.scope, 'other')).toBeNull()
    expect(gallerySnapshot(snapshot.scope, 'origin')).toBe(snapshot)
    expect(gallerySnapshot('account:another-org:version')).toBeNull()
    expect(gallerySnapshot(snapshot.scope)).toBeNull()
  })
  it('登出显式清理', () => {
    saveGallerySnapshot(snapshot); clearGallerySnapshot()
    expect(gallerySnapshot(snapshot.scope)).toBeNull()
  })
})
