<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowRight, PictureFilled, Plus } from '@element-plus/icons-vue'
import AppHeader from '../components/AppHeader.vue'
import InspirationThumbnail from '../components/InspirationThumbnail.vue'
import { api } from '../api/client'
import { ApiError, type InspirationRecord } from '../api/types'

const { t, locale } = useI18n()
const items = ref<InspirationRecord[]>([])
const page = ref(1)
const total = ref(0)
const pageSize = 20
const loading = ref(false)
const error = ref('')
const hasMore = computed(() => items.value.length < total.value)

/** 将网络错误与列表读取错误映射为当前语言的页面提示。 */
function errorText(cause: unknown): string {
  if (cause instanceof ApiError && cause.code === 'network') return t('common.errors.network')
  return t('gallery.loadError')
}

/** 按后端分页读取列表；刷新首屏替换结果，继续翻页时追加结果。 */
async function loadPage(nextPage = 1) {
  loading.value = true
  error.value = ''
  try {
    const result = await api.inspirations(nextPage, pageSize)
    items.value = nextPage === 1 ? result.items : [...items.value, ...result.items]
    page.value = result.page
    total.value = result.total
  } catch (cause) { error.value = errorText(cause) }
  finally { loading.value = false }
}

/** 使用当前界面语言格式化灵感图创建日期。 */
function displayDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: 'short', day: 'numeric' }).format(new Date(value))
}

/** 将后端记录的编辑副本字节数格式化为当前语言下易读的单位。 */
function displayFileSize(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || !Number.isFinite(bytes) || bytes < 0) return ''
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unitIndex = 0
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex += 1
  }
  return `${new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }).format(value)} ${units[unitIndex]}`
}

onMounted(() => { void loadPage() })
</script>

<template>
  <div class="workspace-page">
    <AppHeader />
    <main class="gallery-main">
      <section class="gallery-intro">
        <div><p class="eyebrow">{{ t('common.product') }}</p><h1>{{ t('gallery.title') }}</h1><p class="gallery-subtitle">{{ t('gallery.subtitle') }}</p></div>
        <RouterLink custom to="/inspirations/new" v-slot="{ navigate }"><el-button class="primary-button add-button" type="primary" @click="navigate"><el-icon aria-hidden="true"><Plus /></el-icon>{{ t('gallery.newImage') }}</el-button></RouterLink>
      </section>

      <div v-if="items.length" class="gallery-meta"><span>{{ t('gallery.count', { count: total }) }}</span><span class="gallery-meta-line"></span></div>

      <div v-if="error" class="inline-error" role="alert"><span>{{ error }}</span><el-button text :disabled="loading" @click="loadPage(page)">{{ t('gallery.retry') }}</el-button></div>

      <section v-if="items.length" class="inspiration-grid" :aria-label="t('gallery.title')">
        <article v-for="(item, index) in items" :key="item.business_id" class="inspiration-card" :style="{ '--card-index': index % 8 }">
          <RouterLink class="inspiration-link" :to="`/inspirations/${item.business_id}`" :aria-label="t('gallery.openImage', { name: item.file_name || t('gallery.untitled') })">
            <InspirationThumbnail :record="item" />
            <div class="card-caption">
              <span class="card-title">{{ item.file_name || t('gallery.untitled') }}</span>
              <div class="card-caption-meta">
                <span class="card-date">{{ t('gallery.created', { date: displayDate(item.created_at) }) }}</span>
                <span v-if="item.file_size != null" class="card-size">{{ t('gallery.editCopySize', { size: displayFileSize(item.file_size) }) }}</span>
              </div>
            </div>
          </RouterLink>
        </article>
      </section>

      <section v-else-if="!loading && !error" class="gallery-empty">
        <div class="empty-art" aria-hidden="true"><el-icon><PictureFilled /></el-icon></div>
        <p class="eyebrow">{{ t('gallery.emptyTitle') }}</p><h2>{{ t('gallery.emptyText') }}</h2>
        <RouterLink custom to="/inspirations/new" v-slot="{ navigate }"><el-button @click="navigate">{{ t('gallery.uploadFirst') }}<el-icon aria-hidden="true"><ArrowRight /></el-icon></el-button></RouterLink>
      </section>

      <div v-if="loading && !items.length" class="gallery-loading" role="status"><span class="loading-dot"></span>{{ t('common.loading') }}</div>
      <div v-if="hasMore" class="load-more-wrap"><el-button :disabled="loading" @click="loadPage(page + 1)">{{ loading ? t('common.loading') : t('gallery.loadMore') }}</el-button></div>
    </main>
    <footer class="workspace-footer"><span>{{ t('common.brand') }}</span><span>{{ t('common.footer') }}</span></footer>
  </div>
</template>
