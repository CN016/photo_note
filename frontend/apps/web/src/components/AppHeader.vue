<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElNotification } from 'element-plus'
import { Calendar, ChatDotRound, Clock, Files, House, List, Picture, PictureFilled, Setting, User } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'
import { useAppShellStore } from '../stores/appShell'
import BrandMark from './BrandMark.vue'
import LanguageSwitcher from './LanguageSwitcher.vue'
import ThemeSwitcher from './ThemeSwitcher.vue'
import InspirationGoalReminder from './InspirationGoalReminder.vue'
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
  if (path === '/workbench') return ['/schedule', '/plans', '/messages', '/spaces'].includes(route.path)
  return route.path === path
}

/** 详情页仍属于对应一级栏目；只有一级入口本身才标记为当前页面。 */
function ariaCurrent(path: string): 'page' | 'location' | undefined {
  if (!active(path)) return undefined
  if (path === '/workbench' && route.path !== '/workbench') return 'location'
  return 'page'
}

/** 移动底栏把账本归入工作台区域，减少一级入口数量。 */
function mobileActive(path: string): boolean {
  if (path === '/workbench') return active('/workbench') || route.path === '/ledger'
  return active(path)
}

function ariaMobileCurrent(path: string): 'page' | 'location' | undefined {
  if (path === '/workbench' && route.path !== '/workbench' && mobileActive(path)) return 'location'
  return ariaCurrent(path)
}
</script>

<template>
  <aside class="app-navigation" :aria-label="t('common.navigation')">
    <InspirationGoalReminder />
    <RouterLink class="brand-lockup" to="/inspirations" :aria-label="t('common.brand')">
      <BrandMark />
      <span class="brand-copy"><strong>{{ t('common.brand') }}</strong><small>{{ t('common.product') }}</small></span>
    </RouterLink>
    <nav class="desktop-nav" :aria-label="t('common.desktopNavigation')">
      <p class="nav-group-title">{{ t('common.navigationGroups.create') }}</p>
      <RouterLink class="nav-item" :class="{ active: active('/workbench') }" :aria-current="ariaCurrent('/workbench')" to="/workbench"><el-icon aria-hidden="true"><House /></el-icon>{{ t('common.workbench') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/delivery') }" :aria-current="ariaCurrent('/delivery')" to="/delivery"><el-icon aria-hidden="true"><Files /></el-icon>{{ t('common.delivery') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/inspirations') }" :aria-current="ariaCurrent('/inspirations')" to="/inspirations"><el-icon aria-hidden="true"><Picture /></el-icon>{{ t('common.inspirations') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/portfolios') }" :aria-current="ariaCurrent('/portfolios')" to="/portfolios"><el-icon aria-hidden="true"><PictureFilled /></el-icon>{{ t('common.portfolios') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/projects') }" :aria-current="ariaCurrent('/projects')" to="/projects"><el-icon aria-hidden="true"><Files /></el-icon>{{ t('common.projects') }}</RouterLink>
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
      <div v-if="auth.user" class="nav-user"><el-avatar :size="36" :src="avatarSrc || undefined" @error="fallbackAvatar">{{ auth.user.nickname.slice(0, 1) }}</el-avatar><span>{{ auth.user.nickname }}</span></div>
      <div class="navigation-preferences"><ThemeSwitcher /><LanguageSwitcher /></div>
      <el-button class="sign-out" text @click="signOut">{{ t('common.logout') }}</el-button>
    </div>
    <nav v-if="!['edit-inspiration', 'organization-edit-inspiration', 'platform-edit-inspiration'].includes(String(route.name))" class="mobile-nav" :aria-label="t('common.mobileNavigation')">
      <RouterLink class="mobile-nav-item" :class="{ active: mobileActive('/workbench') }" :aria-current="ariaMobileCurrent('/workbench')" to="/workbench" :aria-label="t('common.workbench')"><el-icon aria-hidden="true"><House /></el-icon><small>{{ t('common.mobileWorkbench') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: mobileActive('/inspirations') }" :aria-current="ariaMobileCurrent('/inspirations')" to="/inspirations"><el-icon aria-hidden="true"><Picture /></el-icon><small>{{ t('common.inspirations') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: mobileActive('/portfolios') }" :aria-current="ariaMobileCurrent('/portfolios')" to="/portfolios"><el-icon aria-hidden="true"><PictureFilled /></el-icon><small>{{ t('common.portfolios') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: mobileActive('/projects') }" :aria-current="ariaMobileCurrent('/projects')" to="/projects"><el-icon aria-hidden="true"><Files /></el-icon><small>{{ t('common.mobileProjects') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: mobileActive('/profile') }" :aria-current="ariaMobileCurrent('/profile')" to="/profile"><el-icon aria-hidden="true"><User /></el-icon><small>{{ t('common.mobileProfile') }}</small></RouterLink>
    </nav>
  </aside>
</template>
