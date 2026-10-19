<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChatDotRound } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'
import { useAppShellStore } from '../stores/appShell'

const { t } = useI18n()
const auth = useAuthStore()
const appShell = useAppShellStore()
const unreadCount = computed(() => appShell.unreadCount)
</script>

<template>
  <div v-if="auth.user" class="global-message-bar">
    <RouterLink class="global-message-link" to="/messages" :aria-label="t('common.messages')" :title="t('common.messages')">
      <el-icon aria-hidden="true"><ChatDotRound /></el-icon>
      <span v-if="unreadCount" class="global-message-count">{{ unreadCount > 99 ? '99+' : unreadCount }}</span>
    </RouterLink>
  </div>
</template>

<style scoped>
.global-message-bar { display: none; }
.global-message-link { position: relative; display: grid; width: 40px; height: 40px; place-items: center; border: 1px solid var(--line); border-radius: 50%; background: var(--surface); box-shadow: var(--soft-shadow); color: var(--ink-soft); font-size: 18px; text-decoration: none; }
.global-message-link:hover, .global-message-link:focus-visible { border-color: var(--accent); color: var(--accent-hover); }
.global-message-count { position: absolute; top: -5px; right: -6px; display: grid; min-width: 17px; height: 17px; place-items: center; padding: 0 4px; border: 2px solid var(--surface); border-radius: 99px; background: #c94f47; color: #fff; font-size: 9px; font-weight: 750; line-height: 1; }
@media (min-width: 769px) {
  .global-message-bar { position: fixed; z-index: 40; top: 0; right: 0; left: 226px; display: flex; height: 48px; align-items: center; justify-content: flex-end; padding: 0 16px; pointer-events: none; }
  .global-message-link { width: 36px; height: 36px; pointer-events: auto; }
}
@media (max-width: 768px) {
  .global-message-bar { position: relative; z-index: 40; display: flex; height: 52px; align-items: flex-start; justify-content: flex-end; padding: 6px 12px 0; }
  .global-message-link { width: 38px; height: 38px; }
}
</style>
