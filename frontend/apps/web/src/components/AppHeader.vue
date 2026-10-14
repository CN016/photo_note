<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../stores/auth'
import LanguageSwitcher from './LanguageSwitcher.vue'
import ThemeSwitcher from './ThemeSwitcher.vue'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()

/** 撤销服务端会话后返回登录页；失败时仍清除本机令牌并显示提示。 */
async function signOut() {
  let failed = false
  try { await auth.signOut() } catch { failed = true }
  await router.replace(failed ? { name: 'login', query: { logout: 'failed' } } : '/login')
}
</script>

<template>
  <header class="app-header">
    <RouterLink class="brand-lockup" to="/inspirations" :aria-label="t('common.brand')">
      <span class="brand-mark">n.</span>
      <span><strong>{{ t('common.brand') }}</strong><small>{{ t('common.product') }}</small></span>
    </RouterLink>
    <div class="header-actions">
      <span v-if="auth.user" class="user-greeting">{{ auth.user.nickname }}</span>
      <ThemeSwitcher />
      <LanguageSwitcher />
      <button class="quiet-button sign-out" type="button" @click="signOut">{{ t('common.logout') }}</button>
    </div>
  </header>
</template>
