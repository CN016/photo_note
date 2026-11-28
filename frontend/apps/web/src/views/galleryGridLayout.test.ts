import { describe, expect, it } from 'vitest'
import {
  DEFAULT_GALLERY_COLUMNS,
  GALLERY_COLUMN_STEPS,
  GALLERY_GRID_GAP,
  GALLERY_MOBILE_GRID_GAP,
  GALLERY_THUMBNAIL_RADIUS,
  nearestGalleryColumnCount,
} from './galleryGridLayout'

describe('图库默认网格', () => {
  it('进入列表默认显示四列并使用小圆角', () => {
    expect(DEFAULT_GALLERY_COLUMNS).toBe(4)
    expect(GALLERY_THUMBNAIL_RADIUS).toBe(3)
    expect(GALLERY_GRID_GAP).toBe(6)
    expect(GALLERY_MOBILE_GRID_GAP).toBe(4)
  })

  it('缩放结果吸附到允许列数档位', () => {
    expect(nearestGalleryColumnCount(4.4)).toBe(4)
    expect(nearestGalleryColumnCount(7.6)).toBe(10)
    expect(nearestGalleryColumnCount(0)).toBe(4)
    expect(nearestGalleryColumnCount(Number.NaN)).toBe(4)
    expect(GALLERY_COLUMN_STEPS).toContain(4)
  })
})
