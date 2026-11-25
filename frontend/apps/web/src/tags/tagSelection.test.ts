import { describe, expect, it } from 'vitest'
import { mergeTagIDs, orderTagIDs } from './tagSelection'

const tags = [
  { business_id: 'custom-new', tag_type: 'custom' as const },
  { business_id: 'system-a', tag_type: 'system' as const },
  { business_id: 'custom-old', tag_type: 'custom' as const },
  { business_id: 'system-b', tag_type: 'system' as const },
]

describe('灵感图标签选择', () => {
  it('系统标签排在自定义标签之前并保持组内顺序', () => {
    expect(orderTagIDs(['custom-new', 'system-a', 'custom-old', 'system-b'], tags)).toEqual([
      'system-a', 'system-b', 'custom-new', 'custom-old',
    ])
  })

  it('批量追加自动去重，超过 8 个时报告溢出', () => {
    const existing = ['custom-old', 'system-a']
    const additions = ['system-a', 'system-b', 'custom-new']
    expect(mergeTagIDs(existing, additions, tags)).toEqual({
      ids: ['system-a', 'system-b', 'custom-old', 'custom-new'],
      overflow: false,
    })
    expect(mergeTagIDs(['custom-old'], Array.from({ length: 8 }, (_, index) => `system-${index}`), [{ business_id: 'custom-old', tag_type: 'custom' }, ...Array.from({ length: 8 }, (_, index) => ({ business_id: `system-${index}`, tag_type: 'system' as const }))])).toMatchObject({ overflow: true })
  })
})
