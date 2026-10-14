export type ColorTheme = 'light' | 'dark'
const THEME_KEY = 'photo-nini.theme'

/** 启动时恢复用户已选主题；首次使用采用浅色，避免改变现有默认视觉。 */
export function initializeTheme(): ColorTheme {
  const saved = localStorage.getItem(THEME_KEY)
  const theme: ColorTheme = saved === 'dark' ? 'dark' : 'light'
  document.documentElement.dataset.theme = theme
  return theme
}

/** 保存主题偏好并立即更新根节点，让所有页面共享同一配色状态。 */
export function toggleTheme(): ColorTheme {
  const next: ColorTheme = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark'
  document.documentElement.dataset.theme = next
  localStorage.setItem(THEME_KEY, next)
  return next
}

/** 读取根节点当前主题，供共享控件初始化显示状态。 */
export function currentTheme(): ColorTheme {
  return document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light'
}
