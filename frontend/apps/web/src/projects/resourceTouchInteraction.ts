/** 安卓容器可能报告鼠标主指针；移动 UA 作为补充，桌面触屏电脑仍保留鼠标原生拖放。 */
export function usesTouchResourceDrag(): boolean {
  return (typeof matchMedia !== 'undefined' && matchMedia('(pointer: coarse)').matches)
    || (typeof navigator !== 'undefined' && /Android|iPhone|iPad|iPod/.test(navigator.userAgent))
}

/** 原子媒体自行管理鼠标/触摸选择，禁止 ProseMirror 再处理兼容鼠标事件并唤起输入法。 */
export function stopResourceEditorEvent({ event }: { event: Event }): boolean {
  if (event.type === 'dragstart' && usesTouchResourceDrag()) {
    event.preventDefault()
    return true
  }
  return !['dragstart', 'dragover', 'dragenter', 'dragleave', 'dragend', 'drop', 'copy', 'cut', 'paste'].includes(event.type)
}

/** 在同层块中寻找最近的可视块，兼容同排小图的水平插入和独占一行块的垂直插入。 */
export function resourceDropSide(rect: { left: number; right: number; top: number; bottom: number }, x: number, y: number, inline: boolean): 'before' | 'after' {
  return (inline ? x > (rect.left + rect.right) / 2 : y > (rect.top + rect.bottom) / 2) ? 'after' : 'before'
}
