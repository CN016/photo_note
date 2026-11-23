import { describe, expect, it, vi } from 'vitest'
import {
  clampWatermarkPoint, clampWatermarkScale, drawWatermark, watermarkFontSize, watermarkPreviewFontSize,
} from './watermark'

describe('导出水印几何', () => {
  it('限制倍率和水印中心，非法输入使用安全默认值', () => {
    expect(clampWatermarkScale(0.1)).toBe(0.5)
    expect(clampWatermarkScale(9)).toBe(3)
    expect(clampWatermarkScale(1.37)).toBe(1.35)
    expect(clampWatermarkScale(Number.NaN)).toBe(1)
    expect(clampWatermarkPoint(-1)).toBe(0.02)
    expect(clampWatermarkPoint(2)).toBe(0.98)
    expect(clampWatermarkPoint(Number.NaN)).toBe(0.5)
  })

  it('字号随图片高度和倍率变化，并保留最小可读值', () => {
    expect(watermarkFontSize(1000, 1)).toBe(45)
    expect(watermarkFontSize(1000, 2)).toBe(90)
    expect(watermarkFontSize(100, 0.5)).toBe(10)
    expect(watermarkPreviewFontSize(300, 1)).toBe(14)
    expect(watermarkPreviewFontSize(100, 0.5)).toBe(12)
  })

  it('绘制使用归一化位置和白色半透明文字', () => {
    const fillText = vi.fn()
    const context = {
      save: vi.fn(), restore: vi.fn(), fillText,
      font: '', textAlign: '', textBaseline: '', shadowColor: '', shadowBlur: 0, shadowOffsetY: 0, fillStyle: '',
    } as unknown as CanvasRenderingContext2D
    drawWatermark(context, 'LightNote', { x: 0.25, y: 0.75, scale: 1 }, 2000, 1000)
    expect(fillText).toHaveBeenCalledWith('LightNote', 500, 750)
    expect(context.font).toContain('45px')
    expect(context.fillStyle).toBe('rgba(255, 255, 255, 0.86)')
    expect(context.save).toHaveBeenCalledTimes(1)
    expect(context.restore).toHaveBeenCalledTimes(1)
  })
})
