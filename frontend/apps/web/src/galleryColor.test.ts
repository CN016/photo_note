import { describe, expect, it } from 'vitest'
import { galleryHexToHsv, galleryHsvToHex, galleryWheelColorAtPoint } from './galleryColor'

const bounds = { left: 10, top: 20, width: 200, height: 200 }

describe('gallery color wheel', () => {
  it('覆盖完整色相圆环，并在圆心返回白色', () => {
    expect(galleryWheelColorAtPoint(110, 120, bounds)).toMatchObject({ color: '#ffffff', saturation: 0 })
    expect(galleryWheelColorAtPoint(110, 20, bounds)).toMatchObject({ color: '#ff0000', hue: 0, saturation: 1 })
    expect(galleryWheelColorAtPoint(210, 120, bounds)).toMatchObject({ color: '#80ff00', hue: 90, saturation: 1 })
    expect(galleryWheelColorAtPoint(110, 220, bounds)).toMatchObject({ color: '#00ffff', hue: 180, saturation: 1 })
    expect(galleryWheelColorAtPoint(10, 120, bounds)).toMatchObject({ color: '#8000ff', hue: 270, saturation: 1 })
  })

  it('指针超出圆环时夹到圆周，持续提供有效预览色', () => {
    const result = galleryWheelColorAtPoint(800, 900, bounds)
    expect(result.saturation).toBe(1)
    expect(result.color).toMatch(/^#[0-9a-f]{6}$/)
    expect(result.x).toBeGreaterThanOrEqual(0)
    expect(result.x).toBeLessThanOrEqual(200)
    expect(result.y).toBeGreaterThanOrEqual(0)
    expect(result.y).toBeLessThanOrEqual(200)
  })

  it('结合明度可覆盖中间灰和暗色，而不是只返回最亮色', () => {
    expect(galleryWheelColorAtPoint(110, 120, bounds, .5)).toMatchObject({ color: '#808080', saturation: 0 })
    expect(galleryWheelColorAtPoint(110, 20, bounds, .5)).toMatchObject({ color: '#800000', hue: 0, saturation: 1 })
    expect(galleryWheelColorAtPoint(110, 20, bounds, 0)).toMatchObject({ color: '#000000' })
  })

  it('六位十六进制色值可往返转换', () => {
    const hsv = galleryHexToHsv('#2fa3f7')
    expect(galleryHsvToHex(hsv.hue, hsv.saturation, hsv.value)).toBe('#2fa3f7')
    expect(galleryHexToHsv('#ffffff')).toMatchObject({ saturation: 0, value: 1 })
  })
})
