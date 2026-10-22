import { readonly, shallowRef } from 'vue'

export type ColorTheme = 'light' | 'dark'
const THEME_KEY = 'photo-nini.theme'
const selectedTheme = shallowRef<ColorTheme>('light')

/** 桌面侧栏与手机顶栏可能同时挂载，所有切换入口共享状态，避免图标与当前主题不一致。 */
export function useColorTheme() { return readonly(selectedTheme) }

/** 启动时恢复用户已选主题；首次使用采用浅色，避免改变现有默认视觉。 */
export function initializeTheme(): ColorTheme {
  const saved = localStorage.getItem(THEME_KEY)
  const theme: ColorTheme = saved === 'dark' ? 'dark' : 'light'
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

/** 同步站点、组件库和浏览器顶栏；配色全部由 CSS 令牌提供，避免行内值覆盖暗色浮层。 */
function applyTheme(theme: ColorTheme): void {
  const root = document.documentElement
  root.dataset.theme = theme
  root.classList.toggle('dark', theme === 'dark')
  selectedTheme.value = theme
  const pageColor = getComputedStyle(root).getPropertyValue('--page').trim()
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', pageColor)
}

/** 读取根节点当前主题，供共享控件初始化显示状态。 */
export function currentTheme(): ColorTheme {
  return document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light'
}
