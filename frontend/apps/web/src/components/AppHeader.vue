<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElNotification } from 'element-plus'
import { Calendar, Clock, Files, House, List, Picture, Setting, User } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'
import { useAppShellStore } from '../stores/appShell'
import BrandMark from './BrandMark.vue'
import LanguageSwitcher from './LanguageSwitcher.vue'
import ThemeSwitcher from './ThemeSwitcher.vue'

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

onBeforeUnmount(() => { stopNotificationListener?.() })

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
  if (path === '/projects') return route.path === '/projects' || route.path.startsWith('/projects/')
  return route.path === path
}
</script>

<template>
  <aside class="app-navigation" :aria-label="t('common.navigation')">
    <RouterLink class="brand-lockup" to="/inspirations" :aria-label="t('common.brand')">
      <BrandMark />
      <span class="brand-copy"><strong>{{ t('common.brand') }}</strong><small>{{ t('common.product') }}</small></span>
    </RouterLink>
    <nav class="desktop-nav">
      <RouterLink class="nav-item" :class="{ active: active('/inspirations') }" to="/inspirations"><el-icon aria-hidden="true"><Picture /></el-icon>{{ t('common.inspirations') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/projects') }" to="/projects"><el-icon aria-hidden="true"><Files /></el-icon>{{ t('common.projects') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/ledger') }" to="/ledger"><el-icon aria-hidden="true"><Clock /></el-icon>{{ t('common.ledger') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/schedule') }" to="/schedule"><el-icon aria-hidden="true"><Calendar /></el-icon>{{ t('common.schedule') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/plans') }" to="/plans"><el-icon aria-hidden="true"><List /></el-icon>{{ t('common.plans') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/profile') }" to="/profile"><el-icon aria-hidden="true"><User /></el-icon>{{ t('common.profile') }}</RouterLink>
      <RouterLink v-if="auth.user?.is_admin" class="nav-item" :class="{ active: active('/atelier-7f3c9a2e') }" to="/atelier-7f3c9a2e"><el-icon aria-hidden="true"><Setting /></el-icon>{{ t('common.console') }}</RouterLink>
      <RouterLink v-if="auth.user?.organization_role === 'admin'" class="nav-item" to="/organization-console"><el-icon aria-hidden="true"><Picture /></el-icon>{{ t('common.organizationConsole') }}</RouterLink>
      <RouterLink v-if="auth.user?.is_admin || auth.user?.organization_role === 'admin'" class="nav-item" to="/organizations"><el-icon aria-hidden="true"><User /></el-icon>{{ t('common.organizations') }}</RouterLink>
      <RouterLink class="nav-item" to="/spaces"><el-icon aria-hidden="true"><House /></el-icon>{{ t('common.spaces') }}</RouterLink>
    </nav>
    <div class="navigation-account">
      <RouterLink to="/spaces" class="nav-item">{{ t('common.spaces') }} · {{ auth.orgid === -1 ? t('spaces.personal') : auth.orgid }}</RouterLink>
      <div v-if="auth.user" class="nav-user"><el-avatar :size="36" :src="avatarSrc || undefined" @error="fallbackAvatar">{{ auth.user.nickname.slice(0, 1) }}</el-avatar><span>{{ auth.user.nickname }}</span></div>
      <div class="navigation-preferences"><ThemeSwitcher /><LanguageSwitcher /></div>
      <el-button class="sign-out" text @click="signOut">{{ t('common.logout') }}</el-button>
    </div>
    <nav class="mobile-nav" :aria-label="t('common.navigation')">
      <RouterLink class="mobile-nav-item" :class="{ active: active('/workbench') }" to="/workbench" :aria-label="t('common.workbench')"><el-icon aria-hidden="true"><House /></el-icon><small>{{ t('common.mobileWorkbench') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: active('/inspirations') }" to="/inspirations"><el-icon aria-hidden="true"><Picture /></el-icon><small>{{ t('common.inspirations') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: active('/projects') }" to="/projects"><el-icon aria-hidden="true"><Files /></el-icon><small>{{ t('common.mobileProjects') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: active('/ledger') }" to="/ledger"><el-icon aria-hidden="true"><Clock /></el-icon><small>{{ t('common.mobileLedger') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: active('/profile') }" to="/profile"><el-icon aria-hidden="true"><User /></el-icon><small>{{ t('common.mobileProfile') }}</small></RouterLink>
    </nav>
  </aside>
</template>
