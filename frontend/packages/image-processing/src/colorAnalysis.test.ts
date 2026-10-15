import { describe, expect, it } from 'vitest'
import { analyzeColorPixels, isColorAnalysis } from './colorAnalysis'

/** 建立由 RGBA 元组重复组成的小型像素样本，便于验证色彩占比。 */
function pixelsFrom(colors: Array<{ rgba: [number, number, number, number]; count: number }>): Uint8ClampedArray {
  const pixels: number[] = []
  for (const { rgba, count } of colors) {
    for (let index = 0; index < count; index += 1) pixels.push(...rgba)
  }
  return new Uint8ClampedArray(pixels)
}

describe('灵感图颜色画像', () => {
  it('为纯色图片生成单一主色且占比为 100%', () => {
    const profile = analyzeColorPixels(pixelsFrom([{ rgba: [52, 105, 170, 255], count: 20 }]))

    expect(profile.palette).toEqual([{ hex: '#3469AA', percentage: 100 }])
    expect(profile.other_percentage).toBe(0)
    expect(isColorAnalysis(profile)).toBe(true)
  })

  it('按像素频率提取双色比例并让占比合计为 100%', () => {
    const profile = analyzeColorPixels(pixelsFrom([
      { rgba: [255, 0, 0, 255], count: 80 },
      { rgba: [0, 0, 255, 255], count: 20 },
    ]))

    expect(profile.palette).toEqual([
      { hex: '#FF0000', percentage: 80 },
      { hex: '#0000FF', percentage: 20 },
    ])
    expect(profile.palette.reduce((sum, color) => sum + color.percentage, profile.other_percentage)).toBe(100)
  })

  it('将透明区域合成白色后计算平均亮度和饱和度', () => {
    const profile = analyzeColorPixels(pixelsFrom([
      { rgba: [255, 0, 0, 0], count: 1 },
      { rgba: [0, 0, 0, 255], count: 2 },
    ]))

    expect(profile.palette[0].hex).toBe('#000000')
    expect(profile.palette[1].hex).toBe('#FFFFFF')
    expect(profile.average_brightness).toBe(33.3)
    expect(profile.average_saturation).toBe(0)
  })

  it('只保留前五种主色，并将剩余颜色占比归入其他', () => {
    const profile = analyzeColorPixels(pixelsFrom([
      { rgba: [255, 0, 0, 255], count: 20 },
      { rgba: [0, 255, 0, 255], count: 18 },
      { rgba: [0, 0, 255, 255], count: 16 },
      { rgba: [255, 255, 0, 255], count: 14 },
      { rgba: [0, 255, 255, 255], count: 12 },
      { rgba: [255, 0, 255, 255], count: 10 },
      { rgba: [20, 20, 20, 255], count: 10 },
    ]))

    expect(profile.palette).toHaveLength(5)
    expect(profile.other_percentage).toBe(20)
    expect(profile.palette.reduce((sum, color) => sum + color.percentage, profile.other_percentage)).toBe(100)
  })

  it('拒绝空或不完整的像素缓冲区', () => {
    expect(() => analyzeColorPixels(new Uint8ClampedArray())).toThrow('image_pixels_invalid')
    expect(() => analyzeColorPixels(new Uint8ClampedArray([0, 0, 0]))).toThrow('image_pixels_invalid')
  })
})
