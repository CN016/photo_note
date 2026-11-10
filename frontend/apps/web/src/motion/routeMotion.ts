/**
 * 页面动效规划与 DOM 操作解耦，便于单测验证列表页、详情页的往返关系。
 * 实际插值、吸附和中断由 GSAP Flip 与 GSAP Timeline 完成。
 */
export interface MotionRoute {
  name?: string | null
  path: string
  fullPath?: string
}

export interface SharedOrigin {
  x: number
  y: number
  width: number
  height: number
  radius: number
  shadow: string
  scrollX: number
  scrollY: number
}

export type RouteMotion =
  | { kind: 'shared-forward'; origin: SharedOrigin }
  | { kind: 'shared-back'; origin: SharedOrigin }
  | { kind: 'page' }
  | { kind: 'none' }

/** 详情路由可返回的列表入口；消息中心和作品集等页面也能成为卡片动画来源。 */
const DETAIL_PARENTS_BY_ROUTE: Record<string, readonly string[]> = {
  'edit-inspiration': ['/inspirations', '/messages'],
  'project-editor': ['/projects'],
  'project-review-editor': ['/project-reviews'],
  'portfolio-detail': ['/portfolios'],
  'delivery-task': ['/delivery', '/messages'],
  'organization-edit-inspiration': ['/organization-console'],
  'platform-edit-inspiration': ['/atelier-7f3c9a2e'],
}

/** 返回详情路由支持的首个列表入口，供返回按钮与浏览器返回共同识别。 */
export function detailParentPath(route: MotionRoute | null | undefined): string | undefined {
  return detailParentPaths(route)[0]
}

/** 读取一个详情路由全部合法来源，避免消息中心等二级入口退回普通淡入动画。 */
export function detailParentPaths(route: MotionRoute | null | undefined): readonly string[] {
  return route?.name ? DETAIL_PARENTS_BY_ROUTE[String(route.name)] ?? [] : []
}

/**
 * 结合来源点击区域判断本段导航应使用共享元素、普通页面或不做动画。
 * 后退时必须已有上次打开详情时记录的原卡片位置，直接打开详情则安全回退为普通动画。
 */
export function planRouteMotion(
  to: MotionRoute,
  from: MotionRoute,
  capturedOrigin: SharedOrigin | null,
  rememberedOrigin: SharedOrigin | null,
): RouteMotion {
  if (detailParentPaths(to).includes(from.path) && capturedOrigin) return { kind: 'shared-forward', origin: capturedOrigin }

  if (detailParentPaths(from).includes(to.path) && rememberedOrigin) return { kind: 'shared-back', origin: rememberedOrigin }

  if (to.path === from.path) return { kind: 'none' }
  return { kind: 'page' }
}
