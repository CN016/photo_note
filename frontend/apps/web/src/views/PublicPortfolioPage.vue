<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '../api/client'
import type { PortfolioPhoto, PortfolioRecord } from '../api/types'
import PortfolioGallery from '../components/PortfolioGallery.vue'
import LanguageSwitcher from '../components/LanguageSwitcher.vue'
const { t } = useI18n()
const route = useRoute()
const portfolio = ref<PortfolioRecord | null>(null)
const photos = ref<PortfolioPhoto[]>([])
const tags = ref<{ business_id:string; content:string }[]>([])
const loading = ref(true)
const error = ref(false)
onMounted(async () => {
  try { const data = await api.publicPortfolio(String(route.params.shareToken)); portfolio.value = data.portfolio; photos.value = data.photos; tags.value = data.tags }
  catch { error.value = true } finally { loading.value = false }
})
</script>
<template>
  <main class="portfolio-public">
    <header><span>{{ t('publicPortfolio.publicTitle') }}</span><LanguageSwitcher /></header>
    <p v-if="loading" role="status">{{ t('publicPortfolio.loading') }}</p>
    <p v-else-if="error" role="alert">{{ t('publicPortfolio.linkUnavailable') }}</p>
    <template v-else-if="portfolio"><h1>{{ portfolio.title }}</h1><p>{{ portfolio.description }}</p><div class="portfolio-tags" :aria-label="t('publicPortfolio.tags')"><el-tag v-for="tag in tags.filter(item => portfolio?.tag_ids.includes(item.business_id))" :key="tag.business_id" effect="plain">{{ tag.content }}</el-tag></div><PortfolioGallery :photos="photos" :tags="tags" namespace="publicPortfolio" /></template>
  </main>
</template>
<style scoped>
.portfolio-public { max-width:1200px; margin:auto; padding:24px; color:var(--el-text-color-primary); } .portfolio-tags { display:flex; flex-wrap:wrap; gap:8px; } header { display:flex; align-items:center; justify-content:space-between; gap:12px; } h1 { margin:32px 0 8px; } p { overflow-wrap:anywhere; white-space:pre-wrap; }
</style>
