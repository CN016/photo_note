import { reactive } from 'vue'

/** 跨顶栏与独立模块共享抽屉进度；只保存界面状态，不保存账号或业务数据。 */
export const navigationDrawer = reactive({ progress: 0, dragging: false })
/** 按钮和路由变化使用明确终态；拖动期间由单指手势直接更新0至1的进度。 */
export function setNavigationDrawer(open: boolean): void { navigationDrawer.dragging = false; navigationDrawer.progress = open ? 1 : 0 }
/** 以位移比例判定松手后的终态，避免轻微抖动打开菜单。 */
export function drawerRelease(progress: number, opening: boolean): boolean { return opening ? progress >= 0.3 : progress >= 0.7 }

/** 仅列表与概览接管右划；详情、编辑和公开分享保留自己的交互。 */
export const drawerListRoutes = new Set(['workbench', 'gallery', 'inspiration-statistics', 'portfolios', 'projects', 'project-reviews', 'ledger', 'schedule', 'plans', 'messages', 'spaces', 'organizations', 'organization-console', 'atelier-console', 'delivery'])
