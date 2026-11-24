import { describe, expect, it } from 'vitest'
import {
  ANCHOR_LABEL_MAX_WIDTH,
  ANCHOR_LABEL_MIN_WIDTH,
  anchorLabelMaxWidth,
  anchorNoteTitle,
  splitAnchorNote,
  thumbnailAnchorTextScale,
} from './editorAnchorLayout'

describe('锚点文字标签宽度', () => {
  it('文字较短时保持紧凑，较长时扩展到上限', () => {
    expect(anchorLabelMaxWidth('树', 400, 1, 1)).toBe(96)
    expect(anchorLabelMaxWidth('一'.repeat(40), 400, 1, 1)).toBe(ANCHOR_LABEL_MAX_WIDTH)
  })

  it('右侧空间不足时收缩到可用宽度以触发自动换行', () => {
    expect(anchorLabelMaxWidth('一'.repeat(20), 120, 1, 1)).toBe(120)
    expect(anchorLabelMaxWidth('一'.repeat(20), 20, 1, 1)).toBe(ANCHOR_LABEL_MIN_WIDTH)
    expect(anchorLabelMaxWidth('一'.repeat(20), 0, 1, 1)).toBe(ANCHOR_LABEL_MIN_WIDTH)
  })

  it('单个锚点文字倍率和全局展示比例都会放大标签宽度', () => {
    expect(anchorLabelMaxWidth('发光的树影落在墙上', 400, 1, 2)).toBe(276)
    expect(anchorLabelMaxWidth('发光的树影落在墙上', 400, 1.5, 1)).toBe(225)
  })
})

describe('锚点标题与详细描述', () => {
  it('第一行作为标题，其余行保留为详情', () => {
    expect(splitAnchorNote('仰拍借势\n蹲低一点往上拍，让树干往上冲\n树显得更高')).toEqual({
      title: '仰拍借势',
      description: '蹲低一点往上拍，让树干往上冲\n树显得更高',
    })
    expect(anchorNoteTitle('蓝绿两色\n上面是蓝天')).toBe('蓝绿两色')
  })

  it('兼容单行旧备注、开头空行和首尾空白', () => {
    expect(splitAnchorNote('旧备注')).toEqual({ title: '旧备注', description: '' })
    expect(splitAnchorNote('\n\n全景\n留出人物到树顶的整段高度\n')).toEqual({
      title: '全景',
      description: '留出人物到树顶的整段高度',
    })
    expect(splitAnchorNote('')).toEqual({ title: '', description: '' })
  })
})

describe('列表锚点视觉比例', () => {
  it('文字、圆点和短横线随图片渲染宽度等比缩小', () => {
    expect(thumbnailAnchorTextScale(420)).toBe(1)
    expect(thumbnailAnchorTextScale(336)).toBeCloseTo(0.8)
    expect(thumbnailAnchorTextScale(630)).toBe(1.25)
  })

  it('过小、非法尺寸使用可读下限，避免手机双列列表文字不可辨认', () => {
    expect(thumbnailAnchorTextScale(120)).toBe(0.6)
    expect(thumbnailAnchorTextScale(0)).toBe(0.6)
    expect(thumbnailAnchorTextScale(Number.NaN)).toBe(0.6)
  })
})
