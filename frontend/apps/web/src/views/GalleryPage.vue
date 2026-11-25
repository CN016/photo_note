<script setup lang="ts">
import { useGallerySectionSwipe } from '../useGallerySectionSwipe'
import { capturePreparedOrigin } from '../motion/navigationMotion'
import { prepareListImage } from '../motion/imageMotion'
import { inspirationDetails, inspirationDetailKey, preloadInspirationDetail, prepareDetailRecord } from '../images/detailImages'
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
import { ArrowDown, PictureFilled, Search, Sort } from '@element-plus/icons-vue'
import CreateFab from '../components/CreateFab.vue'
import InspirationThumbnail from '../components/InspirationThumbnail.vue'
import InspirationLibraryTools from '../components/InspirationLibraryTools.vue'
import GallerySearchPanel from '../components/GallerySearchPanel.vue'
import { useAuthStore } from '../stores/auth'
import type { InspirationLibraryFilters } from '../api/types'
import { api } from '../api/client'
import { ApiError, type InspirationRecord, type TagRecord } from '../api/types'
import { createGallerySearchState, gallerySearchHasValue, inspirationRequestOptions, type GallerySort, type GallerySearchState } from '../gallerySearch'

const { t, locale } = useI18n()
const router = useRouter()
const route = useRoute()
const active = ref(true)
let savedScroll = 0
let openedImageID = ''
// 离开前记录真实位置；激活发生在浏览器绘制之前，缩略图和筛选均沿用原实例。
onBeforeRouteLeave(to => { savedScroll = window.scrollY; openedImageID = to.name === 'edit-inspiration' ? String(to.params.id) : '' })
onDeactivated(() => { active.value = false; openVersion += 1; openingID.value = ''; gesture.cancel(); associationVersion += 1 })
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
const gridColumns = ref(2)
const gridZoom = ref(1)
const gridNavTop = ref(0)
const gridNavThumbTop = ref(0)
const gridNavVisible = ref(false)
const gridNavTrack = ref<HTMLElement>()
const gridNavThumb = ref<HTMLElement>()
const focusedRowStart = ref(-1)
const gridColumnSteps = [1, 2, 3, 4, 5, 10, 15, 20, 25, 30] as const
let gridNavFrame = 0
let gridFocusFrame = 0
let gridTouchDistance = 0
let gridTouchZoom = 1
let gridBaseColumns = 2
let gridNavDragging = false
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
    searchKind: gallerySearch.value.kind, searchColor: gallerySearch.value.color, tagSource: gallerySearch.value.tagSource,
    peopleMode: gallerySearch.value.peopleMode, peopleMin: gallerySearch.value.peopleMin, peopleMax: gallerySearch.value.peopleMax,
    filters: JSON.parse(JSON.stringify(libraryFilters.value)), items: [...items.value], page: page.value, total: total.value,
    scroll: window.scrollY, imageID: activeImage, menuPage, menuScroll: menuState.value?.scroll ?? 0,
    toolsScroll: document.querySelector('.activity-scroll')?.scrollLeft ?? 0,
    tools: libraryTools.value!.snapshot(), zoom: contentZoom.value!.snapshot() })
  void router.push({ path: `/projects/${projectID}`, query: { from: 'gallery', returnKey: key, mode: 'read' } })
}
function touchStart(event: TouchEvent) {
  galleryTouchStart(event)
  if (event.touches.length !== 1) { gesture.cancel(); return }
  touchedCard = (event.target as Element).closest<HTMLElement>('[data-inspiration-card]')
  const touch = event.touches[0]
  if (touchedCard && touch) gesture.start(touch.clientX, touch.clientY)
}
function touchMove(event: TouchEvent) {
  galleryTouchMove(event)
  const touch = event.touches[0]
  if (event.touches.length !== 1 || !touch) gesture.cancel()
  else gesture.move(touch.clientX, touch.clientY)
}
function interceptClick(event: MouseEvent) { if (gesture.consumeClick()) { event.preventDefault(); event.stopPropagation() } }

const openingID = ref('')
let openVersion = 0
let denseGridClickPending = false
/** 超过五列时先收敛到可点击尺寸，并将用户点中的同一张图滚到前排供下一次点击。 */
async function handleGalleryImageClick(event: MouseEvent, item: InspirationRecord) {
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
  if (gridColumns.value <= 5) { void openDetail(event, item); return }
  event.preventDefault()
  if (denseGridClickPending) return
  const link = event.currentTarget as HTMLElement
  const itemIndex = items.value.findIndex(record => record.business_id === item.business_id)
  if (itemIndex < 0) return
  // 细列下的点选只锚定附近图片组，不假设用户精确命中了真正想看的那张图。
  const rowStart = Math.floor(itemIndex / 5) * 5
  const firstRecord = items.value[rowStart]
  if (!firstRecord) return
  denseGridClickPending = true
  gridColumns.value = 5
  gridZoom.value = gridBaseColumns / 5
  focusedRowStart.value = rowStart
  window.clearTimeout(gridFocusFrame)
  gridFocusFrame = window.setTimeout(() => { focusedRowStart.value = -1 }, 1800)
  await nextTick()
  const rowCard = galleryRoot.value?.querySelector<HTMLElement>(`[data-inspiration-card="${firstRecord.business_id}"]`)
  if (rowCard?.isConnected) rowCard.scrollIntoView({ block: 'start', behavior: 'instant' as ScrollBehavior })
  denseGridClickPending = false
}
/** 点击只复用列表已发起的后台任务，不创建详情接口、下载或解码任务；未完成时等待同一 promise。 */
async function openDetail(event: MouseEvent, item: InspirationRecord) {
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
  event.preventDefault()
  if (openingID.value === item.business_id) return
  const marker = event.currentTarget as HTMLElement
  const version = ++openVersion, scope = scopeKey()
  openingID.value = item.business_id
  try {
    const pending = inspirationDetails.peek(inspirationDetailKey(item)) ?? preloadInspirationDetail(item)
    const prepared = await pending
    if (version !== openVersion || !active.value || !alive || scope !== scopeKey() || !marker.isConnected) return
    prepareListImage(marker, prepared.image)
    capturePreparedOrigin(marker)
    prepareDetailRecord(prepared.record)
    await router.push(`/inspirations/${item.business_id}`)
  } catch (cause) {
    if (version === openVersion && active.value && scope === scopeKey()) error.value = errorText(cause)
  } finally {
    if (version === openVersion) openingID.value = ''
  }
}
const items = ref<InspirationRecord[]>([])
const tags = ref<TagRecord[]>([])
const gallerySearch = ref<GallerySearchState>(createGallerySearchState())
const searchQuery = computed({ get: () => gallerySearch.value.query, set: value => { gallerySearch.value = { ...gallerySearch.value, query: value } } })
const selectedTagIDs = computed({ get: () => gallerySearch.value.tagIds, set: value => { gallerySearch.value = { ...gallerySearch.value, tagIds: value } } })
const tagMatch = computed({ get: () => gallerySearch.value.tagMatch, set: value => { gallerySearch.value = { ...gallerySearch.value, tagMatch: value } } })
const sort = ref<Exclude<GallerySort, 'color_match'>>('created_desc')
const page = ref(1)
const total = ref(0)
const pageSize = 20
const loading = ref(false)
const tagsLoading = ref(false)
const error = ref('')
const auth = useAuthStore()
const libraryFilters = ref<InspirationLibraryFilters>({})
const libraryRevision = ref(0)
const hasMore = computed(() => items.value.length < total.value)
const filtersActive = computed(() => gallerySearchHasValue(gallerySearch.value) || Object.values(libraryFilters.value).some(Boolean))
const hasAnyFilters = filtersActive
const sortLabel = computed(() => gallerySearch.value.kind === 'color'
  ? t('gallery.sortColorMatch')
  : t(sort.value === 'created_desc' ? 'gallery.sortNewest' : sort.value === 'created_asc' ? 'gallery.sortOldest' : 'gallery.sortEdited'))
let requestVersion = 0
let searchTimer: number | undefined
let tagsVersion = 0

/** 保留当前分页的详情任务，筛选移除的条目释放对象地址；高清图由缩略图接近视口时再预取。 */
watch(items, records => {
  inspirationDetails.retain(new Set(records.map(inspirationDetailKey)))
}, { deep: true, immediate: true })

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
    const result = await api.inspirations(nextPage, pageSize, inspirationRequestOptions(gallerySearch.value, sort.value, libraryFilters.value))
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
  tagsLoading.value = true
  try {
    const collected: TagRecord[] = []
    let nextPage = 1
    while (version === tagsVersion) {
      const result = await api.tags(nextPage, 100)
      collected.push(...result.items)
      if (!result.items.length || collected.length >= result.total) break
      nextPage += 1
    }
    if (version === tagsVersion) tags.value = Array.from(new Map(collected.map(tag => [tag.business_id, tag])).values())
  }
  catch { if(version === tagsVersion) tags.value = [] }
  finally { if(version === tagsVersion) tagsLoading.value = false }
}

/** 筛选条件变化后短暂合并连续输入，并从第一页重新查询。 */
watch([gallerySearch, sort, libraryFilters], () => {
  if (restoring || !active.value) return
  if (searchTimer !== undefined) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => { void loadPage(1) }, 220)
}, { deep: true })

/** 清空检索条件后恢复默认的收藏时间倒序。 */
function resetFilters() {
  libraryFilters.value = {}
  gallerySearch.value = createGallerySearchState()
  sort.value = 'created_desc'
}

/** 将双指距离吸附到有限列数档位，并短暂显示位置滑块反馈缩放手势。 */
function changeGridZoom(next: number) {
  const nextZoom = Math.max(.05, Math.min(4, next))
  gridZoom.value = nextZoom
  gridColumns.value = nearestGridColumnCount(gridBaseColumns / nextZoom)
  gridNavVisible.value = true
  window.clearTimeout(gridNavFrame);
  gridNavFrame = window.setTimeout(() => { gridNavVisible.value = false }, 1400)
}
/** 计算图库两指间距，用于保持相对缩放比例。 */
function distanceBetweenTouches(event: TouchEvent) {
  const a = event.touches[0], b = event.touches[1]
  return a && b ? Math.hypot(a.clientX - b.clientX, a.clientY - b.clientY) : 0
}
/** 只在图片网格内启动双指缩放，不干扰筛选控件和普通页面手势。 */
function galleryTouchStart(event: TouchEvent) {
  if (event.touches.length === 2) {
    const target = event.target as Element
    if (!target.closest('.inspiration-grid')) return
    gridTouchDistance = distanceBetweenTouches(event); gridTouchZoom = gridZoom.value
  }
}
/** 缩放时阻止浏览器页面缩放，并按比例更新卡片列数。 */
function galleryTouchMove(event: TouchEvent) {
  if (event.touches.length !== 2 || !gridTouchDistance || !(event.target as Element).closest('.inspiration-grid')) return
  event.preventDefault()
  changeGridZoom(gridTouchZoom * distanceBetweenTouches(event) / gridTouchDistance)
}
/** 所有手指离开后结束本次网格缩放。 */
function galleryTouchEnd(event: TouchEvent) { if (!event.touches.length) gridTouchDistance = 0 }
/** 依据页面相对视口的位置更新右侧快速定位滑块。 */
function updateGridNavigation() {
  const max = Math.max(1, document.documentElement.scrollHeight - window.innerHeight)
  setGridNavigationPosition(window.scrollY / max)
}
/** 屏幕旋转或窗口尺寸变化后，以当前平台默认列数重新计算网格。 */
function resizeGrid() { gridBaseColumns = window.innerWidth <= 768 ? 2 : 4; gridColumns.value = nearestGridColumnCount(gridBaseColumns / gridZoom.value) }
/** 列数只取产品规定档位；缩放比例变化时选择视觉密度最接近的一档。 */
function nearestGridColumnCount(desired: number) {
  return gridColumnSteps.reduce((best, candidate) => Math.abs(candidate - desired) < Math.abs(best - desired) ? candidate : best, gridColumnSteps[0])
}
/** 同步列表整体滚动比例和滑块像素位置，拖动与手动滚动使用同一位置计算。 */
function setGridNavigationPosition(ratio: number) {
  gridNavTop.value = Math.max(0, Math.min(1, ratio))
  const track = gridNavTrack.value
  if (track) gridNavThumbTop.value = gridNavTop.value * Math.max(0, track.clientHeight - (gridNavThumb.value?.clientHeight ?? 26))
}
/** 将右侧导航轨道上的点击或拖动位置映射为图库垂直滚动位置。 */
function seekGridPosition(event: PointerEvent) {
  const track = event.currentTarget as HTMLElement
  const bounds = track.getBoundingClientRect()
  const ratio = Math.max(0, Math.min(1, (event.clientY - bounds.top) / bounds.height))
  setGridNavigationPosition(ratio)
  seekGalleryEdge(ratio)
}
/** 跳转到图库内容范围内的相对位置。 */
function seekGalleryEdge(ratio: number) { window.scrollTo({ top: ratio * Math.max(0, document.documentElement.scrollHeight - window.innerHeight), behavior: 'instant' as ScrollBehavior }) }
function startGridNavigation(event: PointerEvent) {
  gridNavDragging = true; gridNavVisible.value = true
  window.clearTimeout(gridNavFrame);
  (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId)
  seekGridPosition(event)
}
function moveGridNavigation(event: PointerEvent) { if (gridNavDragging) seekGridPosition(event) }
function endGridNavigation() {
  if (!gridNavDragging) return
  gridNavDragging = false
  setGridNavigationPosition(gridNavTop.value)
  window.clearTimeout(gridNavFrame)
  gridNavFrame = window.setTimeout(() => { gridNavVisible.value = false }, 1400)
}

/** 色系搜索固定按距离排序，其余条件允许用户选择收藏或编辑时间。 */
function setSort(value: string) {
  if (value === 'created_desc' || value === 'created_asc' || value === 'updated_desc') sort.value = value
}

/** 重试只提交当前图片任务，等待后端确认后刷新列表，防止重复点击。 */

watch(() => [auth.orgid, auth.user?.business_id, auth.sessionVersion], () => {
  openVersion += 1; openingID.value = ''; clearGallerySnapshot(); associationVersion += 1; requestVersion += 1; items.value = []; total.value = 0; libraryFilters.value = {}; gallerySearch.value = createGallerySearchState(); tags.value = []
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
  resizeGrid()
  window.addEventListener('resize', resizeGrid, { passive: true })
  window.addEventListener('scroll', gesture.cancel, true)
  window.addEventListener('scroll', updateGridNavigation, { passive: true })
  updateGridNavigation()
  void loadTags()
  const saved = gallerySnapshot(scopeKey(), history.state.galleryReturnKey)
  if (!saved || saved.key !== history.state.galleryReturnKey || saved.position !== history.state.position) { void loadPage(); return }
  restoring = true
  gallerySearch.value = {
    ...createGallerySearchState(saved.searchKind || 'text'),
    query: saved.search || '',
    tagIds: [...saved.tags],
    tagMatch: saved.match,
    color: saved.searchColor || '',
    tagSource: saved.tagSource || 'all',
    peopleMode: saved.peopleMode || 'exact',
    peopleMin: saved.peopleMin ?? 1,
    peopleMax: saved.peopleMax ?? 1,
  }
  const { color: _legacyColor, people: _legacyPeople, people_mode: _legacyPeopleMode, people_min: _legacyPeopleMin, people_max: _legacyPeopleMax, ...persistentFilters } = saved.filters
  void _legacyColor; void _legacyPeople; void _legacyPeopleMode; void _legacyPeopleMin; void _legacyPeopleMax
  sort.value = saved.sort === 'color_match' ? 'created_desc' : saved.sort
  libraryFilters.value = persistentFilters; items.value = saved.items; page.value = saved.page; total.value = saved.total
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
onBeforeUnmount(() => { alive = false; inspirationDetails.clear(); openVersion += 1; disposeMenu?.(); gesture.cancel(); associationVersion += 1; window.removeEventListener('resize', resizeGrid); window.removeEventListener('scroll', gesture.cancel, true); window.removeEventListener('scroll', updateGridNavigation); window.clearTimeout(gridNavFrame); window.clearTimeout(gridFocusFrame); tagsVersion += 1; window.clearInterval(processingTimer); if (searchTimer !== undefined) window.clearTimeout(searchTimer); requestVersion += 1 })
</script>

<template>
  <div ref="galleryRoot" class="workspace-page gallery-workspace" @touchstart="touchStart" @touchmove="touchMove" @touchend="gesture.cancel(); galleryTouchEnd($event)" @touchcancel="gesture.cancel(); galleryTouchEnd($event)" @click.capture="interceptClick">
    <ContentZoom ref="contentZoom"><main id="main-content" class="gallery-main">
      <section class="gallery-intro">
        <div><h1>{{ t('gallery.title') }}</h1></div>
      </section>

      <nav class="gallery-subnav" :aria-label="t('gallery.sections')">
        <RouterLink class="gallery-subnav-link is-current" to="/inspirations" aria-current="page">{{ t('gallery.listSection') }}</RouterLink>
        <RouterLink class="gallery-subnav-link" to="/inspirations/statistics">{{ t('gallery.statisticsSection') }}</RouterLink>
      </nav>

      <GallerySearchPanel v-model="gallerySearch" :tags="tags" :tags-loading="tagsLoading" />

      <div v-if="items.length" class="gallery-meta">
        <div class="gallery-result-summary"><span>{{ filtersActive ? t('gallery.foundCount', { count: total }) : t('gallery.count', { count: total }) }}</span><button v-if="hasAnyFilters" type="button" @click="resetFilters">{{ t('gallery.clearFilters') }}</button></div>
        <el-dropdown trigger="click" :disabled="gallerySearch.kind === 'color'" @command="setSort">
          <button type="button" class="gallery-sort-button" :disabled="gallerySearch.kind === 'color'" :aria-label="t('gallery.sort')">
            <el-icon aria-hidden="true"><Sort /></el-icon>
            <span>{{ sortLabel }}</span>
            <el-icon class="sort-chevron" aria-hidden="true"><ArrowDown /></el-icon>
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="created_desc">{{ t('gallery.sortNewest') }}</el-dropdown-item>
              <el-dropdown-item command="created_asc">{{ t('gallery.sortOldest') }}</el-dropdown-item>
              <el-dropdown-item command="updated_desc">{{ t('gallery.sortEdited') }}</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>

      <div v-if="error" class="inline-error" role="alert"><span>{{ error }}</span><el-button text :disabled="loading" @click="loadPage(page)">{{ t('gallery.retry') }}</el-button></div>

      <TransitionGroup v-if="items.length" tag="section" name="gallery-grid" move-class="gallery-grid-move" class="inspiration-grid" :class="{ 'is-dense': gridColumns > 5 }" data-content-zoom-ignore :style="{ '--grid-columns': gridColumns }" :aria-label="t('gallery.title')">
        <article v-for="(item, index) in items" :key="item.business_id" class="inspiration-card" :class="{ 'is-zoom-focus': focusedRowStart >= 0 && index >= focusedRowStart && index < focusedRowStart + 5 }" tabindex="0" :data-inspiration-card="item.business_id" :style="{ '--card-index': index % 8 }">
          <RouterLink class="inspiration-link" @click.capture="handleGalleryImageClick($event, item)" :data-shared-transition="item.business_id" :to="`/inspirations/${item.business_id}`" :aria-label="t('gallery.openImage', { name: item.file_name || t('gallery.untitled') })">
            <div class="gallery-image-frame"><InspirationThumbnail :record="item" show-anchors detail-transition /></div>
            <p v-if="imageNote(item.data)" class="gallery-image-note" :title="imageNote(item.data)">{{ imageNote(item.data) }}</p>
          </RouterLink>
        </article>
      </TransitionGroup>

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

      <InspirationLibraryTools ref="libraryTools" v-model:filters="libraryFilters" :revision="libraryRevision" />
    </main></ContentZoom>
    <CreateFab class="gallery-create-fab" :label="t('gallery.newImage')" to="/inspirations/new" />
    <div v-if="items.length > 5" ref="gridNavTrack" class="gallery-side-nav" :class="{ visible: gridNavVisible, dragging: gridNavDragging }" role="slider" tabindex="0" :aria-label="t('gallery.listPosition')" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="Math.round(gridNavTop * 100)" @pointerdown="startGridNavigation" @pointermove="moveGridNavigation" @pointerup="endGridNavigation" @pointercancel="endGridNavigation" @lostpointercapture="endGridNavigation" @keydown.home.prevent="seekGalleryEdge(0)" @keydown.end.prevent="seekGalleryEdge(1)">
      <span ref="gridNavThumb" class="gallery-side-nav-thumb" :style="{ top: `${gridNavThumbTop}px` }" />
    </div>
    <footer class="workspace-footer"><span>{{ t('common.brand') }}</span><span>{{ t('common.footer') }}</span></footer>
  </div>
</template>
<style scoped>
/* 手机优先露出搜索与图片；桌面按可用区域增加列数，卡片本身不增加外框。 */
.gallery-main { padding-top: 14px; }
.gallery-workspace { --gallery-grid-zoom: 1; }
.gallery-create-fab :deep(.create-fab) { bottom: calc(86px + env(safe-area-inset-bottom)); }
.gallery-subnav { margin-top: 0; }
.gallery-meta { justify-content: space-between; margin: 10px 0 12px; }
.gallery-result-summary { display: flex; align-items: center; gap: 10px; }
.gallery-result-summary button { padding: 4px 0; border: 0; background: transparent; color: var(--accent); font: inherit; font-size: 12px; cursor: pointer; }
.gallery-sort-button { display: inline-flex; min-height: 38px; align-items: center; gap: 6px; padding: 0 8px; border: 0; border-radius: 999px; background: transparent; color: var(--muted); font: inherit; font-size: 12px; cursor: pointer; }
.gallery-sort-button:hover:not(:disabled), .gallery-sort-button:focus-visible { background: var(--surface-soft); color: var(--ink); outline: none; }
.gallery-sort-button:disabled { cursor: default; opacity: .72; }
.sort-chevron { font-size: 11px; }
.inspiration-grid { grid-template-columns: repeat(var(--grid-columns, 2), minmax(0, 1fr)); gap: 12px; touch-action: pan-y; }
.inspiration-grid.is-dense { gap: 0; }
.gallery-grid-move { transition: transform .24s cubic-bezier(.2,.75,.25,1); }
.gallery-side-nav { position: fixed; z-index: 35; top: 42%; right: 4px; width: 28px; height: 132px; border-radius: 999px; background: color-mix(in srgb, var(--ink) 22%, transparent); opacity: .25; touch-action: none; transition: width .16s ease, height .16s ease, opacity .18s; cursor: grab; }
.gallery-side-nav::before { position: absolute; top: 13px; right: 12px; bottom: 13px; left: 12px; border-radius: 999px; background: color-mix(in srgb, var(--ink) 18%, transparent); content: ''; }
.gallery-side-nav:hover, .gallery-side-nav:focus-visible, .gallery-side-nav.visible { opacity: .8; outline: none; }
.gallery-side-nav.dragging { width: 38px; height: 148px; opacity: 1; cursor: grabbing; }
.gallery-side-nav-thumb { position: absolute; left: 4px; width: 20px; height: 26px; border-radius: 999px; background: var(--surface-overlay); box-shadow: 0 1px 4px rgb(0 0 0 / 20%); transition: width .16s ease, height .16s ease, left .16s ease; pointer-events: none; }
.gallery-side-nav.dragging .gallery-side-nav-thumb { left: 5px; width: 28px; height: 40px; box-shadow: 0 3px 12px rgb(0 0 0 / 26%); }
.inspiration-card { position: relative; border-radius: var(--radius-card); scroll-margin-top: 78px; -webkit-touch-callout: none; }
.inspiration-card.is-zoom-focus :deep(.thumbnail-frame) { outline: 3px solid var(--accent); outline-offset: 3px; }
@media (prefers-reduced-motion: reduce) { .gallery-grid-move { transition: none; } }
.inspiration-card img { -webkit-user-drag: none; }
.inspiration-card:focus-visible { outline: none; }
.inspiration-card :deep(.thumbnail-frame) { aspect-ratio: 2 / 3; border: 0 !important; background: transparent !important; box-shadow: none !important; }
.inspiration-grid.is-dense .inspiration-card :deep(.thumbnail-frame), .inspiration-grid.is-dense .inspiration-card :deep(.thumbnail-image), .inspiration-grid.is-dense .gallery-image-frame { border-radius: 0 !important; }
.inspiration-card:hover :deep(.thumbnail-frame), .inspiration-card:focus-within :deep(.thumbnail-frame) { border: 0 !important; box-shadow: none !important; }
.inspiration-card:focus-visible :deep(.thumbnail-frame) { outline: 3px solid var(--focus); outline-offset: 3px; }
.gallery-image-frame { position: relative; border: 0 !important; border-radius: var(--radius-card); background: transparent !important; box-shadow: none !important; }
.inspiration-card:hover .gallery-image-frame, .inspiration-card:focus-within .gallery-image-frame { box-shadow: none !important; }
.gallery-image-note { overflow: hidden; margin: 0; padding: 7px 2px 2px; color: var(--ink); font-size: 13px; line-height: 1.5; white-space: nowrap; text-overflow: ellipsis; }
@media (max-width: 768px) {
  .gallery-main { width: calc(100% - 20px); padding: 8px 0 calc(112px + env(safe-area-inset-bottom)) !important; }
  .gallery-create-fab :deep(.create-fab) { bottom: calc(78px + env(safe-area-inset-bottom)); }
  .gallery-subnav { gap: 24px; padding-inline: 4px; }
  .gallery-meta { margin: 8px 0 10px; }
  .gallery-sort-button { min-height: 44px; padding-inline: 5px; }
  .inspiration-grid { gap: 8px; }
  .inspiration-grid.is-dense { gap: 0; }
  .gallery-side-nav { right: 2px; }
  .inspiration-card :deep(.thumbnail-frame) { border-radius: 14px; }
}
@media (min-width: 769px) {
  .inspiration-grid { grid-template-columns: repeat(var(--grid-columns, 3), minmax(0, 1fr)); gap: 16px; }
}
@media (min-width: 1600px) { .gallery-main { max-width: none; } }
</style>
