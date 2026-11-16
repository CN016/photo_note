import { describe, expect, it } from 'vitest'
import { drawerRelease, drawerListRoutes, navigationDrawer, setNavigationDrawer } from './navigationDrawer'

/** 验证抽屉阈值与路由边界，编辑器/公开页面不能被全局列表手势接管。 */
describe('全局菜单抽屉', () => {
  it('打开与关闭使用不同松手阈值，短距离轻划回到原状态', () => {
    expect(drawerRelease(0.29, true)).toBe(false)
    expect(drawerRelease(0.3, true)).toBe(true)
    expect(drawerRelease(0.71, false)).toBe(true)
    expect(drawerRelease(0.69, false)).toBe(false)
  })
  it('按钮和会话清理始终结束拖动并回到明确终态', () => {
    navigationDrawer.dragging = true; navigationDrawer.progress = 0.4
    setNavigationDrawer(true)
    expect(navigationDrawer).toMatchObject({ progress: 1, dragging: false })
    setNavigationDrawer(false)
    expect(navigationDrawer.progress).toBe(0)
  })
  it('包括独立交付列表并排除详情与公开分享', () => {
    for (const name of ['delivery', 'gallery', 'projects', 'plans', 'messages', 'portfolios', 'project-reviews']) expect(drawerListRoutes.has(name)).toBe(true)
    for (const name of ['delivery-task', 'edit-inspiration', 'project-editor', 'public-delivery', 'public-portfolio']) expect(drawerListRoutes.has(name)).toBe(false)
  })
})
