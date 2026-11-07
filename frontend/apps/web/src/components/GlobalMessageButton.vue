<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ChatDotRound, MoreFilled } from '@element-plus/icons-vue'
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
const mobileMenuOpen = ref(false)
</script>

<template>
  <header v-if="auth.user && !route.meta.public && route.name !== 'project-editor'" class="global-message-bar">
    <RouterLink class="global-message-brand" to="/workbench" :aria-label="t('common.workbench')">
      <BrandMark size="small" /><strong>{{ t('common.brand') }}</strong>
    </RouterLink>
    <div class="global-message-actions">
      <div class="global-desktop-actions">
        <RouterLink class="global-message-link" to="/messages" :aria-label="t('common.messages')" :title="t('common.messages')">
          <el-icon aria-hidden="true"><ChatDotRound /></el-icon>
          <span v-if="unreadCount" class="global-message-count">{{ unreadCount > 99 ? '99+' : unreadCount }}</span>
        </RouterLink>
      </div>
      <el-popover v-model:visible="mobileMenuOpen" placement="bottom-end" :width="252" trigger="click">
        <template #reference>
          <button type="button" class="global-mobile-menu-trigger" :aria-label="t('common.accountUtilities')" :title="t('common.accountUtilities')">
            <el-icon aria-hidden="true"><MoreFilled /></el-icon>
            <span v-if="unreadCount" class="global-mobile-unread-dot" aria-hidden="true" />
          </button>
        </template>
        <nav class="global-mobile-menu" :aria-label="t('common.accountUtilities')">
          <RouterLink to="/messages" class="global-mobile-menu-item" @click="mobileMenuOpen = false">
            <el-icon aria-hidden="true"><ChatDotRound /></el-icon><span>{{ t('common.messages') }}</span><b v-if="unreadCount">{{ unreadCount > 99 ? '99+' : unreadCount }}</b>
          </RouterLink>
          <div class="global-mobile-preferences"><ThemeSwitcher /><LanguageSwitcher /></div>
        </nav>
      </el-popover>
    </div>
  </header>
</template>

<style scoped>
.global-message-bar { display: none; }
.global-message-brand { display: none; }
.global-message-actions { display: flex; flex: 0 0 auto; align-items: center; gap: 8px; }
.global-desktop-actions { display: flex; align-items: center; gap: 8px; }
.global-mobile-menu-trigger { display: none; position: relative; width: 44px; height: 44px; place-items: center; padding: 0; border: 1px solid var(--line); border-radius: var(--radius-control); background: var(--surface); color: var(--ink-soft); cursor: pointer; font-size: 20px; }
.global-mobile-unread-dot { position: absolute; top: 8px; right: 8px; width: 7px; height: 7px; border: 2px solid var(--surface); border-radius: 50%; background: var(--danger); }
.global-mobile-menu { display: grid; gap: 4px; }
.global-mobile-menu-item { display: flex; min-height: 44px; align-items: center; gap: 10px; padding: 0 10px; border: 0; border-radius: var(--radius-control); background: transparent; color: var(--ink); cursor: pointer; font: inherit; font-size: 13px; text-align: left; text-decoration: none; }
.global-mobile-menu-item:hover { background: var(--surface-soft); }
.global-mobile-menu-item > span { flex: 1; }
.global-mobile-menu-item > b { display: grid; min-width: 20px; height: 20px; place-items: center; border-radius: 10px; background: var(--danger); color: var(--on-accent); font-size: 12px; }
.global-mobile-preferences { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 4px 6px 0; border-top: 1px solid var(--line); }
.global-mobile-preferences :deep(.language-switcher) { width: 108px; }
.global-message-link { position: relative; display: grid; width: 40px; height: 40px; place-items: center; padding: 0; border: 1px solid var(--line); border-radius: 50%; background: var(--surface); box-shadow: var(--soft-shadow); color: var(--ink-soft); cursor: pointer; font-size: 18px; text-decoration: none; }
.global-message-link:hover, .global-message-link:focus-visible { border-color: var(--accent); color: var(--accent-hover); }
.global-message-count { position: absolute; top: -5px; right: -6px; display: grid; min-width: 17px; height: 17px; place-items: center; padding: 0 4px; border: 2px solid var(--surface); border-radius: 99px; background: var(--danger); color: var(--on-accent); font-size: 12px; font-weight: 600; line-height: 1; }
@media (min-width: 769px) {
  .global-message-bar { position: fixed; z-index: 40; top: 0; right: 0; left: 226px; display: flex; height: 48px; align-items: center; justify-content: flex-end; padding: 0 16px; pointer-events: none; }
  .global-message-link { width: 36px; height: 36px; pointer-events: auto; }
}
@media (max-width: 768px) {
  /* 侧栏偏好在手机上隐藏，顶栏提供可发现的主题、语言与消息入口，并保留返回工作台的品牌链接。 */
  .global-message-bar { position: sticky; z-index: 40; top: 0; display: flex; min-height: 60px; align-items: center; justify-content: space-between; gap: 12px; padding: 8px 12px; border-bottom: 1px solid var(--line); background: var(--surface); }
  .global-message-brand { display: flex; min-width: 0; flex: 1 1 auto; align-items: center; gap: 8px; color: var(--ink); text-decoration: none; }
  .global-message-brand strong { overflow: hidden; font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
  .global-desktop-actions { display: none; }
  .global-mobile-menu-trigger { display: grid; }
  .global-message-link { width: 44px; height: 44px; border-radius: var(--radius-control); box-shadow: none; }
}
</style>
