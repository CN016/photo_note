import { describe, expect, it } from 'vitest'
import { LIGHT_PALETTES, normalizeLightPalette } from './theme'

const lightTextTokens = {
  ink: '#242a27',
  inkSoft: '#46514c',
  muted: '#66706b',
  onAccent: '#ffffff',
  success: '#416a56',
  warning: '#775f36',
  danger: '#8d514a',
}
const lightPaletteTokens = {
  green: { page: '#eff2ef', accent: '#4b6759', accentHover: '#3e584c' },
  aqua: { page: '#edf4f2', accent: '#426b64', accentHover: '#365a54' },
  blue: { page: '#eef3f8', accent: '#4d6678', accentHover: '#405665' },
  gold: { page: '#f5f1e9', accent: '#705f40', accentHover: '#5e4f34' },
} as const

/** 计算 WCAG 相对亮度与对比度，防止色盘只调整背景却遗漏小字号文字可读性。 */
function contrastRatio(foreground: string, background: string): number {
  const luminance = (hex: string) => {
    const channels = [1, 3, 5].map(index => Number.parseInt(hex.slice(index, index + 2), 16) / 255)
      .map(value => value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4)
    return 0.2126 * channels[0]! + 0.7152 * channels[1]! + 0.0722 * channels[2]!
  }
  const first = luminance(foreground)
  const second = luminance(background)
  return (Math.max(first, second) + 0.05) / (Math.min(first, second) + 0.05)
}

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

  it('所有浅色色盘的正文、弱文字和状态文字都满足普通文本对比度', () => {
    for (const palette of LIGHT_PALETTES) {
      const tokens = lightPaletteTokens[palette]
      for (const foreground of [lightTextTokens.ink, lightTextTokens.inkSoft, lightTextTokens.muted, tokens.accent, tokens.accentHover, lightTextTokens.success, lightTextTokens.warning, lightTextTokens.danger]) {
        expect(contrastRatio(foreground, tokens.page), `${palette} ${foreground}`).toBeGreaterThanOrEqual(4.5)
      }
      expect(contrastRatio(lightTextTokens.onAccent, tokens.accent), `${palette} primary button`).toBeGreaterThanOrEqual(4.5)
    }
  })
})
