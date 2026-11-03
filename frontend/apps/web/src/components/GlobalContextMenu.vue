<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ArrowLeft, ArrowRight, Check } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../stores/auth'
import { closeMenu, dispatchMenu, menuPosition, menuState, openMenu, type MenuEntry } from '../context-menu/menu'
import { defaultEntries } from '../context-menu/clipboard'

const route = useRoute()
const auth = useAuthStore()
const { t, locale } = useI18n()
const panel = ref<HTMLElement>()
const left = ref(12), top = ref(12)
const stack = ref<MenuEntry[][]>([])
const entries = computed(() => stack.value.at(-1) ?? menuState.value?.items ?? [])

/** 根捕获阶段统一阻止桌面原生菜单；手机普通文本保留系统选词。 */
function context(event: MouseEvent) {
  const touch = (event as MouseEvent & { sourceCapabilities?: { firesTouchEvents: boolean } }).sourceCapabilities?.firesTouchEvents
    || (event as PointerEvent).pointerType === 'touch'
    || (event.isTrusted && (matchMedia('(pointer: coarse)').matches || /Android|iPhone|iPad|iPod/.test(navigator.userAgent)))
  if (touch && !(event.target as Element)?.closest('[data-inspiration-card]')) return
  event.preventDefault()
  if ((event.target as Element)?.closest('.app-context-menu')) return
  closeMenu()
  if (dispatchMenu(event)) return
  const target = event.target instanceof HTMLElement ? event.target : document.body
  openMenu(event.clientX, event.clientY, defaultEntries(target), target)
}
function outside(event: Event) { if (panel.value && !event.composedPath().includes(panel.value)) closeMenu() }
function buttons() { return Array.from(panel.value?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? []) }
function keydown(event: KeyboardEvent) {
  if (!menuState.value) {
    if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
      event.preventDefault()
      const target = document.activeElement instanceof HTMLElement ? document.activeElement : document.body
      const rect = target.getBoundingClientRect()
      target.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: rect.left + 12, clientY: rect.top + 12, button: 2 }))
    }
    return
  }
  if (event.key === 'Escape') { event.preventDefault(); closeMenu(true) }
  if (event.key === 'Tab') closeMenu()
  if (event.key === 'ArrowRight') {
    const focused = document.activeElement as HTMLButtonElement | null
    if (focused?.getAttribute('aria-haspopup') === 'menu') { event.preventDefault(); focused.click() }
  }
  if (event.key === 'ArrowLeft' && stack.value.length) { event.preventDefault(); stack.value.pop() }
  if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
    event.preventDefault()
    const all = buttons(), index = all.indexOf(document.activeElement as HTMLButtonElement)
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? all.length - 1 : (index + (event.key === 'ArrowDown' ? 1 : -1) + all.length) % all.length
    all[next]?.focus()
  }
}
async function activate(entry: MenuEntry) {
  if (entry.disabled) return
  if (entry.children) { stack.value.push(entry.children); return }
  try {
    const result = entry.run?.()
    if (!entry.stayOpen) closeMenu(true)
    await result
  } catch { ElMessage.error(t('contextMenu.actionFailed')) }
}
async function position() {
  await nextTick()
  const state = menuState.value, element = panel.value
  if (!state || !element) return
  const viewport = window.visualViewport
  const point = menuPosition(state.x, state.y, element.offsetWidth, element.offsetHeight, viewport?.width ?? innerWidth, viewport?.height ?? innerHeight)
  left.value = point.left + (viewport?.offsetLeft ?? 0); top.value = point.top + (viewport?.offsetTop ?? 0)
  element.scrollTop = state.scroll
  if (!element.contains(document.activeElement)) buttons()[0]?.focus({ preventScroll: true })
}
watch(() => menuState.value?.id, () => { stack.value = [] })
watch([menuState, stack], position, { deep: true })
watch(() => [route.fullPath, auth.sessionID, auth.orgid, auth.sessionVersion, auth.user?.business_id, locale.value], () => closeMenu())
const targetObserver = new MutationObserver(() => {
  if (menuState.value?.target && !menuState.value.target.isConnected) closeMenu()
})
onMounted(() => {
  targetObserver.observe(document.body, { childList: true, subtree: true })
  document.addEventListener('contextmenu', context, true)
  document.addEventListener('pointerdown', outside, true)
  document.addEventListener('keydown', keydown, true)
  window.addEventListener('resize', position)
})
onBeforeUnmount(() => {
  targetObserver.disconnect()
  closeMenu(); document.removeEventListener('contextmenu', context, true)
  document.removeEventListener('pointerdown', outside, true); document.removeEventListener('keydown', keydown, true)
  window.removeEventListener('resize', position)
})
</script>

<template>
  <Teleport to="body">
    <div v-if="menuState" ref="panel" class="app-context-menu" role="menu" :aria-label="t('contextMenu.title')" :style="{ left: `${left}px`, top: `${top}px` }" @contextmenu.prevent @mousedown.prevent @scroll="menuState.scroll = ($event.target as HTMLElement).scrollTop">
      <button v-if="stack.length" role="menuitem" @click="stack.pop()"><el-icon><ArrowLeft /></el-icon>{{ t('contextMenu.back') }}</button>
      <button v-for="(entry, index) in entries" :key="index" role="menuitem" :disabled="entry.disabled" :aria-haspopup="entry.children ? 'menu' : undefined" :class="{ danger: entry.danger, active: entry.active }" @click="activate(entry)">
        <el-icon v-if="entry.active"><Check /></el-icon><span>{{ entry.label }}</span><el-icon v-if="entry.children"><ArrowRight /></el-icon>
      </button>
    </div>
  </Teleport>
</template>

<style scoped>
.app-context-menu { position: fixed; z-index: 5000; box-sizing: border-box; width: min(320px, calc(100vw - 24px)); max-height: calc(100dvh - 24px - env(safe-area-inset-top) - env(safe-area-inset-bottom)); overflow-y: auto; padding: 6px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface-overlay); box-shadow: var(--shadow); overscroll-behavior: contain; }
button { display: flex; align-items: center; gap: 8px; width: 100%; min-height: 38px; padding: 8px 12px; border: 0; border-radius: 5px; background: transparent; color: var(--ink); text-align: left; cursor: pointer; font: inherit; font-size: 14px; overflow-wrap: anywhere; }
button span { flex: 1; }
button:hover:not(:disabled), button:focus-visible { background: var(--surface-muted); outline: 2px solid var(--accent); outline-offset: -2px; }
button:disabled { color: var(--muted); cursor: default; }
button.danger { color: var(--el-color-danger); }
button.active { color: var(--accent); }
@media (pointer: coarse) { button { min-height: 44px; } }
</style>
