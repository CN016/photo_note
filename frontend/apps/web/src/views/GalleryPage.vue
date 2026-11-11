<script setup lang="ts">
import { useGallerySectionSwipe } from '../useGallerySectionSwipe'
import { imageNote } from '../images/imageNote'
import ContentZoom from '../components/ContentZoom.vue'
import { computed, nextTick, onActivated, onDeactivated, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { registerMenu, openMenu, updateMenu, menuState, type MenuEntry } from '../context-menu/menu'
import { clipboardAdapter } from '../../../../packages/platform-adapters'
import { copyImage } from '../context-menu/clipboard'
import { createLongPress } from '../context-menu/longPress'
import { gallerySnapshot, saveGallerySnapshot, clearGallerySnapshot } from '../context-menu/galleryNavigation'
import type { InspirationProjectSummary } from '../api/types'
import { useI18n } from 'vue-i18n'
import { ArrowDown, Filter, PictureFilled, Search, User } from '@element-plus/icons-vue'
import CreateFab from '../components/CreateFab.vue'
import InspirationThumbnail from '../components/InspirationThumbnail.vue'
import InspirationLibraryTools from '../components/InspirationLibraryTools.vue'
import { useAuthStore } from '../stores/auth'
import type { InspirationLibraryFilters } from '../api/types'
import { api } from '../api/client'
import { ApiError, type InspirationRecord, type TagRecord } from '../api/types'
import { useCompactViewport } from '../useCompactViewport'

const { t, locale } = useI18n()
const router = useRouter()
const route = useRoute()
const showPeople = ref(false)
const active = ref(true)
let savedScroll = 0
let openedImageID = ''
// 离开前记录真实位置；激活发生在浏览器绘制之前，缩略图和筛选均沿用原实例。
onBeforeRouteLeave(to => { savedScroll = window.scrollY; openedImageID = to.name === 'edit-inspiration' ? String(to.params.id) : '' })
onDeactivated(() => { active.value = false; gesture.cancel(); associationVersion += 1 })
onActivated(() => {
  active.value = true
  window.scrollTo({ top: savedScroll, behavior: 'instant' as ScrollBehavior })
  // 详情可能保存了图片或标签，只更新对应记录；不清空列表、不重置分页或重新下载未变化的缩略图。
  if (openedImageID) {
    const id = openedImageID, scope = scopeKey()
    openedImageID = ''
    void api.inspiration(id).then(record => {
      if (!alive || !active.value || scope !== scopeKey()) return
      const index = items.value.findIndex(item => item.business_id === id)
      if (index >= 0) items.value[index] = record
    }).catch(() => { /* 单条刷新失败保留已显示内容，列表重试仍通过原入口进行。 */ })
  }
  const saved = gallerySnapshot(scopeKey(), history.state.galleryReturnKey)
  if (saved && saved.key === history.state.galleryReturnKey && saved.position === history.state.position) void nextTick(() => {
    if (!active.value || route.name !== 'gallery') return
    const card = galleryRoot.value?.querySelector<HTMLElement>(`[data-inspiration-card="${saved.imageID}"]`)
    const item = items.value.find(item => item.business_id === saved.imageID)
    if (card && item) showProjects(item, card, saved.x, saved.y, saved.menuPage, saved.menuScroll)
  })
})
const galleryRoot = ref<HTMLElement>()
useGallerySectionSwipe(galleryRoot, 'list')
const libraryTools = ref<InstanceType<typeof InspirationLibraryTools>>()
const contentZoom = ref<InstanceType<typeof ContentZoom>>()
const compact = useCompactViewport()
const moreFiltersOpen = ref(false)
const scopeKey = () => `${auth.sessionID}:${auth.orgid}:${auth.sessionVersion}:${auth.user?.business_id ?? ''}`
let restoring = false
let alive = true
let disposeMenu: (() => void) | undefined
let touchedCard: HTMLElement | null = null
let activeImage = ''
let menuPage = 1
let related: InspirationProjectSummary[] = []
let relatedTotal = 0
let associationVersion = 0
const gesture = createLongPress((x, y) => {
  const item = items.value.find(item => item.business_id === touchedCard?.dataset.inspirationCard)
  if (item && touchedCard) showProjects(item, touchedCard, x, y)
})
/** 关联浮窗按需加载，菜单代次和会话范围共同阻止迟到响应串图。 */
function showProjects(item: InspirationRecord, card: HTMLElement, x: number, y: number, pages = 1, scroll = 0) {
  activeImage = item.business_id; related = []; menuPage = 0; relatedTotal = 0
  const version = ++associationVersion, scope = scopeKey()
  const id = openMenu(x, y, [{ label: t('common.loading'), disabled: true }], card, scroll)
  const copy: MenuEntry = { label: t('contextMenu.copyImage'), disabled: !clipboardAdapter.canWriteImage(), run: () => copyImage(() => api.download(item.image_key)) }
  const render = (pending = false, failed = false) => {
    const entries: MenuEntry[] = [{ label: t('gallery.relatedProjects'), disabled: true }, ...related.map(project => ({ label: project.title || t('projects.editorTitlePlaceholder'), run: () => openRelated(project.business_id, x, y) }))]
    if (!related.length && !pending && !failed) entries.push({ label: t('gallery.noRelatedProjects'), disabled: true })
    if (pending) entries.push({ label: t('common.loading'), disabled: true })
    else if (failed) entries.push({ label: t('gallery.relatedProjectsError'), disabled: true }, { label: t('common.retry'), stayOpen: true, run: () => load(menuPage + 1) })
    else if (menuPage * 20 < relatedTotal) entries.push({ label: t('gallery.loadMore'), stayOpen: true, run: () => load(menuPage + 1) })
    entries.push(copy)
    updateMenu(id, entries, menuState.value?.scroll ?? scroll)
  }
  async function load(pageNumber: number): Promise<void> {
    render(true)
    try {
      const result = await api.inspirationProjects(item.business_id, pageNumber)
      if (version !== associationVersion || scope !== scopeKey() || menuState.value?.id !== id) return
      related = Array.from(new Map((pageNumber === 1 ? result.items : [...related, ...result.items]).map(project => [project.business_id, project])).values())
      relatedTotal = result.total; menuPage = result.page
      if (menuPage < pages && menuPage * 20 < relatedTotal) { await load(menuPage + 1); return }
      render()
      if (pages > 1 || scroll) { const state = menuState.value; if (state?.id === id) updateMenu(id, state.items, scroll) }
    } catch { if (version === associationVersion && scope === scopeKey()) render(false, true) }
  }
  void load(1)
}
/** 来源历史项与快照绑定，浏览器返回才恢复；普通重新进入图库不误用旧状态。 */
function openRelated(projectID: string, x: number, y: number) {
  const key = crypto.randomUUID()
  history.replaceState({ ...history.state, galleryReturnKey: key }, '')
  saveGallerySnapshot({ scope: scopeKey(), key, position: history.state.position, x, y,
    search: searchQuery.value, tags: [...selectedTagIDs.value], match: tagMatch.value, sort: sort.value,
    filters: JSON.parse(JSON.stringify(libraryFilters.value)), items: [...items.value], page: page.value, total: total.value,
    scroll: window.scrollY, imageID: activeImage, menuPage, menuScroll: menuState.value?.scroll ?? 0,
    toolsScroll: document.querySelector('.activity-scroll')?.scrollLeft ?? 0,
    tools: libraryTools.value!.snapshot(), zoom: contentZoom.value!.snapshot() })
  void router.push({ path: `/projects/${projectID}`, query: { from: 'gallery', returnKey: key, mode: 'read' } })
}
function touchStart(event: TouchEvent) {
  if (event.touches.length !== 1) { gesture.cancel(); return }
  touchedCard = (event.target as Element).closest<HTMLElement>('[data-inspiration-card]')
  const touch = event.touches[0]
  if (touchedCard && touch) gesture.start(touch.clientX, touch.clientY)
}
function touchMove(event: TouchEvent) {
  const touch = event.touches[0]
  if (event.touches.length !== 1 || !touch) gesture.cancel()
  else gesture.move(touch.clientX, touch.clientY)
}
function interceptClick(event: MouseEvent) { if (gesture.consumeClick()) { event.preventDefault(); event.stopPropagation() } }

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
const auth = useAuthStore()
const libraryFilters = ref<InspirationLibraryFilters>({})
const libraryRevision = ref(0)
const hasMore = computed(() => items.value.length < total.value)
const filtersActive = computed(() => Boolean(searchQuery.value.trim() || selectedTagIDs.value.length || Object.values(libraryFilters.value).some(Boolean)))
const advancedFilterCount = computed(() => selectedTagIDs.value.length + Object.values(libraryFilters.value).filter(Boolean).length + (sort.value === 'created_desc' ? 0 : 1))
let requestVersion = 0
let searchTimer: number | undefined
let tagsVersion = 0

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
      ...libraryFilters.value,
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
  const version = ++tagsVersion
  try { const result = await api.tags(); if(version === tagsVersion) tags.value = result.items }
  catch { if(version === tagsVersion) tags.value = [] }
}

/** 筛选条件变化后短暂合并连续输入，并从第一页重新查询。 */
watch([searchQuery, selectedTagIDs, tagMatch, sort, libraryFilters], () => {
  if (restoring || !active.value) return
  if (searchTimer !== undefined) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => { void loadPage(1) }, 220)
})

/** 清空检索条件后恢复默认的收藏时间倒序。 */
function resetFilters() {
  libraryFilters.value = {}
  searchQuery.value = ''
  selectedTagIDs.value = []
  tagMatch.value = 'any'
  sort.value = 'created_desc'
}

/** 重试只提交当前图片任务，等待后端确认后刷新列表，防止重复点击。 */

watch(() => [auth.orgid, auth.user?.business_id, auth.sessionVersion], () => {
  clearGallerySnapshot(); associationVersion += 1; requestVersion += 1; items.value = []; total.value = 0; libraryFilters.value = {}; selectedTagIDs.value = []; tags.value = []
  if (active.value && route.name === 'gallery') { void loadPage(); void loadTags() }
})

/** 只在当前页有未完成任务且页面可见时刷新，异步结果不依赖用户手动切换筛选。 */
const processingTimer = window.setInterval(() => { if (active.value && document.visibilityState === 'visible' && page.value === 1 && !loading.value && items.value.some(item => ['pending_compression','compressing','pending_analysis','analyzing'].includes(item.analysis_status || ''))) void loadPage() }, 30_000)
onMounted(async () => {
  if (galleryRoot.value) disposeMenu = registerMenu(galleryRoot.value, event => {
    const card = (event.target as Element).closest<HTMLElement>('[data-inspiration-card]')
    const item = items.value.find(item => item.business_id === card?.dataset.inspirationCard)
    if (!item || !card) return false
    gesture.cancel(); showProjects(item, card, event.clientX, event.clientY); return true
  })
  window.addEventListener('scroll', gesture.cancel, true)
  void loadTags()
  const saved = gallerySnapshot(scopeKey(), history.state.galleryReturnKey)
  if (!saved || saved.key !== history.state.galleryReturnKey || saved.position !== history.state.position) { void loadPage(); return }
  restoring = true
  searchQuery.value = saved.search; selectedTagIDs.value = saved.tags; tagMatch.value = saved.match
  sort.value = saved.sort; libraryFilters.value = saved.filters; items.value = saved.items; page.value = saved.page; total.value = saved.total
  await libraryTools.value?.restore(saved.tools)
  if (!alive) return
  await nextTick(); restoring = false
  contentZoom.value?.restore(saved.zoom)
  const activityScroll = document.querySelector('.activity-scroll')
  if (activityScroll) activityScroll.scrollLeft = saved.toolsScroll
  // 隐藏标签页会暂停 requestAnimationFrame；nextTick 后强制布局即可，不等待动画帧。
  void galleryRoot.value?.offsetHeight
  window.scrollTo(0, saved.scroll)
  const card = galleryRoot.value?.querySelector<HTMLElement>(`[data-inspiration-card="${saved.imageID}"]`)
  const item = items.value.find(item => item.business_id === saved.imageID)
  if (item && card) showProjects(item, card, saved.x, saved.y, saved.menuPage, saved.menuScroll)
})
onBeforeUnmount(() => { alive = false; disposeMenu?.(); gesture.cancel(); associationVersion += 1; window.removeEventListener('scroll', gesture.cancel, true); tagsVersion += 1; window.clearInterval(processingTimer); if (searchTimer !== undefined) window.clearTimeout(searchTimer); requestVersion += 1 })
</script>

<template>
  <div ref="galleryRoot" class="workspace-page" @touchstart.passive="touchStart" @touchmove.passive="touchMove" @touchend="gesture.cancel" @touchcancel="gesture.cancel" @click.capture="interceptClick">
    <ContentZoom ref="contentZoom"><main id="main-content" class="gallery-main">
      <section class="gallery-intro">
        <div><h1>{{ t('gallery.title') }}</h1></div>
      </section>

      <nav class="gallery-subnav" :aria-label="t('gallery.sections')">
        <RouterLink class="gallery-subnav-link is-current" to="/inspirations" aria-current="page">{{ t('gallery.listSection') }}</RouterLink>
        <RouterLink class="gallery-subnav-link" to="/inspirations/statistics">{{ t('gallery.statisticsSection') }}</RouterLink>
      </nav>

      <section class="gallery-tools" :aria-label="t('gallery.filters')">
        <el-input v-model="searchQuery" class="gallery-search" :aria-label="t('gallery.searchPlaceholder')" :placeholder="t('gallery.searchPlaceholder')" clearable>
          <template #prefix><el-icon aria-hidden="true"><Search /></el-icon></template>
        </el-input>
        <button v-if="compact" type="button" class="gallery-filter-toggle" :aria-expanded="moreFiltersOpen" aria-controls="gallery-advanced-filters" @click="moreFiltersOpen = !moreFiltersOpen">
          <el-icon aria-hidden="true"><Filter /></el-icon>
          <span>{{ moreFiltersOpen ? t('gallery.hideFilters') : t('gallery.moreFilters') }}</span>
          <b v-if="advancedFilterCount">{{ advancedFilterCount }}</b>
          <el-icon class="filter-chevron" :class="{ expanded: moreFiltersOpen }" aria-hidden="true"><ArrowDown /></el-icon>
        </button>
        <div v-if="!compact || moreFiltersOpen" id="gallery-advanced-filters" class="gallery-advanced-filters">
          <el-select v-model="selectedTagIDs" class="gallery-tag-select" multiple filterable collapse-tags collapse-tags-tooltip :placeholder="t('gallery.filterTags')" :aria-label="t('gallery.filterTags')" :max-collapse-tags="2">
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
        </div>
      </section>

      <div v-if="items.length" class="gallery-meta"><span>{{ t('gallery.count', { count: total }) }}</span><button type="button" class="gallery-people-toggle" :class="{ 'is-active': showPeople }" :aria-pressed="showPeople" :aria-label="t('gallery.togglePeople')" :title="t('gallery.togglePeople')" @click="showPeople = !showPeople"><el-icon aria-hidden="true"><User /></el-icon></button></div>

      <div v-if="error" class="inline-error" role="alert"><span>{{ error }}</span><el-button text :disabled="loading" @click="loadPage(page)">{{ t('gallery.retry') }}</el-button></div>

      <section v-if="items.length" class="inspiration-grid" :aria-label="t('gallery.title')">
        <article v-for="(item, index) in items" :key="item.business_id" class="inspiration-card" tabindex="0" :data-inspiration-card="item.business_id" :style="{ '--card-index': index % 8 }">
          <RouterLink class="inspiration-link" :data-shared-transition="item.business_id" :to="`/inspirations/${item.business_id}`" :aria-label="t('gallery.openImage', { name: item.file_name || t('gallery.untitled') })">
            <InspirationThumbnail :record="item" />
            <p v-if="imageNote(item.data)" class="gallery-image-note" :title="imageNote(item.data)">{{ imageNote(item.data) }}</p>

          </RouterLink>
          <div v-if="showPeople && item.analysis_status" class="gallery-analysis">
            <span v-if="item.analysis_status === 'succeeded'">{{ item.people_count === 0 ? t('gallery.library.emptyOnly') : t('gallery.library.personCount', { count: item.people_count }) }}<i v-if="item.dominant_color" :style="{ background: item.dominant_color }" :title="item.color_bucket ? t(`gallery.library.colorNames.${item.color_bucket}`) : ''" /></span>
            <span v-else role="status">{{ t(`gallery.library.states.${item.analysis_status}`) }}</span>
          </div>
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
      </section>

      <div v-if="loading && !items.length" class="gallery-loading" role="status"><span class="loading-dot"></span>{{ t('common.loading') }}</div>
      <div v-if="hasMore" class="load-more-wrap"><el-button :disabled="loading" @click="loadPage(page + 1)">{{ loading ? t('common.loading') : t('gallery.loadMore') }}</el-button></div>

      <InspirationLibraryTools ref="libraryTools" v-model:filters="libraryFilters" :revision="libraryRevision" :selected-tags="selectedTagIDs" @tag="id => selectedTagIDs = selectedTagIDs.includes(id) ? selectedTagIDs.filter(tag => tag !== id) : [...selectedTagIDs, id]" />
    </main></ContentZoom>
    <CreateFab :label="t('gallery.newImage')" to="/inspirations/new" />
    <footer class="workspace-footer"><span>{{ t('common.brand') }}</span><span>{{ t('common.footer') }}</span></footer>
  </div>
</template>
<style scoped>
.gallery-analysis{display:flex;justify-content:space-between;align-items:center;gap:8px;padding:0 12px 12px;color:var(--muted);font-size:12px}.gallery-analysis>span{display:flex;align-items:center;gap:8px}.gallery-analysis i{width:12px;height:12px;border-radius:50%;border:1px solid var(--line)}
</style>

<style scoped>
.inspiration-card { -webkit-touch-callout: none; }
.inspiration-card img { -webkit-user-drag: none; }
</style>

<style scoped>
/* 双列优先呈现图片，元信息保留在详情；人数是可选的图上角标，不改变卡片高度。 */
.gallery-main { padding-top: 20px; }
.gallery-subnav { margin-top: 0; }
.gallery-tools { display: flex; flex-wrap: wrap; padding: 0; margin-top: 12px; border: 0; box-shadow: none; background: transparent; }
.gallery-search { flex: 1; min-width: 100px; max-width: 360px; }
.gallery-advanced-filters { flex: 1; }
.gallery-meta { justify-content: space-between; margin: 8px 0; }
.gallery-people-toggle { display: grid; place-items: center; width: 36px; height: 36px; border: 1px solid var(--line); border-radius: 50%; background: var(--surface); color: var(--muted); cursor: pointer; }
.gallery-people-toggle.is-active { color: var(--accent); background: var(--accent-soft); border-color: var(--accent); }
.gallery-people-toggle:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.inspiration-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.inspiration-card { position: relative; }
.gallery-analysis { position: absolute; left: 8px; bottom: 8px; max-width: calc(100% - 16px); padding: 5px 8px; border-radius: 999px; background: var(--surface); color: var(--ink); pointer-events: none; }
@media (max-width: 768px) {
  .gallery-main { width: calc(100% - 20px); padding-top: 0; }
  .gallery-subnav { gap: 24px; padding-inline: 4px; }
  .gallery-search { max-width: none; }
  .gallery-filter-toggle { min-height: 38px; height: 38px; width: 40px; }
  .gallery-tools :deep(.el-input__wrapper) { min-height: 36px; }
  .gallery-advanced-filters { flex-basis: 100%; }
  .inspiration-grid { gap: 8px; }
  .inspiration-card :deep(.thumbnail-frame) { border-radius: 14px; }
}
</style>

<style scoped>
/* 手机双列；桌面按可用区域自动增加列数，保持卡片至少约 210px 宽。 */
@media (min-width: 769px) { .inspiration-grid { grid-template-columns: repeat(auto-fill, minmax(210px, 1fr)); gap: 16px; } }
@media (min-width: 1600px) { .gallery-main { max-width: none; } }
</style>

<style scoped>
.gallery-image-note { overflow: hidden; margin: 0; padding: 7px 2px 2px; color: var(--ink); font-size: 13px; line-height: 1.5; white-space: nowrap; text-overflow: ellipsis; }
.gallery-analysis { top: 8px; bottom: auto; }
</style>
