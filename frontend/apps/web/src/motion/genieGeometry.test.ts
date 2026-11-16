import { describe, expect, it } from 'vitest'
import { geniePoint } from './genieGeometry'

const source = { x: 0, y: 0, width: 390, height: 844 }
const targets = [
  { x: 18, y: 110, width: 165, height: 180 },
  { x: 207, y: 610, width: 165, height: 190 },
  { x: 0, y: -50, width: 180, height: 180 },
]

describe('Genie 贝塞尔滑道', () => {
  it('完整展开和收回精确匹配真实边界，不改变最终图片屏占比', () => {
    for (const target of targets) for (const u of [0, 0.5, 1]) for (const v of [0, 0.5, 1]) {
      expect(geniePoint(source, target, 1, u, v)).toEqual([u * 390, v * 844])
      expect(geniePoint(source, target, 0, u, v)).toEqual([target.x + u * target.width, target.y + v * target.height])
    }
  })
  it('列表框与详情重叠时无翻折、倒置或非有限坐标', () => {
    for (const target of targets) for (let frame = 0; frame <= 100; frame++) {
      let previous = -Infinity
      for (let row = 0; row <= 96; row++) {
        const left = geniePoint(source, target, frame / 100, 0, row / 96)
        const right = geniePoint(source, target, frame / 100, 1, row / 96)
        expect(left.every(Number.isFinite) && right.every(Number.isFinite)).toBe(true)
        expect(right[0]).toBeGreaterThan(left[0])
        expect(left[1]).toBeGreaterThanOrEqual(previous - 0.001)
        previous = left[1]
      }
    }
  })
  it('中段边缘有弯曲且各横截面宽度不同，不能退化为统一缩放或梯形', () => {
    const target = targets[1]!
    const width = (v: number) => geniePoint(source, target, 0.65, 1, v)[0] - geniePoint(source, target, 0.65, 0, v)[0]
    expect(width(0) - width(1)).toBeGreaterThan(150)
    expect(Math.abs(width(0.25) - (width(0) * 0.75 + width(1) * 0.25))).toBeGreaterThan(5)
  })
  it('收回口附近切片连续，正倒放同进度同位置', () => {
    for (const target of targets) for (let frame = 1; frame < 100; frame++) {
      const p = frame / 100
      for (const v of [0, 0.25, 0.75, 0.999999, 1]) {
        const a = geniePoint(source, target, p, 0.5, v)
        const b = geniePoint(source, target, 1 - (1 - p), 0.5, v)
        expect(a[0]).toBeCloseTo(b[0], 5); expect(a[1]).toBeCloseTo(b[1], 5)
      }
    }
    const edge = geniePoint(source, targets[1]!, 0.9, 0.5, 1)
    const nearby = geniePoint(source, targets[1]!, 0.9, 0.5, 0.999999)
    expect(Math.abs(edge[1] - nearby[1])).toBeLessThan(0.01)
  })
})
