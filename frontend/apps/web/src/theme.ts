import { readonly, shallowRef } from 'vue'

export type ColorTheme = 'light' | 'dark'
export const LIGHT_PALETTES = ['green', 'aqua', 'gold', 'blue'] as const
export type LightPalette = typeof LIGHT_PALETTES[number]
const THEME_KEY = 'photo-nini.theme'
const PALETTE_KEY = 'photo-nini.light-palette'
const selectedTheme = shallowRef<ColorTheme>('light')
const selectedPalette = shallowRef<LightPalette>('green')

/** 桌面侧栏与手机顶栏可能同时挂载，所有切换入口共享状态，避免图标与当前主题不一致。 */
export function useColorTheme() { return readonly(selectedTheme) }
/** 浅色色盘与明暗模式分开保存，切回浅色时恢复用户上次选择。 */
export function useLightPalette() { return readonly(selectedPalette) }

/** 过滤本地存储中的未知值，避免升级或手工修改后落入无样式状态。 */
export function normalizeLightPalette(value: string | null): LightPalette {
  return LIGHT_PALETTES.includes(value as LightPalette) ? value as LightPalette : 'green'
}

/** 启动时恢复明暗模式与浅色色盘；首次使用沿用当前玻璃绿浅色主题。 */
export function initializeTheme(): ColorTheme {
  const saved = localStorage.getItem(THEME_KEY)
  const theme: ColorTheme = saved === 'dark' ? 'dark' : 'light'
  selectedPalette.value = normalizeLightPalette(localStorage.getItem(PALETTE_KEY))
  applyTheme(theme)
  return theme
}

/** 保存主题偏好并立即更新根节点，让所有页面共享同一配色状态。 */
export function toggleTheme(): ColorTheme {
  const next: ColorTheme = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark'
  applyTheme(next)
  localStorage.setItem(THEME_KEY, next)
  return next
}

/** 色盘切换会直接进入浅色模式，使一次点击即可看到完整主题变化。 */
export function setLightPalette(palette: LightPalette): LightPalette {
  selectedPalette.value = palette
  localStorage.setItem(PALETTE_KEY, palette)
  localStorage.setItem(THEME_KEY, 'light')
  applyTheme('light')
  return palette
}

/** 同步站点、组件库和浏览器顶栏；配色全部由 CSS 令牌提供，避免行内值覆盖暗色浮层。 */
function applyTheme(theme: ColorTheme): void {
  const root = document.documentElement
  root.dataset.theme = theme
  root.dataset.palette = selectedPalette.value
  root.classList.toggle('dark', theme === 'dark')
  selectedTheme.value = theme
  const pageColor = getComputedStyle(root).getPropertyValue('--page').trim()
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', pageColor)
}

/** 读取根节点当前主题，供共享控件初始化显示状态。 */
export function currentTheme(): ColorTheme {
  return document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light'
}
