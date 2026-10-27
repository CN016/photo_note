<script setup lang="ts">
import ContentZoom from '../components/ContentZoom.vue'
import { computed, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowRight, Calendar, ChatDotRound, Clock, Files, List, Picture, Setting, User } from '@element-plus/icons-vue'
import AppHeader from '../components/AppHeader.vue'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const auth = useAuthStore()

/** 工作台集中展示应用的主要页面，管理入口仅对管理员账号显示。 */
const menuItems = computed(() => {
  const items: Array<{ key: string; to: string; icon: Component }> = [
    { key: 'inspirations', to: '/inspirations', icon: Picture },
    { key: 'projects', to: '/projects', icon: Files },
    { key: 'ledger', to: '/ledger', icon: Clock },
    { key: 'schedule', to: '/schedule', icon: Calendar },
    { key: 'plans', to: '/plans', icon: List },
    { key: 'messages', to: '/messages', icon: ChatDotRound },
    { key: 'profile', to: '/profile', icon: User },
  ]
  if (auth.user?.is_admin) items.push({ key: 'console', to: '/atelier-7f3c9a2e', icon: Setting })
  return items
})
</script>

<template>
  <div class="workspace-page workbench-page">
    <AppHeader />
    <ContentZoom><main class="workbench-main">
      <header class="workbench-heading">
        <p class="eyebrow">{{ t('workbench.eyebrow') }}</p>
        <h1>{{ t('workbench.title') }}</h1>
        <p>{{ t('workbench.subtitle') }}</p>
      </header>
      <section class="workbench-menu" :aria-label="t('workbench.allMenus')">
        <h2>{{ t('workbench.allMenus') }}</h2>
        <nav class="workbench-grid">
          <RouterLink v-for="item in menuItems" :key="item.key" class="workbench-card" :to="item.to">
            <span class="workbench-card-icon"><el-icon aria-hidden="true"><component :is="item.icon" /></el-icon></span>
            <span class="workbench-card-copy"><strong>{{ t(`common.${item.key}`) }}</strong><small>{{ t(`workbench.descriptions.${item.key}`) }}</small></span>
            <el-icon class="workbench-card-arrow" aria-hidden="true"><ArrowRight /></el-icon>
          </RouterLink>
        </nav>
      </section>
    </main></ContentZoom>
  </div>
</template>
