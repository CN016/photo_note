import { describe, expect, it } from 'vitest'
import { MAX_PINCH_ZOOM_SCALE, pinchTargetScale, pinchZoomFrame, pointDistance } from './pinchZoom'

const source = { left: 20, top: 100, width: 360, height: 240 }
const viewport = { width: 390, height: 844 }

describe('灵感图双指全屏几何', () => {
  it('手势开始时保持原位，随后按双指距离直接放大', () => {
    const start = pinchZoomFrame(source, viewport, { distance: 100, x: 200, y: 220 }, { distance: 100, x: 200, y: 220 })
    const frame = pinchZoomFrame(source, viewport, { distance: 100, x: 200, y: 220 }, { distance: 160, x: 200, y: 220 })
    expect(start.x).toBeCloseTo(0)
    expect(start.y).toBeCloseTo(0)
    expect(frame.scale).toBeCloseTo(1.6)
    expect(frame.progress).toBeGreaterThan(0)
  })

  it('达到目标比例后把图片中心送到视口中心', () => {
    const target = pinchTargetScale(source, viewport)
    const frame = pinchZoomFrame(source, viewport, { distance: 100, x: 140, y: 180 }, { distance: 100 * target, x: 260, y: 420 })
    expect(frame.scale).toBeCloseTo(target)
    expect(frame.progress).toBeCloseTo(1)
    expect(frame.x + source.left + source.width / 2).toBeCloseTo(viewport.width / 2)
    expect(frame.y + source.top + source.height / 2).toBeCloseTo(viewport.height / 2)
  })

  it('缩放和异常输入均收敛到安全范围', () => {
    const tooSmall = pinchZoomFrame(source, viewport, { distance: 100, x: 200, y: 220 }, { distance: 20, x: 200, y: 220 })
    const tooLarge = pinchZoomFrame(source, viewport, { distance: 0.1, x: 200, y: 220 }, { distance: 1000, x: 200, y: 220 })
    expect(tooSmall.scale).toBe(1)
    expect(tooLarge.scale).toBe(MAX_PINCH_ZOOM_SCALE)
  })

  it('双指距离计算返回欧氏距离', () => {
    expect(pointDistance({ x: 0, y: 0 }, { x: 3, y: 4 })).toBe(5)
  })
})
