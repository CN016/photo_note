import { onBeforeUnmount, ref } from 'vue'

/** 响应式移动端判断只用于改变信息层级，不替代 CSS 断点；窄屏默认收起低频工具。 */
export function useCompactViewport(maxWidth = 768) {
  const compact = ref(false)
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return compact

  const query = window.matchMedia(`(max-width: ${maxWidth}px)`)
  const update = () => { compact.value = query.matches }
  update()
  query.addEventListener('change', update)
  onBeforeUnmount(() => query.removeEventListener('change', update))
  return compact
}
