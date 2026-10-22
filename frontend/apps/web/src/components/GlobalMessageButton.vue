<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ChatDotRound } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'
import { useAppShellStore } from '../stores/appShell'
import BrandMark from './BrandMark.vue'
import ThemeSwitcher from './ThemeSwitcher.vue'
import LanguageSwitcher from './LanguageSwitcher.vue'

const { t } = useI18n()
const route = useRoute()
const auth = useAuthStore()
const appShell = useAppShellStore()
const unreadCount = computed(() => appShell.unreadCount)
</script>

<template>
  <div v-if="auth.user && !route.meta.public" class="global-message-bar">
    <RouterLink class="global-message-brand" to="/workbench" :aria-label="t('common.workbench')">
      <BrandMark size="small" /><strong>{{ t('common.brand') }}</strong>
    </RouterLink>
    <div class="global-message-actions">
      <div class="mobile-header-preferences"><ThemeSwitcher /><LanguageSwitcher /></div>
      <RouterLink class="global-message-link" to="/messages" :aria-label="t('common.messages')" :title="t('common.messages')">
        <el-icon aria-hidden="true"><ChatDotRound /></el-icon>
        <span v-if="unreadCount" class="global-message-count">{{ unreadCount > 99 ? '99+' : unreadCount }}</span>
      </RouterLink>
    </div>
  </div>
</template>

<style scoped>
.global-message-bar { display: none; }
.global-message-brand, .mobile-header-preferences { display: none; }
.global-message-actions { display: flex; flex: 0 0 auto; align-items: center; gap: 8px; }
.global-message-link { position: relative; display: grid; width: 40px; height: 40px; place-items: center; border: 1px solid var(--line); border-radius: 50%; background: var(--surface); box-shadow: var(--soft-shadow); color: var(--ink-soft); font-size: 18px; text-decoration: none; }
.global-message-link:hover, .global-message-link:focus-visible { border-color: var(--accent); color: var(--accent-hover); }
.global-message-count { position: absolute; top: -5px; right: -6px; display: grid; min-width: 17px; height: 17px; place-items: center; padding: 0 4px; border: 2px solid var(--surface); border-radius: 99px; background: var(--danger); color: var(--on-accent); font-size: 9px; font-weight: 750; line-height: 1; }
@media (min-width: 769px) {
  .global-message-bar { position: fixed; z-index: 40; top: 0; right: 0; left: 226px; display: flex; height: 48px; align-items: center; justify-content: flex-end; padding: 0 16px; pointer-events: none; }
  .global-message-link { width: 36px; height: 36px; pointer-events: auto; }
}
@media (max-width: 768px) {
  /* 侧栏偏好在手机上隐藏，顶栏提供可发现的主题、语言与消息入口，并保留返回工作台的品牌链接。 */
  .global-message-bar { position: sticky; z-index: 40; top: 0; display: flex; min-height: 60px; align-items: center; justify-content: space-between; gap: 10px; padding: 8px 12px; border-bottom: 1px solid var(--line); background: var(--surface); }
  .global-message-brand { display: flex; min-width: 0; flex: 1 1 auto; align-items: center; gap: 8px; color: var(--ink); text-decoration: none; }
  .global-message-brand strong { overflow: hidden; font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
  .mobile-header-preferences { display: flex; align-items: center; gap: 4px; }
  .mobile-header-preferences :deep(.language-switcher) { width: 94px; }
  .mobile-header-preferences :deep(.theme-switcher) { width: 44px; padding: 0; }
  .mobile-header-preferences :deep(.theme-switcher-text) { display: none; }
  .global-message-link { width: 44px; height: 44px; border-radius: var(--radius-control); box-shadow: none; }
}
</style>
