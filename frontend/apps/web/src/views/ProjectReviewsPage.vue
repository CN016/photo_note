<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, ArrowRight, Document, Search } from '@element-plus/icons-vue'
import AppHeader from '../components/AppHeader.vue'
import ContentZoom from '../components/ContentZoom.vue'
import { api } from '../api/client'
import type { ProjectReview } from '../api/types'

const { t, locale } = useI18n()
const records = ref<ProjectReview[]>([])
const query = ref('')
const page = ref(1)
const total = ref(0)
const loading = ref(true)
const failed = ref(false)
let generation = 0
let searchTimer: ReturnType<typeof setTimeout> | undefined
/** 列表只读取独立总结元数据；检索与分页由服务端在当前空间、用户范围内完成。 */
async function load() {
  const current = ++generation; loading.value = true; failed.value = false
  try {
    const result = await api.projectReviews(page.value, query.value)
    if (current !== generation) return
    records.value = result.items; total.value = result.total
  } catch { if (current === generation) failed.value = true }
  finally { if (current === generation) loading.value = false }
}
watch(query, () => { if (searchTimer) clearTimeout(searchTimer); searchTimer = setTimeout(() => { if (page.value !== 1) page.value = 1; else void load() }, 250) })
watch(page, () => { void load() })
onMounted(() => { void load() })
onBeforeUnmount(() => { generation++; if (searchTimer) clearTimeout(searchTimer) })
</script>

<template>
  <AppHeader />
  <ContentZoom><main class="reviews-main gallery-main">
    <header><h1>{{ t('reviews.title') }}</h1><RouterLink class="el-button" to="/projects">{{ t('reviews.plans') }}</RouterLink></header>
    <el-input v-model="query" class="review-search" clearable :prefix-icon="Search" :placeholder="t('reviews.search')" :aria-label="t('reviews.search')" />
    <p v-if="loading" class="review-state" role="status">{{ t('common.loading') }}</p>
    <div v-else-if="failed" class="review-state" role="alert"><p>{{ t('reviews.error') }}</p><el-button @click="load">{{ t('common.retry') }}</el-button></div>
    <div v-else-if="records.length" class="reviews-list"><RouterLink v-for="item in records" :key="item.business_id" :to="`/project-reviews/${item.business_id}`" class="review-row"><el-icon><Document /></el-icon><div><h2>{{ item.title }}</h2><time :datetime="item.updated_at">{{ new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'Asia/Shanghai' }).format(new Date(item.updated_at)) }}</time></div><el-icon><ArrowRight /></el-icon></RouterLink></div>
    <p v-else class="review-state">{{ t(query ? 'reviews.noResults' : 'reviews.empty') }}</p>
    <nav v-if="total > 20" class="review-pagination" :aria-label="t('reviews.pages')"><el-button :icon="ArrowLeft" :aria-label="t('reviews.previous')" :disabled="loading || page <= 1" @click="page--" /><span>{{ new Intl.NumberFormat(locale).format(page) }} / {{ new Intl.NumberFormat(locale).format(Math.ceil(total / 20)) }}</span><el-button :icon="ArrowRight" :aria-label="t('reviews.next')" :disabled="loading || page >= Math.ceil(total / 20)" @click="page++" /></nav>
  </main></ContentZoom>
</template>

<style scoped>
.reviews-main { padding-top: 48px; padding-bottom: 104px; min-height: 100svh; }
header { display: flex; justify-content: space-between; align-items: center; gap: 16px; flex-wrap: wrap; margin-bottom: 24px; }
h1 { font-weight: 500; margin: 0; font-size: clamp(28px, 4vw, 40px); }
.review-search { max-width: 520px; margin-bottom: 24px; }
.reviews-list { border-top: 1px solid var(--line); }
.review-row { display: flex; align-items: center; gap: 16px; padding: 20px 8px; color: var(--ink); border-bottom: 1px solid var(--line); text-decoration: none; transition: background-color .18s; }
.review-row:hover { background: var(--surface-muted); }
.review-row > div { flex: 1; min-width: 0; }
h2 { overflow-wrap: anywhere; margin: 0 0 8px; font-size: 17px; font-weight: 500; }
time { color: var(--muted); font-size: 13px; }
.review-state { display: grid; place-items: center; min-height: 240px; color: var(--muted); text-align: center; }
.review-pagination { display: flex; justify-content: center; align-items: center; gap: 16px; margin-top: 24px; font-variant-numeric: tabular-nums; }
.reviews-main :deep(.el-button) { min-height: 44px; }
@media (max-width: 768px) { .reviews-main { padding-top: 24px; } }
</style>
