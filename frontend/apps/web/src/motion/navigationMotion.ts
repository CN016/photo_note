import { gsap } from 'gsap'
import { Flip } from 'gsap/Flip'
import type { Router } from 'vue-router'
import { planRouteMotion, type MotionRoute, type RouteMotion, type SharedOrigin } from './routeMotion'

gsap.registerPlugin(Flip)

type CapturedOrigin = SharedOrigin

let installed = false
let capturedOrigin: CapturedOrigin | null = null
let pendingMotion: RouteMotion = { kind: 'page' }
let reducedMotion = false
const rememberedOrigins = new Map<string, SharedOrigin>()

/** 读取 GSAP 需要复现的卡片外框，用真实布局尺寸避免各端手写比例和偏移。 */
function readOrigin(element: HTMLElement): CapturedOrigin {
  const rect = element.getBoundingClientRect()
  const style = getComputedStyle(element)
  return {
    x: rect.x,
    y: rect.y,
    width: rect.width,
    height: rect.height,
    radius: Number.parseFloat(style.borderRadius) || 0,
    shadow: style.boxShadow === 'none' ? 'none' : style.boxShadow,
    scrollX: window.scrollX,
    scrollY: window.scrollY,
  }
}

/** 在路由点击的捕获阶段记录卡片，Vue Router 更新 DOM 前仍能拿到准确几何信息。 */
function captureSharedOrigin(event: Event): void {
  if (reducedMotion || !(event.target instanceof Element)) return
  if (event instanceof MouseEvent && (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)) return
  const marker = event.target.closest<HTMLElement>('[data-shared-transition]')
  if (!marker || marker.closest('[aria-disabled="true"]')) return
  capturedOrigin = readOrigin(marker)
}

/** 检查系统减弱动态效果设置；开启时保留即时页面切换，不执行缩放与位移动画。 */
function updateReducedMotion(): void {
  reducedMotion = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
}

/** 创建不可见定位框，让 Flip.fit 统一计算尺寸、缩放和绝对定位。 */
function createBoundsTarget(origin: SharedOrigin | null): HTMLDivElement {
  const target = document.createElement('div')
  target.setAttribute('aria-hidden', 'true')
  Object.assign(target.style, {
    position: 'fixed',
    left: `${origin?.x ?? 0}px`,
    top: `${origin?.y ?? 0}px`,
    width: `${origin?.width ?? window.innerWidth}px`,
    height: `${origin?.height ?? window.innerHeight}px`,
    pointerEvents: 'none',
    visibility: 'hidden',
    zIndex: '-1',
  })
  document.body.appendChild(target)
  return target
}

/** 过渡期间固定路由根节点，避免页面文档高度参与目标框计算。 */
function pinRouteRoot(element: HTMLElement): void {
  gsap.killTweensOf(element)
  Flip.killFlipsOf(element, false)
  document.body.classList.add('route-motion-active')
  gsap.set(element, {
    position: 'fixed',
    inset: 0,
    zIndex: 80,
    width: '100vw',
    height: '100dvh',
    maxWidth: 'none',
    margin: 0,
    overflow: 'hidden',
    transformOrigin: '0 0',
    willChange: 'transform, opacity',
  })
}

/** 动画结束后恢复 Vue 管理的页面布局，避免影响滚动、吸顶导航和响应式断点。 */
function releaseRouteRoot(element: HTMLElement): void {
  gsap.set(element, {
    clearProps: 'position,inset,top,right,bottom,left,zIndex,width,height,maxWidth,margin,overflow,transformOrigin,transform,willChange,borderRadius,boxShadow',
  })
  document.body.classList.remove('route-motion-active')
}

/** 普通页面切换使用短促淡入上移，详情页始终由共享元素动画主导。 */
function runPageEnter(element: HTMLElement, done: () => void): void {
  if (reducedMotion) {
    releaseRouteRoot(element)
    done()
    return
  }
  gsap.fromTo(element, { opacity: 0, y: 10 }, {
    opacity: 1,
    y: 0,
    duration: 0.22,
    ease: 'power3.out',
    clearProps: 'opacity,transform',
    onComplete: done,
  })
}

/** 从点击卡片开始，将详情页面框放大到完整视口；Timeline 可被反向导航直接中断。 */
function runSharedForward(element: HTMLElement, origin: SharedOrigin, done: () => void): void {
  pinRouteRoot(element)
  const originTarget = createBoundsTarget(origin)
  const fullTarget = createBoundsTarget(null)
  const timeline = gsap.timeline({
    onComplete: () => {
      originTarget.remove()
      fullTarget.remove()
      releaseRouteRoot(element)
      done()
    },
  })

  Flip.fit(element, originTarget, { duration: 0, scale: true, absolute: false })
  gsap.set(element, { borderRadius: origin.radius, boxShadow: origin.shadow })
  const expand = Flip.fit(element, fullTarget, {
    duration: 0.5,
    ease: 'expo.out',
    scale: true,
    absolute: false,
  }) as gsap.core.Tween | null
  timeline
    .add(expand ?? gsap.to(element, { duration: 0.5 }), 0)
    .to(element, { borderRadius: 0, boxShadow: 'none', duration: 0.38, ease: 'power3.out' }, 0)
}

/** 返回时把当前详情页面框缩回原点击区域，并同步恢复原列表滚动位置。 */
function runSharedBack(element: HTMLElement, origin: SharedOrigin, done: () => void): void {
  pinRouteRoot(element)
  const originTarget = createBoundsTarget(origin)
  const timeline = gsap.timeline({
    onComplete: () => {
      originTarget.remove()
      releaseRouteRoot(element)
      restoreScroll(origin)
      done()
    },
  })
  const shrink = Flip.fit(element, originTarget, {
    duration: 0.38,
    ease: 'power3.inOut',
    scale: true,
    absolute: false,
  }) as gsap.core.Tween | null
  timeline
    .add(shrink ?? gsap.to(element, { duration: 0.38 }), 0)
    .to(element, {
      borderRadius: origin.radius,
      boxShadow: origin.shadow,
      duration: 0.38,
      ease: 'power3.inOut',
    }, 0)
}

/** 等待列表重新布局后恢复滚动；首次立即恢复，较长页面加载完成后再校准一次。 */
function restoreScroll(origin: SharedOrigin): void {
  const restore = () => window.scrollTo({ left: origin.scrollX, top: origin.scrollY, behavior: 'instant' as ScrollBehavior })
  restore()
  window.setTimeout(restore, 260)
}

/** Vue Transition 进入前钩子：仅准备共享详情起点，普通页面保留浏览器原生布局。 */
export function beforeRouteEnter(element: Element): void {
  const node = element as HTMLElement
  node.dataset.routeMotion = pendingMotion.kind
  if (pendingMotion.kind !== 'shared-forward' || reducedMotion) return
  pinRouteRoot(node)
  const target = createBoundsTarget(pendingMotion.origin)
  Flip.fit(node, target, { duration: 0, scale: true, absolute: false })
  target.remove()
  gsap.set(node, { borderRadius: pendingMotion.origin.radius, boxShadow: pendingMotion.origin.shadow })
}

/** Vue Transition 进入钩子：运行共享元素或普通页面时间线。 */
export function enterRoute(element: Element, done: () => void): void {
  const node = element as HTMLElement
  if (pendingMotion.kind === 'shared-forward' && !reducedMotion) runSharedForward(node, pendingMotion.origin, done)
  else runPageEnter(node, done)
}

/** Vue Transition 离开钩子：后退时把详情框缩回来源；前进时只淡化旧列表。 */
export function leaveRoute(element: Element, done: () => void): void {
  const node = element as HTMLElement
  node.dataset.exitMotion = pendingMotion.kind
  if (pendingMotion.kind === 'shared-back' && !reducedMotion) {
    runSharedBack(node, pendingMotion.origin, done)
    return
  }
  if (reducedMotion) {
    releaseRouteRoot(node)
    done()
    return
  }
  gsap.to(node, {
    opacity: 0,
    y: -6,
    duration: 0.18,
    ease: 'power2.in',
    onComplete: () => {
      releaseRouteRoot(node)
      done()
    },
  })
}

/** 账号、组织或会话代次变化时清空页面动画历史，避免把上一空间的返回位置带入新空间。 */
export function resetNavigationMotion(): void {
  capturedOrigin = null
  rememberedOrigins.clear()
  pendingMotion = { kind: 'page' }
}

/** 安装全局点击记录与路由方向规划；每个应用生命周期只安装一次。 */
export function installNavigationMotion(router: Router): void {
  if (installed) return
  installed = true
  updateReducedMotion()
  window.matchMedia?.('(prefers-reduced-motion: reduce)').addEventListener?.('change', updateReducedMotion)
  document.addEventListener('click', captureSharedOrigin, true)

  router.beforeEach((to, from) => {
    const toRoute = to as unknown as MotionRoute
    const fromRoute = from as unknown as MotionRoute
    pendingMotion = planRouteMotion(
      toRoute,
      fromRoute,
      capturedOrigin,
      rememberedOrigins.get(fromRoute.fullPath ?? fromRoute.path) ?? null,
    )
    if (pendingMotion.kind === 'shared-forward') rememberedOrigins.set(toRoute.fullPath ?? toRoute.path, pendingMotion.origin)
    capturedOrigin = null
    return true
  })
}
