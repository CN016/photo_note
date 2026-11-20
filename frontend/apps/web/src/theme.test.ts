import { describe, expect, it } from 'vitest'
import { LIGHT_PALETTES, normalizeLightPalette } from './theme'

describe('浅色色盘', () => {
  it('只接受已注册的色盘值', () => {
    expect(LIGHT_PALETTES).toEqual(['green', 'aqua', 'gold', 'blue'])
    for (const palette of LIGHT_PALETTES) expect(normalizeLightPalette(palette)).toBe(palette)
  })

  it('无效或旧值回退到默认玻璃绿', () => {
    expect(normalizeLightPalette(null)).toBe('green')
    expect(normalizeLightPalette('orange')).toBe('green')
    expect(normalizeLightPalette('unexpected')).toBe('green')
  })
})
