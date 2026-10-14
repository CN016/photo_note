<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import LanguageSwitcher from '../components/LanguageSwitcher.vue'
import ThemeSwitcher from '../components/ThemeSwitcher.vue'
import { backendHealthState, retryBackendAvailability } from '../backendHealth'

const { t, locale } = useI18n()
const checking = ref(false)
const lastChecked = computed(() => {
  const checkedAt = backendHealthState.checkedAt
  if (!checkedAt) return ''
  const formatted = new Intl.DateTimeFormat(locale.value, { hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(checkedAt)
  return t('maintenance.lastChecked', { time: formatted })
})

/** 手动探测服务；服务恢复后由健康检查模块返回之前访问的页面。 */
async function retry() {
  checking.value = true
  try { await retryBackendAvailability() }
  finally { checking.value = false }
}

</script>

<template>
  <main class="maintenance-page">
    <header class="maintenance-header">
      <RouterLink class="brand-lockup" to="/maintenance"><span class="brand-mark">n.</span><span><strong>{{ t('common.brand') }}</strong><small>{{ t('common.product') }}</small></span></RouterLink>
      <div class="header-actions"><ThemeSwitcher /><LanguageSwitcher /></div>
    </header>
    <section class="maintenance-content" aria-labelledby="maintenance-title">
      <div class="maintenance-art" aria-hidden="true"><span class="maintenance-orbit orbit-one"></span><span class="maintenance-orbit orbit-two"></span><span class="maintenance-seed">✳</span><span class="maintenance-card">{{ t('common.brand') }}</span></div>
      <p class="eyebrow">{{ t('maintenance.eyebrow') }}</p>
      <h1 id="maintenance-title">{{ t('maintenance.title') }}</h1>
      <p class="maintenance-message">{{ t('maintenance.message') }}</p>
      <div class="maintenance-status" role="status"><span class="status-indicator"></span>{{ t('maintenance.status') }}</div>
      <p v-if="backendHealthState.version" class="maintenance-version">{{ t('maintenance.lastKnownVersion', { version: backendHealthState.version }) }}</p>
      <button class="primary-button maintenance-retry" type="button" :disabled="checking" @click="retry">{{ checking ? t('maintenance.checking') : t('maintenance.retry') }}<span aria-hidden="true">↻</span></button>
      <p class="maintenance-checked">{{ lastChecked }}</p>
      <p class="maintenance-help">{{ t('maintenance.help') }}</p>
    </section>
  </main>
</template>
