export type ColorTheme = 'light' | 'dark'
const THEME_KEY = 'photo-nini.theme'

/** 启动时恢复用户已选主题；首次使用采用浅色，避免改变现有默认视觉。 */
export function initializeTheme(): ColorTheme {
  const saved = localStorage.getItem(THEME_KEY)
  const theme: ColorTheme = saved === 'dark' ? 'dark' : 'light'
  document.documentElement.dataset.theme = theme
  syncElementTheme(theme)
  return theme
}

/** 保存主题偏好并立即更新根节点，让所有页面共享同一配色状态。 */
export function toggleTheme(): ColorTheme {
  const next: ColorTheme = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark'
  document.documentElement.dataset.theme = next
  syncElementTheme(next)
  localStorage.setItem(THEME_KEY, next)
  return next
}

/** 将站点现有颜色令牌映射到 Element Plus，保证内置表单、弹层与明暗主题一致。 */
function syncElementTheme(theme: ColorTheme): void {
  const root = document.documentElement
  root.style.setProperty('--el-color-primary', theme === 'dark' ? '#df8b78' : '#df7c67')
  root.style.setProperty('--el-color-primary-light-3', theme === 'dark' ? '#e9a395' : '#e89b8b')
  root.style.setProperty('--el-color-primary-light-5', theme === 'dark' ? '#efbaae' : '#efb7aa')
  root.style.setProperty('--el-color-primary-light-7', theme === 'dark' ? '#f5d2ca' : '#f6d3cb')
  root.style.setProperty('--el-color-primary-light-8', theme === 'dark' ? '#f8dfda' : '#f9e2dc')
  root.style.setProperty('--el-color-primary-light-9', theme === 'dark' ? '#faebe8' : '#fcf1ee')
  root.style.setProperty('--el-color-primary-dark-2', theme === 'dark' ? '#c76f5e' : '#c96855')
  root.style.setProperty('--el-border-radius-base', '4px')
  root.style.setProperty('--el-border-radius-small', '4px')
  root.style.setProperty('--el-border-radius-round', '4px')
}

/** 读取根节点当前主题，供共享控件初始化显示状态。 */
export function currentTheme(): ColorTheme {
  return document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light'
}
