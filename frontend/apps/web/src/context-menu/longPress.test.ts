import { afterEach, describe, expect, it, vi } from 'vitest'
import { createLongPress } from './longPress'
import { menuPosition } from './menu'

afterEach(() => vi.useRealTimers())
describe('图库长按与浮窗边界', () => {
  it('500ms 触发并仅吞掉一次后续点击', () => {
    vi.useFakeTimers()
    const open = vi.fn(), press = createLongPress(open)
    press.start(30, 40); vi.advanceTimersByTime(499); expect(open).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1); expect(open).toHaveBeenCalledWith(30, 40)
    press.cancel(); expect(press.consumeClick()).toBe(true); expect(press.consumeClick()).toBe(false)
  })
  it.each(['move', 'cancel'] as const)('%s 取消后不会误触发', kind => {
    vi.useFakeTimers()
    const open = vi.fn(), press = createLongPress(open)
    press.start(30, 40)
    if (kind === 'move') press.move(41, 40); else press.cancel()
    vi.advanceTimersByTime(600); expect(open).not.toHaveBeenCalled(); expect(press.consumeClick()).toBe(false)
  })
  it('容许10px抖动并重置上一手势定时器', () => {
    vi.useFakeTimers()
    const open = vi.fn(), press = createLongPress(open)
    press.start(30, 40); vi.advanceTimersByTime(300); press.start(50, 60); press.move(56, 68)
    vi.advanceTimersByTime(499); expect(open).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1); expect(open).toHaveBeenCalledOnce(); expect(open).toHaveBeenCalledWith(50, 60)
  })
  it('屏幕四角均留出12px边距', () => {
    expect(menuPosition(380, 790, 320, 400, 390, 800)).toEqual({ left: 58, top: 388 })
    expect(menuPosition(-20, -20, 320, 400, 390, 800)).toEqual({ left: 12, top: 12 })
  })
})
