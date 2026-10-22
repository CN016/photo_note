<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowRight, PictureFilled, Plus, Search } from '@element-plus/icons-vue'
import AppHeader from '../components/AppHeader.vue'
import InspirationThumbnail from '../components/InspirationThumbnail.vue'
import { api } from '../api/client'
import { ApiError, type InspirationRecord, type TagRecord } from '../api/types'

const { t, locale } = useI18n()
const items = ref<InspirationRecord[]>([])
const tags = ref<TagRecord[]>([])
const searchQuery = ref('')
const selectedTagIDs = ref<string[]>([])
const tagMatch = ref<'any' | 'all'>('any')
const sort = ref<'created_desc' | 'created_asc' | 'updated_desc'>('created_desc')
const page = ref(1)
const total = ref(0)
const pageSize = 20
const loading = ref(false)
const error = ref('')
const hasMore = computed(() => items.value.length < total.value)
const filtersActive = computed(() => Boolean(searchQuery.value.trim() || selectedTagIDs.value.length))
let requestVersion = 0
let searchTimer: number | undefined

/** 将网络错误与列表读取错误映射为当前语言的页面提示。 */
function errorText(cause: unknown): string {
  if (cause instanceof ApiError && cause.code === 'network') return t('common.errors.network')
  return t('gallery.loadError')
}

/** 按后端分页读取列表；刷新首屏替换结果，继续翻页时追加结果。 */
async function loadPage(nextPage = 1) {
  const version = ++requestVersion
  loading.value = true
  error.value = ''
  try {
    const result = await api.inspirations(nextPage, pageSize, {
      q: searchQuery.value,
      tagIds: selectedTagIDs.value,
      tagMatch: tagMatch.value,
      sort: sort.value,
    })
    if (version !== requestVersion) return
    items.value = nextPage === 1 ? result.items : [...items.value, ...result.items]
    page.value = result.page
    total.value = result.total
  } catch (cause) { if (version === requestVersion) error.value = errorText(cause) }
  finally { if (version === requestVersion) loading.value = false }
}

/** 标签获取失败不影响图库浏览；筛选请求始终由后端分页执行。 */
async function loadTags() {
  try { tags.value = (await api.tags()).items }
  catch { tags.value = [] }
}

/** 筛选条件变化后短暂合并连续输入，并从第一页重新查询。 */
watch([searchQuery, selectedTagIDs, tagMatch, sort], () => {
  if (searchTimer !== undefined) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => { void loadPage(1) }, 220)
})

/** 清空检索条件后恢复默认的收藏时间倒序。 */
function resetFilters() {
  searchQuery.value = ''
  selectedTagIDs.value = []
  tagMatch.value = 'any'
  sort.value = 'created_desc'
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

onMounted(() => { void loadPage(); void loadTags() })
onBeforeUnmount(() => { if (searchTimer !== undefined) window.clearTimeout(searchTimer); requestVersion += 1 })
</script>

<template>
  <div class="workspace-page">
    <AppHeader />
    <main class="gallery-main">
      <section class="gallery-intro">
        <div><p class="eyebrow">{{ t('common.product') }}</p><h1>{{ t('gallery.title') }}</h1><p class="gallery-subtitle">{{ t('gallery.subtitle') }}</p></div>
        <RouterLink custom to="/inspirations/new" v-slot="{ navigate }"><el-button class="primary-button add-button" type="primary" :aria-label="t('gallery.newImage')" :title="t('gallery.newImage')" @click="navigate"><el-icon aria-hidden="true"><Plus /></el-icon>{{ t('gallery.newImage') }}</el-button></RouterLink>
      </section>

      <nav class="gallery-subnav" :aria-label="t('gallery.sections')">
        <RouterLink class="gallery-subnav-link is-current" to="/inspirations" aria-current="page">{{ t('gallery.listSection') }}</RouterLink>
        <RouterLink class="gallery-subnav-link" to="/inspirations/statistics">{{ t('gallery.statisticsSection') }}</RouterLink>
      </nav>

      <section class="gallery-tools" :aria-label="t('gallery.filters')">
        <el-input v-model="searchQuery" class="gallery-search" :aria-label="t('gallery.searchPlaceholder')" :placeholder="t('gallery.searchPlaceholder')" clearable>
          <template #prefix><el-icon aria-hidden="true"><Search /></el-icon></template>
        </el-input>
        <el-select v-model="selectedTagIDs" class="gallery-tag-select" multiple filterable collapse-tags collapse-tags-tooltip :placeholder="t('gallery.filterTags')" :max-collapse-tags="2">
          <el-option v-for="tag in tags" :key="tag.business_id" :label="tag.content" :value="tag.business_id" />
        </el-select>
        <el-select v-if="selectedTagIDs.length" v-model="tagMatch" class="gallery-match-select" :aria-label="t('gallery.tagMatch')">
          <el-option :label="t('gallery.matchAny')" value="any" />
          <el-option :label="t('gallery.matchAll')" value="all" />
        </el-select>
        <el-select v-model="sort" class="gallery-sort-select" :aria-label="t('gallery.sort')">
          <el-option :label="t('gallery.sortNewest')" value="created_desc" />
          <el-option :label="t('gallery.sortOldest')" value="created_asc" />
          <el-option :label="t('gallery.sortEdited')" value="updated_desc" />
        </el-select>
      </section>

      <div v-if="items.length" class="gallery-meta"><span>{{ t('gallery.count', { count: total }) }}</span><span class="gallery-meta-line"></span></div>

      <div v-if="error" class="inline-error" role="alert"><span>{{ error }}</span><el-button text :disabled="loading" @click="loadPage(page)">{{ t('gallery.retry') }}</el-button></div>

      <section v-if="items.length" class="inspiration-grid" :aria-label="t('gallery.title')">
        <article v-for="(item, index) in items" :key="item.business_id" class="inspiration-card" :style="{ '--card-index': index % 8 }">
          <RouterLink class="inspiration-link" :to="`/inspirations/${item.business_id}`" :aria-label="t('gallery.openImage', { name: item.file_name || t('gallery.untitled') })">
            <InspirationThumbnail :record="item" />
            <div class="card-caption">
              <span class="card-title">{{ item.file_name || t('gallery.untitled') }}</span>
              <div v-if="item.tag_names?.length" class="card-tags" :aria-label="t('gallery.tags')">
                <el-tag v-for="name in (item.tag_names ?? []).slice(0, 3)" :key="name" class="inspiration-card-tag" effect="plain" size="small">{{ name }}</el-tag>
                <el-tooltip
                  v-if="(item.tag_names?.length ?? 0) > 3"
                  :content="(item.tag_names ?? []).slice(3).join(' · ')"
                  placement="top"
                >
                  <el-tag
                    class="inspiration-card-tag inspiration-card-tag-overflow"
                    effect="plain"
                    size="small"
                    :aria-label="t('gallery.moreTagsTooltip', { tags: (item.tag_names ?? []).slice(3).join(' · ') })"
                  >{{ t('gallery.moreTags', { count: (item.tag_names?.length ?? 0) - 3 }) }}</el-tag>
                </el-tooltip>
              </div>
              <div class="card-caption-meta">
                <span class="card-date">{{ t('gallery.created', { date: displayDate(item.created_at) }) }}</span>
                <span v-if="item.file_size != null" class="card-size">{{ t('gallery.editCopySize', { size: displayFileSize(item.file_size) }) }}</span>
              </div>
            </div>
          </RouterLink>
        </article>
      </section>

      <section v-else-if="!loading && !error && filtersActive" class="gallery-empty gallery-filter-empty">
        <div class="empty-art" aria-hidden="true"><el-icon><Search /></el-icon></div>
        <p class="eyebrow">{{ t('gallery.noMatchesEyebrow') }}</p><h2>{{ t('gallery.noMatches') }}</h2>
        <el-button @click="resetFilters">{{ t('gallery.clearFilters') }}</el-button>
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
