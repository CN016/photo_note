import { afterEach, describe, expect, it, vi } from 'vitest'
import { resourceDropSide, stopResourceEditorEvent, usesTouchResourceDrag } from './resourceTouchInteraction'

/** 防止移动端兼容鼠标事件重新进入编辑器，以及同排缩略图落点退化成纯垂直判断。 */
describe('媒体触屏事件与落点', () => {
  it.each(['touchstart', 'touchend', 'mousedown', 'mouseup', 'click', 'pointerdown'])('拦截 %s 的编辑器默认处理', type => {
    expect(stopResourceEditorEvent({ event: new Event(type) })).toBe(true)
  })
  it.each(['copy', 'cut', 'paste', 'drop', 'dragover'])('保留 %s 的原生编辑器管线', type => {
    expect(stopResourceEditorEvent({ event: new Event(type) })).toBe(false)
  })
  it('小图按左右半区预览同排落点，附件按上下半区预览落点', () => {
    const rect = { left: 10, right: 110, top: 20, bottom: 220 }
    expect(resourceDropSide(rect, 20, 200, true)).toBe('before')
    expect(resourceDropSide(rect, 100, 30, true)).toBe('after')
    expect(resourceDropSide(rect, 100, 30, false)).toBe('before')
    expect(resourceDropSide(rect, 20, 200, false)).toBe('after')
  })
})

// 桌面浏览器可能暴露触点，不能仅靠 maxTouchPoints 禁用鼠标原生拖动。
describe('媒体输入平台选择', () => {
  afterEach(() => vi.unstubAllGlobals())
  it('桌面触屏能力不替换鼠标排序方式', () => {
    vi.stubGlobal('navigator', { userAgent: 'Windows Chrome', maxTouchPoints: 10 })
    vi.stubGlobal('matchMedia', () => ({ matches: false }))
    expect(usesTouchResourceDrag()).toBe(false)
  })
  it('安卓容器即使报告鼠标指针仍使用触屏排序', () => {
    vi.stubGlobal('navigator', { userAgent: 'Mozilla Android Gecko', maxTouchPoints: 1 })
    vi.stubGlobal('matchMedia', () => ({ matches: false }))
    expect(usesTouchResourceDrag()).toBe(true)
    const event = new Event('dragstart', { cancelable: true })
    expect(stopResourceEditorEvent({ event })).toBe(true)
    expect(event.defaultPrevented).toBe(true)
  })
})
