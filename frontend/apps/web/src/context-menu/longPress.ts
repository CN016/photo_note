/** 长按只识别静止单指；调用方在滚动、取消、多指或卸载时统一清理。 */
export function createLongPress(open: (x: number, y: number) => void) {
  let timer: ReturnType<typeof setTimeout> | undefined
  let start: { x: number; y: number } | null = null
  let suppressClick = false
  function cancel() { if (timer !== undefined) clearTimeout(timer); timer = undefined; start = null }
  return {
    start(x: number, y: number) {
      cancel(); suppressClick = false; start = { x, y }
      timer = setTimeout(() => { timer = undefined; suppressClick = true; open(x, y) }, 500)
    },
    move(x: number, y: number) { if (start && Math.hypot(x - start.x, y - start.y) > 10) cancel() },
    cancel,
    consumeClick() { const consume = suppressClick; suppressClick = false; return consume },
  }
}
