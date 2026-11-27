import { describe, expect, it, vi } from 'vitest'
import { createEditorDocument } from '../../../../packages/editor-core/src'
import { anchorExportMetrics, drawDocumentAnchors, wrapAnchorText } from './imageAnchorExport'

/** 构造只记录绘制调用的 Canvas 上下文，避免单元测试依赖真实浏览器像素。 */
function fakeContext() {
  const fillText = vi.fn()
  const fillRect = vi.fn()
  const measureText = vi.fn((text: string) => ({ width: Array.from(text).length * 10 }))
  const context = {
    save: vi.fn(),
    restore: vi.fn(),
    fillText,
    fillRect,
    measureText,
    beginPath: vi.fn(),
    arc: vi.fn(),
    fill: vi.fn(),
    stroke: vi.fn(),
    font: '',
    textAlign: 'left',
    textBaseline: 'alphabetic',
    shadowColor: '',
    shadowBlur: 0,
    shadowOffsetY: 0,
    fillStyle: '',
    strokeStyle: '',
    lineWidth: 1,
  } as unknown as CanvasRenderingContext2D
  return { context, fillText, fillRect, measureText }
}

describe('图片锚点导出', () => {
  it('按常见手机预览比例换算字号和落点尺寸', () => {
    const metrics = anchorExportMetrics(1800, 1.25, 1.5)
    expect(metrics.titleFontSize).toBeCloseTo(96.428571)
    expect(metrics.descriptionFontSize).toBeCloseTo(80.357143)
    expect(metrics.markerDotSize).toBeCloseTo(54)
    expect(metrics.markerOffset).toBeCloseTo(42.857143)
  })

  it('连续中文和网址都按画布宽度逐字换行', () => {
    const { context } = fakeContext()
    expect(wrapAnchorText(context, '蓝绿两色', 20)).toEqual(['蓝绿', '两色'])
    expect(wrapAnchorText(context, '第一行\nhttps://example.com', 30)).toEqual([
      '第一行',
      'htt',
      'ps:',
      '//e',
      'xam',
      'ple',
      '.co',
      'm',
    ])
  })

  it('把标题、黄色下划线、落点和描述绘制到输出坐标', () => {
    const document = createEditorDocument()
    document.anchors = [{ id: 'a1', x: 0.25, y: 0.5, note: '主体\n注意边缘' }]
    const { context, fillText, fillRect } = fakeContext()
    drawDocumentAnchors(context, document, 840, 600)

    expect(fillText).toHaveBeenCalledWith('主体', 210, 300)
    expect(fillText).toHaveBeenCalledWith('注意边缘', 210, expect.any(Number))
    expect(fillRect).toHaveBeenCalledWith(210, expect.any(Number), 20, expect.any(Number))
    expect(context.arc).toHaveBeenCalledWith(210, 300, expect.any(Number), 0, Math.PI * 2)
    expect(context.save).toHaveBeenCalledTimes(1)
    expect(context.restore).toHaveBeenCalledTimes(1)
  })
})
