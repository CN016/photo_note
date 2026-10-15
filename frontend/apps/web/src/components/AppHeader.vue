<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElNotification } from 'element-plus'
import { useAuthStore } from '../stores/auth'
import { api } from '../api/client'
import LanguageSwitcher from './LanguageSwitcher.vue'
import ThemeSwitcher from './ThemeSwitcher.vue'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
const unreadCount = ref(0)
let refreshTimer: number | undefined
let notificationBaselineReady = false
let latestNotificationID = ''
let notificationBaselineUserID = ''

/** 轮询未读角标，并在已建立基线后仅对新到达的最新消息提示一次。 */
async function refreshNotifications() {
  if (!auth.user) return
  try {
    if (notificationBaselineUserID !== auth.user.business_id) {
      notificationBaselineReady = false
      latestNotificationID = ''
    }
    const [countResult, page] = await Promise.all([
      api.unreadNotificationCount(),
      api.notifications(1, 1, false),
    ])
    unreadCount.value = countResult.unread_count
    const latest = page.items[0]?.business_id ?? ''
    if (notificationBaselineReady && latest && latest !== latestNotificationID) {
      ElNotification({
        title: t('common.newMessage'),
        message: t('common.openMessageCenter'),
        type: 'info',
        duration: 5000,
        onClick: () => { void router.push('/messages') },
      })
    }
    latestNotificationID = latest
    notificationBaselineUserID = auth.user.business_id
    notificationBaselineReady = true
  } catch {
    // 轮询失败不影响当前页面；下一周期会继续请求，编辑内容仍由对应页面负责保存。
  }
}

onMounted(() => {
  void refreshNotifications()
  refreshTimer = window.setInterval(() => { void refreshNotifications() }, 30_000)
})

onBeforeUnmount(() => {
  if (refreshTimer !== undefined) window.clearInterval(refreshTimer)
})

/** 撤销服务端会话后返回登录页；失败时仍清除本机令牌并显示提示。 */
async function signOut() {
  let failed = false
  try { await auth.signOut() } catch { failed = true }
  await router.replace(failed ? { name: 'login', query: { logout: 'failed' } } : '/login')
}

function active(path: string): boolean {
  if (path === '/inspirations') return route.path === '/inspirations' || route.path.startsWith('/inspirations/')
  return route.path === path
}
</script>

<template>
  <aside class="app-navigation" :aria-label="t('common.navigation')">
    <RouterLink class="brand-lockup" to="/inspirations" :aria-label="t('common.brand')">
      <span class="brand-mark">n.</span>
      <span class="brand-copy"><strong>{{ t('common.brand') }}</strong><small>{{ t('common.product') }}</small></span>
    </RouterLink>
    <nav class="desktop-nav">
      <RouterLink class="nav-item" :class="{ active: active('/inspirations') }" to="/inspirations"><span aria-hidden="true">▦</span>{{ t('common.inspirations') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: route.path === '/inspirations/new' }" to="/inspirations/new"><span aria-hidden="true">＋</span>{{ t('common.create') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/ledger') }" to="/ledger"><span aria-hidden="true">◷</span>{{ t('common.ledger') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/schedule') }" to="/schedule"><span aria-hidden="true">▦</span>{{ t('common.schedule') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/messages') }" to="/messages"><span class="nav-icon-wrap"><span aria-hidden="true">◌</span><b v-if="unreadCount" class="nav-unread-count">{{ unreadCount > 99 ? '99+' : unreadCount }}</b></span>{{ t('common.messages') }}</RouterLink>
      <RouterLink class="nav-item" :class="{ active: active('/profile') }" to="/profile"><span aria-hidden="true">◉</span>{{ t('common.profile') }}</RouterLink>
    </nav>
    <div class="navigation-account">
      <div v-if="auth.user" class="nav-user"><el-avatar :size="34" :src="undefined">{{ auth.user.nickname.slice(0, 1) }}</el-avatar><span>{{ auth.user.nickname }}</span></div>
      <div class="navigation-preferences"><ThemeSwitcher /><LanguageSwitcher /></div>
      <el-button class="sign-out" text @click="signOut">{{ t('common.logout') }}</el-button>
    </div>
    <nav class="mobile-nav" :aria-label="t('common.navigation')">
      <RouterLink class="mobile-nav-item" :class="{ active: active('/inspirations') }" to="/inspirations"><span aria-hidden="true">▦</span><small>{{ t('common.inspirations') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: route.path === '/inspirations/new' }" to="/inspirations/new"><span aria-hidden="true">＋</span><small>{{ t('common.create') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: active('/ledger') }" to="/ledger"><span aria-hidden="true">◷</span><small>{{ t('common.ledger') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: active('/schedule') }" to="/schedule"><span aria-hidden="true">▦</span><small>{{ t('common.schedule') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: active('/messages') }" to="/messages"><span class="mobile-nav-icon-wrap"><span aria-hidden="true">◌</span><b v-if="unreadCount" class="mobile-unread-count">{{ unreadCount > 9 ? '9+' : unreadCount }}</b></span><small>{{ t('common.messages') }}</small></RouterLink>
      <RouterLink class="mobile-nav-item" :class="{ active: active('/profile') }" to="/profile"><span aria-hidden="true">◉</span><small>{{ t('common.profile') }}</small></RouterLink>
    </nav>
  </aside>
</template>
