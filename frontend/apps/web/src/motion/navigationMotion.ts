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
function pinRouteRoot(element: HTMLElement, preserveTimeline = false): void {
  // 完整展开后重新固定窗口时，原时间线还要倒放，不能杀掉其内部 Tween。
  if (!preserveTimeline) { gsap.killTweensOf(element); Flip.killFlipsOf(element, false) }
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
  if (!scrollLocks.size) document.body.classList.remove('route-motion-active')
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

/** 每个详情持有同一条开合时间线，中途返回只反转当前进度，不另建竞争动画。 */
interface WindowMotion {
  timeline: gsap.core.Timeline
  origin: SharedOrigin
  pinned: boolean
  reversing: boolean
}
const windowMotions = new Map<HTMLElement, WindowMotion>()
const scrollLocks = new Set<HTMLElement>()
let originalMinHeight = ''
let originalOverflowAnchor = ''

/** 固定详情期间保留列表文档高度，防止旧列表被缓存后 window.scrollY 被浏览器夹到零。 */
function holdScrollExtent(element: HTMLElement, origin: SharedOrigin): void {
  if (!scrollLocks.size) {
    originalMinHeight = document.body.style.minHeight
    originalOverflowAnchor = document.body.style.overflowAnchor
  }
  scrollLocks.add(element)
  document.body.style.minHeight = `${Math.max(document.documentElement.scrollHeight, origin.scrollY + window.innerHeight)}px`
  document.body.style.overflowAnchor = 'none'
}

/** 最后一个过渡退出才释放页面高度，快速导航不能释放另一条动画的滚动保护。 */
function freeScrollExtent(element: HTMLElement): void {
  scrollLocks.delete(element)
  if (scrollLocks.size) return
  document.body.style.minHeight = originalMinHeight
  document.body.style.overflowAnchor = originalOverflowAnchor
  document.body.classList.remove('route-motion-active')
}

/** 使用 GSAP 的 back 弹性曲线，从实际点击框蹦出展开；同一时间线支持原进度倒放。 */
function runSharedForward(element: HTMLElement, origin: SharedOrigin, done: () => void): void {
  const existing = windowMotions.get(element)
  if (existing) {
    // 收回途中再次打开时继续正放，禁止旧的 reverseComplete 再移除当前窗口。
    existing.reversing = false
    existing.timeline.eventCallback('onReverseComplete', null)
    existing.timeline.eventCallback('onComplete', () => {
      releaseRouteRoot(element); existing.pinned = false; freeScrollExtent(element)
      window.scrollTo({ top: 0, left: 0, behavior: 'instant' as ScrollBehavior }); done()
    })
    existing.timeline.play()
    return
  }
  holdScrollExtent(element, origin)
  pinRouteRoot(element)
  const target = createBoundsTarget(origin)
  Flip.fit(element, target, { duration: 0, scale: true, absolute: false })
  target.remove()
  gsap.set(element, { borderRadius: Math.max(origin.radius, 18), boxShadow: '0 24px 90px rgb(0 0 0 / 24%)' })
  const timeline = gsap.timeline({ paused: true })
  const state: WindowMotion = { timeline, origin, pinned: true, reversing: false }
  windowMotions.set(element, state)
  timeline.to(element, {
    x: 0, y: 0, scaleX: 1, scaleY: 1, borderRadius: 0,
    boxShadow: '0 0 0 rgb(0 0 0 / 0%)',
    duration: 0.7, ease: 'back.out(1.35)',
  })
  timeline.eventCallback('onComplete', () => {
    if (state.reversing) return
    releaseRouteRoot(element)
    state.pinned = false
    freeScrollExtent(element)
    window.scrollTo({ top: 0, left: 0, behavior: 'instant' as ScrollBehavior })
    done()
  })
  timeline.play(0)
}

/** 反向播放已存在的窗口时间线；中断时保留当前变换，禁止重新固定或跳到完整展开状态。 */
function runSharedBack(element: HTMLElement, origin: SharedOrigin, done: () => void): void {
  let state = windowMotions.get(element)
  if (!state) {
    // 非本次实例打开的详情沿用相同开合轨迹，但从完整窗口开始倒放。
    runSharedForward(element, origin, () => {})
    state = windowMotions.get(element)!
    state.timeline.pause().progress(1, true)
  }
  holdScrollExtent(element, origin)
  state.timeline.pause()
  state.reversing = true
  if (!state.pinned) {
    pinRouteRoot(element, true)
    gsap.set(element, { x: 0, y: 0, scaleX: 1, scaleY: 1 })
    state.pinned = true
  }
  const finish = () => {
    state!.timeline.kill()
    windowMotions.delete(element)
    // 保持详情固定定位直至移除，避免退出最后一帧触发滚动锚定。
    done()
    releaseRouteRoot(element)
    freeScrollExtent(element)
    if (!scrollLocks.size) restoreScroll(origin)
  }
  state.timeline.eventCallback('onComplete', null)
  state.timeline.eventCallback('onReverseComplete', finish)
  if (state.timeline.time() <= 0.001) finish()
  else state.timeline.timeScale(1).reverse()
}

/** Vue 取消进入动画时只暂停；随后 leave 钩子会倒放同一条时间线。 */
export function cancelRouteEnter(element: Element): void {
  const node = element as HTMLElement
  const state = windowMotions.get(node)
  if (state) state.timeline.pause()
  else gsap.killTweensOf(node)
}

/** 缓存列表被重新激活时取消旧离场回调，防止它稍后再把列表隐藏或改变滚动。 */
export function cancelRouteLeave(element: Element): void {
  const node = element as HTMLElement
  const state = windowMotions.get(node)
  if (state) { state.timeline.pause(); return }
  gsap.killTweensOf(node)
  releaseRouteRoot(node)
  gsap.set(node, { clearProps: 'opacity,transform' })
}

/** 缓存列表已保留布局，即时恢复位置，禁止延迟二次滚动覆盖用户返回后的操作。 */
function restoreScroll(origin: SharedOrigin): void {
  window.scrollTo({ left: origin.scrollX, top: origin.scrollY, behavior: 'instant' as ScrollBehavior })
}

/** Vue Transition 进入前钩子：仅准备共享详情起点，普通页面保留浏览器原生布局。 */
export function beforeRouteEnter(element: Element): void {
  const node = element as HTMLElement
  if (windowMotions.has(node)) return
  gsap.set(node, { clearProps: 'opacity,transform' })
  node.dataset.routeMotion = pendingMotion.kind
  // 返回列表在首帧绘制前就到达原位置；离开的详情覆盖在其上执行缩回动画。
  if (pendingMotion.kind === 'shared-back') { restoreScroll(pendingMotion.origin); return }
  if (pendingMotion.kind !== 'shared-forward' || reducedMotion) return
  holdScrollExtent(node, pendingMotion.origin)
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
  else if (pendingMotion.kind === 'shared-back') { gsap.set(node, { clearProps: 'opacity,transform' }); restoreScroll(pendingMotion.origin); done() }
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
  const previous = windowMotions.get(node)
  if (previous) { previous.timeline.kill(); windowMotions.delete(node); freeScrollExtent(node) }
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
  for (const [node, state] of windowMotions) { state.timeline.kill(); releaseRouteRoot(node); freeScrollExtent(node) }
  windowMotions.clear()
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
