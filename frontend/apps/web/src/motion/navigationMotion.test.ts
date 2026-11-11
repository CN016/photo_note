import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { RouteMotion } from './routeMotion'

const fixture = vi.hoisted(() => ({ planned: { kind: 'page' } as RouteMotion, timelines: [] as any[] }))
vi.mock('./routeMotion', () => ({ planRouteMotion: () => fixture.planned }))
vi.mock('gsap/Flip', () => ({ Flip: { fit: vi.fn(), killFlipsOf: vi.fn() } }))
vi.mock('gsap', () => ({ gsap: {
  registerPlugin: vi.fn(), killTweensOf: vi.fn(), set: vi.fn(), fromTo: vi.fn(), to: vi.fn(),
  timeline: vi.fn(() => {
    const callbacks: Record<string, (() => void) | null> = {}
    const timeline: any = { callbacks }
    for (const name of ['to', 'play', 'pause', 'reverse', 'timeScale', 'kill', 'progress']) timeline[name] = vi.fn(() => timeline)
    timeline.time = vi.fn(() => 0.18)
    timeline.eventCallback = vi.fn((name: string, callback: (() => void) | null) => { callbacks[name] = callback; return timeline })
    fixture.timelines.push(timeline)
    return timeline
  }),
} }))

const origin = { x: 20, y: 150, width: 180, height: 225, radius: 14, shadow: 'none', scrollX: 0, scrollY: 1600 }
let guard: (to: any, from: any) => void
let motion: typeof import('./navigationMotion')
const detail = () => ({ dataset: {}, style: {} }) as unknown as HTMLElement

/** 用 GSAP 替身验证时间线生命周期和滚动保护；真实插值及布局另由浏览器逐帧验收。 */
beforeEach(async () => {
  vi.resetModules(); vi.clearAllMocks(); fixture.timelines.length = 0
  vi.stubGlobal('window', { innerWidth: 390, innerHeight: 844, scrollX: 0, scrollY: 1600,
    scrollTo: vi.fn(), matchMedia: () => ({ matches: false, addEventListener: vi.fn() }) })
  vi.stubGlobal('document', {
    documentElement: { scrollHeight: 4200 },
    body: { style: { minHeight: '', overflowAnchor: '' }, classList: { add: vi.fn(), remove: vi.fn() }, appendChild: vi.fn() },
    addEventListener: vi.fn(),
    createElement: () => ({ style: {}, setAttribute: vi.fn(), remove: vi.fn() }),
  })
  motion = await import('./navigationMotion')
  motion.installNavigationMotion({ beforeEach: (fn: typeof guard) => { guard = fn } } as any)
})
function plan(kind: 'shared-forward' | 'shared-back') {
  fixture.planned = { kind, origin }
  guard({ name: 'edit-inspiration', path: '/inspirations/a' }, { name: 'gallery', path: '/inspirations' })
}
function open(node: HTMLElement) {
  plan('shared-forward'); motion.beforeRouteEnter(node); motion.enterRoute(node, vi.fn())
}

describe('可中断窗口动画', () => {
  it('中途返回倒放同一时间线且保留页面高度直至详情移除', () => {
    const node = detail(), done = vi.fn()
    open(node)
    expect(document.body.style.minHeight).toBe('4200px')
    motion.cancelRouteEnter(node)
    plan('shared-back'); motion.leaveRoute(node, done)
    expect(fixture.timelines).toHaveLength(1)
    expect(fixture.timelines[0].reverse).toHaveBeenCalledOnce()
    expect(done).not.toHaveBeenCalled()
    expect(document.body.style.minHeight).toBe('4200px')
    fixture.timelines[0].callbacks.onReverseComplete()
    expect(done).toHaveBeenCalledOnce()
    expect(document.body.style.minHeight).toBe('')
    expect(window.scrollTo).toHaveBeenLastCalledWith({ left: 0, top: 1600, behavior: 'instant' })
  })
  it('完整展开后返回也复用原轨迹并再次保护列表高度', async () => {
    const { gsap } = await import('gsap')
    const node = detail()
    open(node); fixture.timelines[0].callbacks.onComplete()
    expect(document.body.style.minHeight).toBe('')
    const kills = vi.mocked(gsap.killTweensOf).mock.calls.length
    plan('shared-back'); motion.leaveRoute(node, vi.fn())
    expect(gsap.killTweensOf).toHaveBeenCalledTimes(kills)
    expect(fixture.timelines).toHaveLength(1)
    expect(fixture.timelines[0].reverse).toHaveBeenCalledOnce()
    expect(document.body.style.minHeight).toBe('4200px')
  })
  it('列表激活取消旧离场 Tween，避免迟到回调隐藏列表', async () => {
    const { gsap } = await import('gsap')
    const node = detail()
    motion.cancelRouteLeave(node)
    expect(gsap.killTweensOf).toHaveBeenCalledWith(node)
    expect(gsap.set).toHaveBeenLastCalledWith(node, { clearProps: 'opacity,transform' })
  })
  it('收回途中再次打开取消旧关闭回调并继续正放同一时间线', () => {
    const node = detail()
    open(node); plan('shared-back'); motion.leaveRoute(node, vi.fn())
    motion.cancelRouteLeave(node)
    plan('shared-forward'); motion.beforeRouteEnter(node); motion.enterRoute(node, vi.fn())
    expect(fixture.timelines).toHaveLength(1)
    expect(fixture.timelines[0].callbacks.onReverseComplete).toBeNull()
    expect(fixture.timelines[0].play).toHaveBeenCalledTimes(2)
  })
  it('切换会话清理未完成时间线与文档高度保护', () => {
    open(detail()); motion.resetNavigationMotion()
    expect(fixture.timelines[0].kill).toHaveBeenCalledOnce()
    expect(document.body.style.minHeight).toBe('')
  })
})
