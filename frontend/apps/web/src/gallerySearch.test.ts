import { describe, expect, it } from 'vitest'
import {
  applyGalleryPeoplePreset,
  createGallerySearchState,
  galleryPeoplePreset,
  gallerySearchHasValue,
  inspirationRequestOptions,
  switchGallerySearchKind,
} from './gallerySearch'

describe('gallery search criteria', () => {
  it('切换条件时清除上一种搜索输入', () => {
    const text = { ...createGallerySearchState('text'), query: '海边' }
    const tags = switchGallerySearchKind(text, 'tags')
    expect(tags.kind).toBe('tags')
    expect(tags.query).toBe('')
    expect(tags.tagIds).toEqual([])
  })

  it('文本条件只提交 q 并保留独立浏览条件', () => {
    const state = { ...createGallerySearchState('text'), query: '  日落  ' }
    expect(inspirationRequestOptions(state, 'updated_desc', { project: 'unassigned' })).toMatchObject({
      q: '日落',
      sort: 'updated_desc',
      project: 'unassigned',
    })
  })

  it('色系选择固定使用 color_match 且拒绝无效色值', () => {
    const state = { ...createGallerySearchState('color'), color: '#3a7bd5' }
    expect(inspirationRequestOptions(state, 'created_desc')).toMatchObject({ color: '#3a7bd5', sort: 'color_match' })
    expect(gallerySearchHasValue({ ...state, color: 'blue' })).toBe(false)
  })

  it('标签来源与匹配方式只随标签条件提交', () => {
    const state = {
      ...createGallerySearchState('tags'),
      tagIds: ['tag-a', 'tag-b'],
      tagMatch: 'all' as const,
      tagSource: 'ai' as const,
    }
    expect(inspirationRequestOptions(state, 'created_desc')).toMatchObject({
      tagIds: ['tag-a', 'tag-b'],
      tagMatch: 'all',
      tag_source: 'ai',
    })
  })

  it('人数范围会规范上下界并只提交范围参数', () => {
    const state = { ...createGallerySearchState('people'), peopleMode: 'range' as const, peopleMin: 8, peopleMax: 3 }
    expect(inspirationRequestOptions(state, 'created_desc')).toMatchObject({ people_mode: 'range', people_min: 3, people_max: 8 })
  })

  it('快捷人数覆盖无人、1 至 6 人和多人', () => {
    const initial = createGallerySearchState('people')
    expect(applyGalleryPeoplePreset(initial, 'absent')).toMatchObject({ peopleMode: 'absent' })
    expect(applyGalleryPeoplePreset(initial, '4')).toMatchObject({ peopleMode: 'exact', peopleMin: 4, peopleMax: 4 })
    expect(applyGalleryPeoplePreset(initial, 'many')).toMatchObject({ peopleMode: 'range', peopleMin: 6, peopleMax: 100000 })
    expect(galleryPeoplePreset(applyGalleryPeoplePreset(initial, '6'))).toBe('6')
    expect(galleryPeoplePreset(applyGalleryPeoplePreset(initial, 'many'))).toBe('many')
    expect(galleryPeoplePreset({ ...initial, peopleMode: 'present' })).toBe('')
  })
})
