import { describe, expect, it } from 'vitest'
import { LIGHT_PALETTES, normalizeLightPalette } from './theme'

const lightTextTokens = {
  ink: '#26332c',
  inkSoft: '#3d4a43',
  muted: '#48574f',
  onAccent: '#fffaf3',
  success: '#3f6855',
  warning: '#74582f',
  danger: '#8a4c44',
}
const lightPaletteTokens = {
  green: { page: '#dcecdf', accent: '#3f6754', accentHover: '#355b47' },
  aqua: { page: '#d3f3ed', accent: '#1f685c', accentHover: '#1b5d52' },
  blue: { page: '#dcecf8', accent: '#2f6385', accentHover: '#295775' },
  gold: { page: '#f8e8bd', accent: '#745412', accentHover: '#68490e' },
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
