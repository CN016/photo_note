import { beforeEach, describe, expect, it, vi } from 'vitest'

const fixture = vi.hoisted(() => ({ ready: true, planned: vi.fn() }))
vi.mock('./imageMotion', () => ({
  captureListImage: () => fixture.ready, discardPendingImage: vi.fn(), resetMotionImages: vi.fn(),
  attachListImage: vi.fn(), completeImageOpen: vi.fn(), removeMotionImage: vi.fn(),
}))
vi.mock('gsap', () => ({ gsap: { registerPlugin: vi.fn() } }))
vi.mock('gsap/Flip', () => ({ Flip: {} }))
vi.mock('./routeMotion', () => ({ planRouteMotion: (...args: unknown[]) => { fixture.planned(...args); return { kind: 'page' } } }))

beforeEach(() => {
  vi.resetModules(); fixture.planned.mockClear(); fixture.ready = true
  vi.stubGlobal('window', { scrollX: 0, scrollY: 1200, matchMedia: () => ({ matches: false, addEventListener: vi.fn() }) })
  vi.stubGlobal('document', { addEventListener: vi.fn() })
  vi.stubGlobal('getComputedStyle', () => ({ borderRadius: '14px', boxShadow: 'none' }))
})

describe('开合动画绑定真实点击卡片', () => {
  it('不同卡片的起点不同，并使用加载完成时的实时位置而非固定屏幕位置', async () => {
    const motion = await import('./navigationMotion')
    let guard: any
    motion.installNavigationMotion({ beforeEach: (fn: any) => { guard = fn } } as any)
    let bounds = { x: 18, y: 90, width: 165, height: 190 }
    const thumbnail = { getBoundingClientRect: () => bounds }
    const marker = { querySelector: () => thumbnail } as any
    for (const rect of [{ x: 18, y: 90 }, { x: 207, y: 610 }, { x: 207, y: 184 }]) {
      bounds = { ...bounds, ...rect }
      motion.capturePreparedOrigin(marker)
      guard({ path: '/inspirations/a' }, { path: '/inspirations' })
      expect(fixture.planned.mock.calls.at(-1)?.[2]).toMatchObject({ ...bounds, scrollY: 1200 })
    }
  })
  it('详情像素准备失败时不从占位框制造动画起点', async () => {
    const motion = await import('./navigationMotion')
    let guard: any
    motion.installNavigationMotion({ beforeEach: (fn: any) => { guard = fn } } as any)
    fixture.ready = false
    motion.capturePreparedOrigin({ querySelector: () => ({}) } as any)
    guard({ path: '/inspirations/a' }, { path: '/inspirations' })
    expect(fixture.planned.mock.calls.at(-1)?.[2]).toBeNull()
  })
})
