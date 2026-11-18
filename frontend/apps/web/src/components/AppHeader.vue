<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElNotification } from 'element-plus'
import { Calendar, ChatDotRound, Clock, Files, House, List, Picture, PictureFilled, Setting, User, Menu, Close, Grid } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'
import { useAppShellStore } from '../stores/appShell'
import LanguageSwitcher from './LanguageSwitcher.vue'
import ThemeSwitcher from './ThemeSwitcher.vue'
import InspirationGoalReminder from './InspirationGoalReminder.vue'
import { navigationDrawer, setNavigationDrawer, drawerRelease, drawerListRoutes } from '../navigationDrawer'
import { ensureNotificationSession } from '../notificationSession'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
const appShell = useAppShellStore()
const avatarSrc = computed(() => {
  const user = auth.user
  return user && appShell.avatarBusinessID === user.business_id && appShell.avatarStoreKey === user.avatar_key ? appShell.avatarSrc : ''
})
const drawerPanel = ref<HTMLElement>()
const isDrawer = computed(() => navigationDrawer.progress > 0)
const drawerWidth = () => Math.min(320, window.innerWidth * 0.86)
let gesture: { x: number; y: number; base: number; claimed: boolean } | null = null
let suppressClick = false
let restoreFocus: HTMLElement | null = null
let oldOverflow = ''
/** 锁定方向后逐帧跟手；表单、可横滚容器、弹层和已缩放画布保留原手势。 */
function startDrawer(event: TouchEvent) {
  gesture = null
  if (event.touches.length !== 1 || (!isDrawer.value && !drawerListRoutes.has(String(route.name)))) return
  const target = event.target as HTMLElement
  if (target.closest('input,textarea,select,button,[role="slider"],[role="combobox"],.el-overlay,.el-popper,[data-content-zoom-ignore]')) return
  if (!isDrawer.value && document.querySelector('.el-overlay:not([style*="display: none"])')) return
  for (let node: HTMLElement | null = target; node && node !== document.body; node = node.parentElement) {
    if (Number(node.style.zoom || 1) > 1) return
    const style = getComputedStyle(node)
    if (node.scrollWidth > node.clientWidth + 2 && ['auto', 'scroll'].includes(style.overflowX)) return
  }
  const point = event.touches[0]!
  gesture = { x: point.clientX, y: point.clientY, base: navigationDrawer.progress, claimed: false }
}
function moveDrawer(event: TouchEvent) {
  if (!gesture) return
  if (event.touches.length !== 1) { cancelDrawer(); return }
  const dx = event.touches[0]!.clientX - gesture.x, dy = event.touches[0]!.clientY - gesture.y
  if (!gesture.claimed) {
    if (Math.abs(dy) > 10 && Math.abs(dy) >= Math.abs(dx)) { gesture = null; return }
    if (Math.abs(dx) < 12 || Math.abs(dx) < Math.abs(dy) * 1.5) return
    if (gesture.base === 0 && dx < 0) { gesture = null; return }
    gesture.claimed = true; navigationDrawer.dragging = true
  }
  event.preventDefault()
  navigationDrawer.progress = Math.max(0, Math.min(1, gesture.base + dx / drawerWidth()))
}
function endDrawer(event: TouchEvent) {
  if (!gesture) return
  const current = gesture; gesture = null
  if (!current.claimed) return
  event.preventDefault(); event.stopImmediatePropagation()
  suppressClick = true
  window.setTimeout(() => { suppressClick = false }, 400)
  setNavigationDrawer(drawerRelease(navigationDrawer.progress, current.base === 0))
}
function resizeDrawer() { gesture = null; setNavigationDrawer(false) }
function cancelDrawer() { if (gesture) setNavigationDrawer(gesture.base === 1); gesture = null }
function preventGestureClick(event: MouseEvent) { if (suppressClick) { event.preventDefault(); event.stopImmediatePropagation(); suppressClick = false } }
/** 抽屉支持Escape、Tab循环以及关闭后焦点恢复；语言下拉的独立弹层保留自身键盘处理。 */
function drawerKey(event: KeyboardEvent) {
  if (!isDrawer.value) return
  if (event.key === 'Escape') { event.preventDefault(); setNavigationDrawer(false) }
  if (event.key !== 'Tab' || (event.target as HTMLElement).closest('.el-popper')) return
  const nodes = [...(drawerPanel.value?.querySelectorAll<HTMLElement>('a[href],button:not([disabled]),input:not([disabled]),[tabindex="0"]') ?? [])].filter(node => node.getClientRects().length)
  const first = nodes[0], last = nodes[nodes.length - 1]
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}
watch(isDrawer, async open => {
  if (open) {
    restoreFocus = document.activeElement as HTMLElement; oldOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    await nextTick(); if (isDrawer.value) drawerPanel.value?.querySelector<HTMLButtonElement>('.drawer-close')?.focus()
  } else { document.body.style.overflow = oldOverflow; if (restoreFocus?.isConnected) restoreFocus.focus() }
})
watch(() => route.fullPath, () => { gesture = null; setNavigationDrawer(false) })
watch(() => [auth.sessionID, auth.orgid, auth.sessionVersion], () => { gesture = null; setNavigationDrawer(false) })
onMounted(() => {
  window.addEventListener('resize', resizeDrawer)
  document.addEventListener('touchstart', startDrawer, { capture: true, passive: true })
  document.addEventListener('touchmove', moveDrawer, { capture: true, passive: false })
  document.addEventListener('touchend', endDrawer, { capture: true, passive: false })
  document.addEventListener('touchcancel', cancelDrawer, true)
  document.addEventListener('click', preventGestureClick, true)
  document.addEventListener('keydown', drawerKey, true)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', resizeDrawer)
  document.removeEventListener('touchstart', startDrawer, true); document.removeEventListener('touchmove', moveDrawer, true)
  document.removeEventListener('touchend', endDrawer, true); document.removeEventListener('touchcancel', cancelDrawer, true)
  document.removeEventListener('click', preventGestureClick, true); document.removeEventListener('keydown', drawerKey, true)
  if (isDrawer.value) document.body.style.overflow = oldOverflow
  setNavigationDrawer(false)
})
let stopNotificationListener: (() => void) | null = null

/** 页面切换时复用 store 内的数据；只有账号首次出现或变化时才建立头像缓存和全局轮询。 */
watch(() => auth.user ? [auth.user.business_id, auth.user.avatar_key, auth.orgid, auth.sessionVersion] as const : null, identity => {
  stopNotificationListener?.()
  stopNotificationListener = null
  if (!identity) {
    appShell.reset()
    return
  }
  const [businessID, storekey] = identity
  void ensureNotificationSession().catch(() => { /* 通知初始化失败不影响登录、头像或业务页面。 */ })
  void appShell.ensureAvatar(businessID, storekey)
  stopNotificationListener = appShell.subscribeToNewNotifications(() => {
    ElNotification({
      title: t('common.newMessage'),
      message: t('common.openMessageCenter'),
      type: 'info',
      duration: 5000,
      onClick: () => { void router.push('/messages') },
    })
  })
  appShell.startNotificationPolling(businessID)
}, { immediate: true })

/** 重试仅发生于已登录状态；通知接口401不触发Web刷新，下一次正常Web刷新或回到前台再尝试。 */
const retryNoticeSession = () => { if (auth.user) void ensureNotificationSession().catch(() => {}) }
window.addEventListener('focus', retryNoticeSession)
window.addEventListener('photo-nini-session-updated', retryNoticeSession)
onBeforeUnmount(() => {
  stopNotificationListener?.()
  window.removeEventListener('focus', retryNoticeSession)
  window.removeEventListener('photo-nini-session-updated', retryNoticeSession)
})

/** 浏览器无法解码头像时同步切换所有共享位置的昵称首字母回退。 */
function fallbackAvatar() {
  if (auth.user) appShell.markAvatarUnavailable(auth.user.business_id, auth.user.avatar_key)
}

/** 撤销服务端会话后返回登录页；失败时仍清除本机令牌并显示提示。 */
async function signOut() {
  let failed = false
  try { await auth.signOut() } catch { failed = true }
  await router.replace(failed ? { name: 'login', query: { logout: 'failed' } } : '/login')
}

function active(path: string): boolean {
  if (path === '/inspirations') return route.path === '/inspirations' || route.path.startsWith('/inspirations/')
  if (path === '/portfolios') return route.path === '/portfolios' || route.path.startsWith('/portfolios/')
  if (path === '/projects') return route.path === '/projects' || route.path.startsWith('/projects/') || route.path.startsWith('/project-reviews')
  if (path === '/workbench') return route.path === '/workbench'
  return route.path === path
}

/** 详情页仍属于对应一级栏目；只有一级入口本身才标记为当前页面。 */
function ariaCurrent(path: string): 'page' | 'location' | undefined {
  if (!active(path)) return undefined
  if (path === '/workbench' && route.path !== '/workbench') return 'location'
  return 'page'
}

/** 底栏工作台入口承接计划、消息等次级模块，抽屉仍分别标记其独立入口。 */
function mobileActive(path: string): boolean {
  if (path === '/workbench') return ['/workbench', '/ledger', '/schedule', '/plans', '/messages', '/spaces'].includes(route.path)
  return active(path)
}

/** 次级页面使用栏目级无障碍状态，避免误报为底栏入口页面本身。 */
function ariaMobileCurrent(path: string): 'page' | 'location' | undefined {
  if (!mobileActive(path)) return undefined
  return route.path === path ? 'page' : 'location'
}
</script>

<template>
  <button v-if="route.name === 'delivery'" class="standalone-menu-trigger" type="button" :aria-label="t('common.openNavigation')" @click="setNavigationDrawer(true)"><el-icon><Menu /></el-icon></button>
  <div v-if="isDrawer" class="navigation-backdrop" :style="{ opacity: navigationDrawer.progress }" aria-hidden="true" @click="setNavigationDrawer(false)" />
  <aside ref="drawerPanel" class="app-navigation" :class="{ 'drawer-visible': isDrawer, 'drawer-dragging': navigationDrawer.dragging, 'standalone-navigation': route.meta.standalone }" :style="{ '--drawer-progress': navigationDrawer.progress }" :role="isDrawer ? 'dialog' : undefined" :aria-modal="isDrawer ? true : undefined" :aria-label="t('common.navigation')">
    <div class="drawer-account-heading"><RouterLink to="/profile" class="drawer-profile"><el-avatar :size="44" :src="avatarSrc || undefined" @error="fallbackAvatar">{{ auth.user?.nickname.slice(0, 1) }}</el-avatar><strong>{{ auth.user?.nickname }}</strong></RouterLink><button class="drawer-close" type="button" :aria-label="t('common.closeNavigation')" @click="setNavigationDrawer(false)"><el-icon><Close /></el-icon></button></div>
    <InspirationGoalReminder />

    <nav class="desktop-nav" :aria-label="t('common.navigation')">
      <p class="nav-group-title">{{ t('common.navigationGroups.create') }}</p>
      <RouterLink class="nav-item" :class="{ active: active('/workbench') }" :aria-current="ariaCurrent('/workbench')" to="/workbench"><el-icon aria-hidden="true"><House /></el-icon>{{ t('common.workbench') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/delivery') }" :aria-current="ariaCurrent('/delivery')" to="/delivery"><el-icon aria-hidden="true"><Files /></el-icon>{{ t('common.delivery') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/collage') }" :aria-current="ariaCurrent('/collage')" to="/collage"><el-icon aria-hidden="true"><Grid /></el-icon>{{ t('common.collage') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/inspirations') }" :aria-current="ariaCurrent('/inspirations')" to="/inspirations"><el-icon aria-hidden="true"><Picture /></el-icon>{{ t('common.inspirations') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/portfolios') }" :aria-current="ariaCurrent('/portfolios')" to="/portfolios"><el-icon aria-hidden="true"><PictureFilled /></el-icon>{{ t('common.portfolios') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/projects') }" :aria-current="ariaCurrent('/projects')" to="/projects"><el-icon aria-hidden="true"><Files /></el-icon>{{ t('common.projects') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/project-reviews') }" to="/project-reviews"><el-icon aria-hidden="true"><Files /></el-icon>{{ t('reviews.title') }}</RouterLink>
      <p class="nav-group-title">{{ t('common.navigationGroups.plan') }}</p>
      <RouterLink class="nav-item" :class="{ active: active('/ledger') }" :aria-current="ariaCurrent('/ledger')" to="/ledger"><el-icon aria-hidden="true"><Clock /></el-icon>{{ t('common.ledger') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/schedule') }" :aria-current="ariaCurrent('/schedule')" to="/schedule"><el-icon aria-hidden="true"><Calendar /></el-icon>{{ t('common.schedule') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/plans') }" :aria-current="ariaCurrent('/plans')" to="/plans"><el-icon aria-hidden="true"><List /></el-icon>{{ t('common.plans') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/messages') }" :aria-current="ariaCurrent('/messages')" to="/messages"><el-icon aria-hidden="true"><ChatDotRound /></el-icon>{{ t('common.messages') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/profile') }" :aria-current="ariaCurrent('/profile')" to="/profile"><el-icon aria-hidden="true"><User /></el-icon>{{ t('common.profile') }}</RouterLink>
      <template v-if="auth.user?.is_admin || auth.user?.organization_role === 'admin'">
        <p class="nav-group-title">{{ t('common.navigationGroups.manage') }}</p>
        <RouterLink v-if="auth.user?.is_admin" class="nav-item" :class="{ active: active('/atelier-7f3c9a2e') }" :aria-current="ariaCurrent('/atelier-7f3c9a2e')" to="/atelier-7f3c9a2e"><el-icon aria-hidden="true"><Setting /></el-icon>{{ t('common.console') }}</RouterLink>
        <RouterLink v-if="auth.user?.organization_role === 'admin'" class="nav-item" :class="{ active: active('/organization-console') }" :aria-current="ariaCurrent('/organization-console')" to="/organization-console"><el-icon aria-hidden="true"><Picture /></el-icon>{{ t('common.organizationConsole') }}</RouterLink>
        <RouterLink class="nav-item" :class="{ active: active('/organizations') }" :aria-current="ariaCurrent('/organizations')" to="/organizations"><el-icon aria-hidden="true"><User /></el-icon>{{ t('common.organizations') }}</RouterLink>
      </template>
    </nav>
    <div class="navigation-account">
      <RouterLink to="/spaces" class="nav-item">{{ t('common.spaces') }} · {{ auth.orgid === -1 ? t('spaces.personal') : auth.orgid }}</RouterLink>


      <el-button class="sign-out" text @click="signOut">{{ t('common.logout') }}</el-button>
    </div>

    <footer class="drawer-preferences"><LanguageSwitcher :teleported="false" /><ThemeSwitcher /></footer>
  </aside>
  <!-- 底栏独立于抽屉，避免继承抽屉的隐藏、位移和滚动状态。 -->
  <div v-if="!route.meta.standalone && !['edit-inspiration', 'organization-edit-inspiration', 'platform-edit-inspiration'].includes(String(route.name))" class="mobile-navigation-bar">
    <nav class="mobile-nav" :inert="isDrawer" :aria-label="t('common.mobileNavigation')">
      <RouterLink class="mobile-nav-item" :class="{ active: mobileActive('/workbench') }" :aria-current="ariaMobileCurrent('/workbench')" to="/workbench" :aria-label="t('common.workbench')"><el-icon aria-hidden="true"><House /></el-icon><small>{{ t('common.mobileWorkbench') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: mobileActive('/inspirations') }" :aria-current="ariaMobileCurrent('/inspirations')" to="/inspirations"><el-icon aria-hidden="true"><Picture /></el-icon><small>{{ t('common.inspirations') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: mobileActive('/portfolios') }" :aria-current="ariaMobileCurrent('/portfolios')" to="/portfolios"><el-icon aria-hidden="true"><PictureFilled /></el-icon><small>{{ t('common.portfolios') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: mobileActive('/projects') }" :aria-current="ariaMobileCurrent('/projects')" to="/projects"><el-icon aria-hidden="true"><Files /></el-icon><small>{{ t('common.mobileProjects') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: mobileActive('/profile') }" :aria-current="ariaMobileCurrent('/profile')" to="/profile"><el-icon aria-hidden="true"><User /></el-icon><small>{{ t('common.mobileProfile') }}</small></RouterLink>
    </nav>
  </div>
</template>

<style scoped>
.mobile-navigation-bar { display: none; }
.navigation-backdrop { position: fixed; inset: 0; z-index: 2000; background: rgb(0 0 0 / 35%); }
.app-navigation { padding: 18px 12px max(12px, env(safe-area-inset-bottom)); }
.drawer-account-heading { display: flex; flex: 0 0 auto; align-items: center; justify-content: space-between; gap: 8px; padding: 8px 4px 18px; }
.drawer-profile { display: flex; min-width: 0; align-items: center; gap: 12px; color: var(--ink); text-decoration: none; }
.drawer-profile strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.drawer-close { display: none; width: 44px; height: 44px; flex-shrink: 0; place-items: center; border: 0; border-radius: 50%; background: var(--surface-soft); color: var(--ink); font-size: 20px; cursor: pointer; }
.app-navigation > .brand-lockup { display: none; }
.desktop-nav { flex: 1 1 auto; margin-top: 0; }
.navigation-account { flex: 0 0 auto; margin-top: 8px; padding-top: 8px; }
.drawer-preferences { display: flex; flex: 0 0 auto; align-items: center; justify-content: space-between; gap: 12px; border-top: 1px solid var(--line); padding-top: 14px; background: var(--surface); }
.drawer-preferences :deep(.language-switcher) { width: 120px; }
.app-navigation.drawer-visible { z-index: 2001; inset: 0 auto 0 0; width: min(320px, 86vw); height: 100dvh; background: var(--surface); transform: translateX(calc((var(--drawer-progress) - 1) * 100%)); transition: transform 220ms ease; }
.drawer-visible .desktop-nav, .drawer-visible .navigation-account { display: grid; }
.drawer-visible .drawer-close { display: grid; }
.drawer-dragging { transition: none !important; }
.standalone-navigation { visibility: hidden; transform: translateX(-100%); }
.standalone-navigation.drawer-visible { visibility: visible; }
.standalone-menu-trigger { position: fixed; z-index: 41; top: 14px; left: 12px; width: 44px; height: 44px; display: grid; place-items: center; background: var(--surface); color: var(--ink); border: 1px solid var(--line); border-radius: 12px; font-size: 22px; }
@media (max-width: 768px) {
  .mobile-navigation-bar { display: block; position: fixed; z-index: 40; inset: auto 0 0; padding: 0 8px env(safe-area-inset-bottom); border-top: 1px solid var(--line); background: var(--surface); }
  :global(.project-editor-route) .mobile-navigation-bar { display: none; }
  .app-navigation { inset: 0 auto 0 0; width: min(320px, 86vw); height: 100dvh; border-top: 0; border-right: 1px solid var(--line); background: var(--surface); transform: translateX(-100%); visibility: hidden; }
  .app-navigation.drawer-visible { visibility: visible; }
  .app-navigation .desktop-nav { display: grid; }
  .app-navigation .navigation-account { display: grid; }
}
@media (prefers-reduced-motion: reduce) { .app-navigation.drawer-visible { transition: none; } }
</style>
