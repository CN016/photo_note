import { describe, expect, it, vi } from 'vitest'
import { createGalleryImageMenu } from './galleryImageMenu'

describe('灵感库图片菜单', () => {
  it('复制第一项、添加第二项，已关联策划从第三项开始', async () => {
    const copy = vi.fn()
    const add = vi.fn()
    const open = vi.fn()
    const entries = createGalleryImageMenu({
      copyLabel: '复制图片',
      canCopy: true,
      copy,
      addLabel: '添加到…',
      addProjects: [{ business_id: 'p2', title: '海边策划' }],
      onAdd: add,
      relatedProjects: [{ business_id: 'p1', title: '春日策划' }],
      onOpenRelated: open,
    })

    expect(entries.map(entry => entry.label)).toEqual(['复制图片', '添加到…', '春日策划'])
    expect(entries[0]).toMatchObject({ label: '复制图片', disabled: false })
    await entries[0]?.run?.()
    expect(copy).toHaveBeenCalledTimes(1)
    await entries[1]?.children?.[0]?.run?.()
    expect(add).toHaveBeenCalledTimes(1)
    await entries[2]?.run?.()
    expect(open).toHaveBeenCalledTimes(1)
  })

  it('剪贴板不可写和加载状态下不改变前两行位置', () => {
    const entries = createGalleryImageMenu({
      copyLabel: 'Copy image',
      canCopy: false,
      copy: () => {},
      addLabel: 'Add to…',
      addProjects: [],
      addStatus: 'Loading…',
      addDisabled: true,
      onAdd: () => {},
      relatedProjects: [],
      relatedStatus: 'Loading…',
      onOpenRelated: () => {},
    })
    expect(entries.map(entry => entry.label)).toEqual(['Copy image', 'Add to…', 'Loading…'])
    expect(entries[0]?.disabled).toBe(true)
    expect(entries[1]?.disabled).toBe(true)
    expect(entries[1]?.children?.[0]?.disabled).toBe(true)
  })
})
