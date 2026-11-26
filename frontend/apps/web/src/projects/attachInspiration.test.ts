import { describe, expect, it } from 'vitest'
import { appendInspirationToProject } from './attachInspiration'

describe('把灵感图加入策划正文', () => {
  it('保留末尾段落并在其前插入灵感图节点', () => {
    const source = {
      type: 'doc',
      content: [
        { type: 'paragraph', content: [{ type: 'text', text: '开头' }] },
        { type: 'paragraph', content: [] },
      ],
    }
    const result = appendInspirationToProject(source, 'image-1')
    expect(result.added).toBe(true)
    expect(result.document.content).toEqual([
      source.content[0],
      { type: 'inspirationImage', attrs: { src: 'inspiration://image-1', width: 100, layout: 'original' } },
      source.content[1],
    ])
    expect(source.content).toHaveLength(2)
  })

  it('正文已引用图片时不重复添加', () => {
    const source = {
      type: 'doc',
      content: [{ type: 'inspirationImage', attrs: { src: 'inspiration://image-1', width: 60 } }],
    }
    expect(appendInspirationToProject(source, 'image-1').added).toBe(false)
  })

  it('非段落结尾时在图片后补空段落，保证还能继续输入', () => {
    const result = appendInspirationToProject({ type: 'doc', content: [] }, 'image-2')
    expect(result.document.content).toEqual([
      { type: 'inspirationImage', attrs: { src: 'inspiration://image-2', width: 100, layout: 'original' } },
      { type: 'paragraph' },
    ])
  })
})
