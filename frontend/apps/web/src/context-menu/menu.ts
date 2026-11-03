import { shallowRef } from 'vue'

/** 菜单动作只在用户点击时执行；子菜单使用同一浮层，避免窄屏溢出。 */
export interface MenuEntry {
  label: string
  disabled?: boolean
  stayOpen?: boolean
  active?: boolean
  danger?: boolean
  children?: MenuEntry[]
  run?: () => void | Promise<void>
}
export interface MenuState {
  id: number; x: number; y: number; items: MenuEntry[]; target: HTMLElement | null; scroll: number
}
export const menuState = shallowRef<MenuState | null>(null)
let sequence = 0
const providers = new WeakMap<HTMLElement, (event: MouseEvent) => boolean>()

/** 按实际 DOM 从内到外寻找业务处理器，使资源节点优先于编辑区和全局。 */
export function registerMenu(element: HTMLElement, handler: (event: MouseEvent) => boolean): () => void {
  providers.set(element, handler)
  return () => { providers.delete(element) }
}
export function dispatchMenu(event: MouseEvent): boolean {
  for (const target of event.composedPath()) {
    if (target instanceof HTMLElement && providers.get(target)?.(event)) return true
  }
  return false
}
export function openMenu(x: number, y: number, items: MenuEntry[], target: HTMLElement | null, scroll = 0): number {
  const id = ++sequence
  menuState.value = { id, x, y, items, target, scroll }
  return id
}
/** 迟到的网络响应不得替换另一图片或新页面的菜单。 */
export function updateMenu(id: number, items: MenuEntry[], scroll = 0): void {
  if (menuState.value?.id === id) menuState.value = { ...menuState.value, items, scroll }
}
export function closeMenu(restoreFocus = false): void {
  const target = menuState.value?.target
  menuState.value = null
  if (restoreFocus && target?.isConnected) {
    const focusable = target.closest<HTMLElement>('button,a,input,textarea,[tabindex],[contenteditable="true"]')
    focusable?.focus({ preventScroll: true })
  }
}

/** 固定位置浮窗始终限制在可视视口中，留出安全区间距。 */
export function menuPosition(x: number, y: number, width: number, height: number, viewportWidth: number, viewportHeight: number) {
  return { left: Math.max(12, Math.min(x, viewportWidth - width - 12)), top: Math.max(12, Math.min(y, viewportHeight - height - 12)) }
}
