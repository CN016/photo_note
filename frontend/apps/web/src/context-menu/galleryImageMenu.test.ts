import { describe, expect, it, vi } from 'vitest'
import { createGalleryImageMenu } from './galleryImageMenu'

describe('灵感库图片菜单', () => {
  it('复制图片是第一项，且不再包含关联策划', async () => {
    const copy = vi.fn()
    const entries = createGalleryImageMenu('复制图片', true, copy)

    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({ label: '复制图片', disabled: false })
    await entries[0]?.run?.()
    expect(copy).toHaveBeenCalledTimes(1)
  })

  it('剪贴板不可写时保留唯一的禁用复制入口', () => {
    expect(createGalleryImageMenu('Copy image', false, () => {})).toEqual([
      { label: 'Copy image', disabled: true, run: expect.any(Function) },
    ])
  })
})
