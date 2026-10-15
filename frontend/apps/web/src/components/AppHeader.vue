<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../stores/auth'
import LanguageSwitcher from './LanguageSwitcher.vue'
import ThemeSwitcher from './ThemeSwitcher.vue'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

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
      <RouterLink class="mobile-nav-item" :class="{ active: active('/profile') }" to="/profile"><span aria-hidden="true">◉</span><small>{{ t('common.profile') }}</small></RouterLink>
    </nav>
  </aside>
</template>
