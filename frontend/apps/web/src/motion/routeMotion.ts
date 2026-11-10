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

/** 详情路由对应的列表父路由；只有这些页面使用卡片到全屏的共享元素动画。 */
const DETAIL_PARENT_BY_ROUTE: Record<string, string> = {
  'edit-inspiration': '/inspirations',
  'project-editor': '/projects',
  'project-review-editor': '/project-reviews',
  'portfolio-detail': '/portfolios',
  'delivery-task': '/delivery',
  'organization-edit-inspiration': '/organization-console',
  'platform-edit-inspiration': '/atelier-7f3c9a2e',
}

/** 返回详情页对应的列表入口，供页面返回按钮与浏览器返回共同识别。 */
export function detailParentPath(route: MotionRoute | null | undefined): string | undefined {
  return route?.name ? DETAIL_PARENT_BY_ROUTE[String(route.name)] : undefined
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
  const forwardParent = detailParentPath(to)
  if (forwardParent === from.path && capturedOrigin) return { kind: 'shared-forward', origin: capturedOrigin }

  const backParent = detailParentPath(from)
  if (backParent === to.path && rememberedOrigin) return { kind: 'shared-back', origin: rememberedOrigin }

  if (to.path === from.path) return { kind: 'none' }
  return { kind: 'page' }
}
