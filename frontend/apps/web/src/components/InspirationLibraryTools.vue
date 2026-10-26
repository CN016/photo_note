<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, ArrowRight, Calendar, CollectionTag, Folder, Histogram, Picture, Setting, User } from '@element-plus/icons-vue'
import { api } from '../api/client'
import type { InspirationDay, InspirationFacet, InspirationGoalStatus, InspirationLibraryFilters } from '../api/types'
import { useAuthStore } from '../stores/auth'
import InspirationThumbnail from './InspirationThumbnail.vue'

const props = defineProps<{ filters: InspirationLibraryFilters; revision?: number; selectedTags?: string[] }>()
const emit = defineEmits<{ 'update:filters': [value: InspirationLibraryFilters]; tag: [id:string] }>()
const { t, locale } = useI18n()
const auth = useAuthStore()
/** 以北京时间确定入库日期，浏览器所在时区不会改变日期选择。 */
function chinaDay(): string {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(new Date())
  const part = (name: string) => parts.find(value => value.type === name)?.value || ''
  return `${part('year')}-${part('month')}-${part('day')}`
}
const today = ref(chinaDay())
const year = ref(Number(today.value.slice(0, 4)))
const month = ref(today.value.slice(0, 7))
const view = ref('calendar')
const views = [ ['calendar',Calendar], ['tags',CollectionTag], ['projects',Folder], ['people',User], ['empty',Picture], ['colors',Histogram] ] as const
const activity = ref<InspirationGoalStatus | null>(null)
const summary = ref<Record<string, InspirationFacet[]>>({})
const error = ref('')
const loading = ref(false)
const settings = ref(false)
const saving = ref(false)
const saveError = ref('')
const draft = ref({ enabled: false, target: 5, reminder_time: '20:00' })
let requestVersion = 0
let timer: number | undefined
const number = (value: number) => new Intl.NumberFormat(locale.value).format(value)
const activityByDay = computed(() => new Map((activity.value?.days || []).map(day => [day.day, day])))
const currentCount = computed(() => activity.value?.today.saved_count || 0)
const progress = computed(() => activity.value?.goal.enabled ? Math.min(100, currentCount.value / activity.value.goal.target * 100) : 0)
const selectedDate = computed(() => props.filters.date_from === props.filters.date_to ? props.filters.date_from : '')
const dates = computed(() => {
  const first = new Date(Date.UTC(year.value, 0, 1))
  const offset = (first.getUTCDay() + 6) % 7
  const length = (Date.UTC(year.value + 1, 0, 1) - first.getTime()) / 86400000
  return Array.from({ length: offset + length }, (_, index) => index < offset ? '' : new Date(first.getTime() + (index - offset) * 86400000).toISOString().slice(0, 10))
})
const monthRange = computed(() => {
  const [y, m] = month.value.split('-').map(Number)
  return { from: `${month.value}-01`, to: new Date(Date.UTC(y!, m!, 0)).toISOString().slice(0, 10) }
})
const calendar = computed(() => {
  const [y, m] = month.value.split('-').map(Number)
  const first = new Date(Date.UTC(y!, m! - 1, 1))
  const offset = (first.getUTCDay() + 6) % 7
  const days = Number(monthRange.value.to.slice(-2))
  return Array.from({ length: Math.ceil((offset + days) / 7) * 7 }, (_, i) => i < offset || i >= offset + days ? '' : `${month.value}-${String(i - offset + 1).padStart(2, '0')}`)
})
const weekdays = computed(() => Array.from({ length: 7 }, (_, i) => new Intl.DateTimeFormat(locale.value, { weekday: 'short', timeZone: 'UTC' }).format(new Date(Date.UTC(2026, 0, 5 + i)))))
const monthLabel = computed(() => new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: 'long', timeZone: 'UTC' }).format(new Date(`${month.value}-01T00:00:00Z`)))
const dayFacets = computed(() => new Map((summary.value.days || []).map(day => [day.key, day])))
const hueColors = Array.from({ length: 12 }, (_, i) => `hsl(${i * 30} 75% 48%)`)
const colorKeys = [...Array.from({ length: 12 }, (_, i) => `h${String(i).padStart(2, '0')}`), 'black', 'white', 'gray']
const wheelGradient = computed(() => `conic-gradient(from -15deg, ${hueColors.map((color, i) => `${color} ${i * 30}deg ${(i + 1) * 30}deg`).join(',')})`)
const colorName = (key: string) => t(`gallery.library.colorNames.${key}`)
const colorCount = (key: string) => summary.value.colors?.find(color => color.key === key)?.count || 0
const hasSelection = computed(() => Object.values(props.filters).some(Boolean))

/** 所有筛选都交由图库请求在数据库中执行，视图切换不清除用户明确选择的其他条件。 */
function filter(key: keyof InspirationLibraryFilters, value: string) {
  emit('update:filters', { ...props.filters, [key]: props.filters[key] === value ? undefined : value || undefined })
}
function selectDay(day: string) {
  if (!day) return
  const value = selectedDate.value === day ? undefined : day
  emit('update:filters', { ...props.filters, date_from: value, date_to: value })
  if (value) month.value = value.slice(0, 7)
}
function changeMonth(delta: number) {
  const [y, m] = month.value.split('-').map(Number)
  month.value = new Date(Date.UTC(y!, m! - 1 + delta, 1)).toISOString().slice(0, 7)
}
function dayTitle(day: string): string {
  const row: InspirationDay = activityByDay.value.get(day) || { day, saved_count: 0, target: null }
  const date = new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date(`${day}T00:00:00Z`))
  return t('gallery.library.dayTitle', { date, count: number(row.saved_count), goal: row.target ? t('gallery.library.dayGoal', { target: number(row.target) }) : t('gallery.library.goalDisabled') })
}
function level(day: string): number {
  const count = activityByDay.value.get(day)?.saved_count || 0
  return count === 0 ? 0 : count < 3 ? 1 : count < 6 ? 2 : count < 10 ? 3 : 4
}
/** 请求代次和空间身份共同淘汰迟到响应，旧空间数据不显示在新空间中。 */
async function load() {
  const version = ++requestVersion
  loading.value = true
  error.value = ''
  try {
    const [days, facets] = await Promise.all([api.inspirationActivity(year.value), api.inspirationBrowseSummary(monthRange.value.from, monthRange.value.to)])
    if (version !== requestVersion) return
    activity.value = days
    summary.value = facets
  } catch { if (version === requestVersion) error.value = t('gallery.library.loadError') }
  finally { if (version === requestVersion) loading.value = false }
}
function editGoal() {
  draft.value = activity.value ? { ...activity.value.goal, target: activity.value.goal.target || 5 } : { enabled: false, target: 5, reminder_time: '20:00' }
  saveError.value = ''
  settings.value = true
}
async function saveGoal() {
  const version = requestVersion
  saving.value = true
  saveError.value = ''
  try {
    await api.saveInspirationGoal(draft.value)
    if (version !== requestVersion) return
    settings.value = false
    await load()
  } catch { if (version === requestVersion) saveError.value = t('gallery.library.goalError') }
  finally { saving.value = false }
}
watch([year, month, () => props.revision], () => { void load() })
watch(() => [auth.orgid, auth.user?.business_id, auth.sessionVersion], () => {
  requestVersion += 1
  settings.value = false
  draft.value = { enabled: false, target: 5, reminder_time: '20:00' }
  activity.value = null
  summary.value = {}
  settings.value = false
  today.value = chinaDay()
  if (auth.user) void load()
}, { immediate: true })
/** 页面可见时刷新处理状态及跨日进度，不让每张图片分别轮询。 */
timer = window.setInterval(() => {
  if (document.visibilityState !== 'visible') return
  today.value = chinaDay()
  void load()
}, 30_000)
onBeforeUnmount(() => { requestVersion += 1; window.clearInterval(timer) })
</script>

<template>
  <section class="library-board" :aria-label="t('gallery.library.overview')" :aria-busy="loading">
    <div class="library-goal">
      <div class="goal-copy"><p>{{ t('gallery.library.overview') }}</p><h2>{{ activity?.goal.enabled ? t('gallery.library.progress', { count: number(currentCount), target: number(activity.goal.target) }) : t('gallery.library.goalOff') }}</h2><small>{{ t('gallery.library.goalHint') }}</small></div>
      <el-button :disabled="loading || !activity" @click="editGoal"><el-icon><Setting /></el-icon>{{ t('gallery.library.goalSettings') }}</el-button>
      <el-progress v-if="activity?.goal.enabled" class="goal-progress" :percentage="progress" :show-text="false" :aria-label="t('gallery.library.progress', { count: number(currentCount), target: number(activity.goal.target) })" />
    </div>
    <div v-if="error" class="library-error" role="alert">{{ error }}<el-button text @click="load">{{ t('gallery.library.retry') }}</el-button></div>
    <div class="activity-heading"><h3>{{ t('gallery.library.heatmap') }}</h3><el-input-number v-model="year" :min="1970" :max="9999" :aria-label="t('gallery.library.year')" controls-position="right" size="small" /></div>
    <div class="activity-scroll"><div class="activity-grid">
      <template v-for="(day, index) in dates" :key="day || `pad-${index}`"><button v-if="day" type="button" class="activity-cell" :class="[`heat-${level(day)}`, { selected: selectedDate === day, achieved: !!activityByDay.get(day)?.target && (activityByDay.get(day)?.saved_count || 0) >= (activityByDay.get(day)?.target || Infinity) }]" :title="dayTitle(day)" :aria-label="dayTitle(day)" :aria-pressed="selectedDate === day" @click="selectDay(day)" /><span v-else /></template>
    </div></div>
    <div class="heat-legend"><span>{{ t('gallery.library.less') }}</span><i v-for="n in 5" :key="n" :class="`heat-${n - 1}`" /><span>{{ t('gallery.library.more') }}</span><b />{{ t('gallery.library.achieved') }}</div>
    <nav class="library-tabs" :aria-label="t('gallery.library.view')"><button v-for="[key, icon] in views" :key="key" type="button" :class="{ active: view === key }" :aria-pressed="view === key" @click="view = key"><el-icon><component :is="icon" /></el-icon>{{ t(`gallery.library.${key}`) }}</button></nav>
    <div v-if="hasSelection" class="library-selection"><span>{{ t('gallery.library.selection') }}</span><el-tag v-if="selectedDate" closable @close="emit('update:filters', { ...filters, date_from: undefined, date_to: undefined })">{{ selectedDate }}</el-tag><el-tag v-if="filters.people" closable @close="filter('people', filters.people!)">{{ filters.people === '3+' ? t('gallery.library.peopleMany') : t('gallery.library.personCount', { count: filters.people }) }}</el-tag><el-tag v-if="filters.color" closable @close="filter('color', filters.color!)">{{ colorName(filters.color) }}</el-tag><el-tag v-if="filters.project" closable @close="filter('project', filters.project!)">{{ filters.project === 'unassigned' ? t('gallery.library.unassigned') : summary.projects?.find(p => p.key === filters.project)?.name || t('gallery.library.projects') }}</el-tag><el-tag v-if="filters.empty_shot" closable @close="filter('empty_shot', filters.empty_shot!)">{{ t(filters.empty_shot === 'true' ? 'gallery.library.emptyOnly' : 'gallery.library.withPeople') }}</el-tag><el-tag v-if="filters.status" closable @close="filter('status', filters.status!)">{{ t(`gallery.library.states.${filters.status}`) }}</el-tag><el-button text @click="emit('update:filters', {})">{{ t('gallery.library.clear') }}</el-button></div>
    <section v-if="view === 'calendar'" class="library-calendar" :aria-label="t('gallery.library.calendar')">
      <div class="calendar-heading"><el-button circle :aria-label="t('gallery.library.previousMonth')" @click="changeMonth(-1)"><el-icon><ArrowLeft /></el-icon></el-button><h3>{{ monthLabel }}</h3><el-button circle :aria-label="t('gallery.library.nextMonth')" @click="changeMonth(1)"><el-icon><ArrowRight /></el-icon></el-button><el-button class="all-dates" text @click="emit('update:filters', { ...filters, date_from: undefined, date_to: undefined })">{{ t('gallery.library.allDates') }}</el-button></div>
      <div class="calendar-week"><span v-for="day in weekdays" :key="day">{{ day }}</span></div>
      <div class="calendar-days"><template v-for="(day, index) in calendar" :key="day || `empty-${index}`"><button v-if="day" type="button" class="calendar-day" :class="{ selected: selectedDate === day, today: day === today }" :aria-label="`${day} ${t('gallery.library.images', { count: number(dayFacets.get(day)?.count || 0) })}`" :aria-pressed="selectedDate === day" @click="selectDay(day)"><span><strong>{{ Number(day.slice(-2)) }}</strong><small v-if="dayFacets.get(day)?.count">{{ number(dayFacets.get(day)!.count) }}</small></span><div class="calendar-previews"><InspirationThumbnail v-for="image in dayFacets.get(day)?.previews || []" :key="image.business_id" :record="image" /></div></button><span v-else class="calendar-padding" /></template></div>
    </section>
    <div v-else-if="view === 'tags'" class="facet-buttons"><el-button v-for="tag in summary.tags || []" :key="tag.key" :type="selectedTags?.includes(tag.key) ? 'primary' : 'default'" @click="emit('tag', tag.key)">{{ tag.name }}<span class="facet-count">{{ number(tag.count) }}</span></el-button></div>
    <div v-else-if="view === 'projects'" class="facet-buttons"><el-button :type="filters.project === 'unassigned' ? 'primary' : 'default'" @click="filter('project', 'unassigned')">{{ t('gallery.library.unassigned') }}</el-button><el-button v-for="project in summary.projects || []" :key="project.key" :type="filters.project === project.key ? 'primary' : 'default'" @click="filter('project', project.key)">{{ project.name }}<span class="facet-count">{{ number(project.count) }}</span></el-button><p v-if="!summary.projects?.length">{{ t('gallery.library.noProjects') }}</p></div>
    <div v-else-if="view === 'people'" class="facet-buttons"><el-button v-for="people in ['0', '1', '2', '3+']" :key="people" :type="filters.people === people ? 'primary' : 'default'" @click="filter('people', people)">{{ people === '3+' ? t('gallery.library.peopleMany') : t('gallery.library.personCount', { count: people }) }}<span class="facet-count">{{ number(summary.people?.find(p => p.key === people)?.count || 0) }}</span></el-button></div>
    <div v-else-if="view === 'empty'" class="empty-facet"><p>{{ t('gallery.library.emptyHint') }}</p><el-button :type="filters.empty_shot === 'true' ? 'primary' : 'default'" @click="filter('empty_shot', 'true')">{{ t('gallery.library.emptyOnly') }}</el-button><el-button :type="filters.empty_shot === 'false' ? 'primary' : 'default'" @click="filter('empty_shot', 'false')">{{ t('gallery.library.withPeople') }}</el-button></div>
    <section v-else-if="view === 'colors'" class="color-panel" :aria-label="t('gallery.library.colors')"><div class="color-wheel" :style="{ background: wheelGradient }"><button v-for="(_, i) in hueColors" :key="i" type="button" :style="{ transform: `rotate(${i * 30}deg) translateY(-77px) rotate(${-i * 30}deg)` }" :class="{ selected: filters.color === colorKeys[i] }" :aria-pressed="filters.color === colorKeys[i]" :aria-label="t('gallery.library.colorCount', { name: colorName(colorKeys[i]!), count: number(colorCount(colorKeys[i]!)) })" :title="colorName(colorKeys[i]!)" @click="filter('color', colorKeys[i]!)">{{ number(colorCount(colorKeys[i]!)) }}</button><div class="wheel-center">{{ filters.color ? colorName(filters.color) : t('gallery.library.colors') }}</div></div><div class="color-options"><p>{{ t('gallery.library.colorHint') }}</p><div class="facet-buttons"><el-button v-for="key in colorKeys" :key="key" :type="filters.color === key ? 'primary' : 'default'" @click="filter('color', key)"><i class="color-swatch" :style="{ background: key === 'black' ? '#111' : key === 'white' ? '#fff' : key === 'gray' ? '#888' : hueColors[Number(key.slice(1))] }" />{{ colorName(key) }}<span class="facet-count">{{ number(colorCount(key)) }}</span></el-button></div></div></section>
    <div class="status-facets"><el-button v-for="status in summary.statuses || []" :key="status.key" text :type="filters.status === status.key ? 'primary' : 'default'" @click="filter('status', status.key)">{{ t(`gallery.library.states.${status.key}`) }} · {{ number(status.count) }}</el-button></div>
    <el-dialog v-model="settings" :title="t('gallery.library.goalTitle')" width="min(440px, calc(100vw - 32px))" :close-on-click-modal="false"><el-form label-position="top" @submit.prevent="saveGoal"><el-form-item :label="t('gallery.library.goalEnabled')"><el-switch v-model="draft.enabled" /></el-form-item><el-form-item :label="t('gallery.library.target')"><el-input-number v-model="draft.target" :min="1" :max="100000" :precision="0" :disabled="!draft.enabled" /></el-form-item><el-form-item :label="t('gallery.library.reminderTime')"><el-time-select v-model="draft.reminder_time" start="00:00" end="23:55" step="00:05" :clearable="false" :disabled="!draft.enabled" /></el-form-item><p class="library-hint">{{ t('gallery.library.goalHint') }}</p><p v-if="saveError" class="library-error" role="alert">{{ saveError }}</p></el-form><template #footer><el-button :disabled="saving" @click="settings = false">{{ t('gallery.library.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="saveGoal">{{ t('gallery.library.save') }}</el-button></template></el-dialog>
  </section>
</template>

<style scoped>
.library-board{border:1px solid var(--line);border-radius:var(--radius-card);background:var(--surface);padding:22px;margin-bottom:22px;min-width:0}.library-goal{display:flex;align-items:center;gap:18px;flex-wrap:wrap}.goal-copy{flex:1;min-width:220px}.goal-copy p{margin:0 0 7px;color:var(--accent);font-size:12px;font-weight:650}.goal-copy h2{font-size:20px;line-height:1.5;margin:0;color:var(--ink)}.goal-copy small,.library-hint{display:block;color:var(--muted);line-height:1.7;font-size:12px;margin-top:5px}.goal-progress{width:100%}.activity-heading{display:flex;align-items:center;justify-content:space-between;margin:24px 0 10px;gap:12px}.activity-heading h3,.calendar-heading h3{margin:0;font-size:14px;color:var(--ink)}.activity-heading :deep(.el-input-number){width:105px}.activity-scroll{overflow-x:auto;padding:4px 2px 9px}.activity-grid{display:grid;grid-auto-flow:column;grid-template-rows:repeat(7,12px);grid-auto-columns:12px;gap:4px;width:max-content}.activity-cell{width:12px;height:12px;border:0;border-radius:3px;padding:0;cursor:pointer}.heat-0{background:var(--surface-muted)}.heat-1{background:color-mix(in srgb,var(--accent) 25%,var(--surface))}.heat-2{background:color-mix(in srgb,var(--accent) 45%,var(--surface))}.heat-3{background:color-mix(in srgb,var(--accent) 70%,var(--surface))}.heat-4{background:var(--accent)}.activity-cell.achieved{box-shadow:inset 0 0 0 1px var(--success)}.activity-cell.selected{outline:2px solid var(--ink);outline-offset:1px}.heat-legend{display:flex;align-items:center;justify-content:flex-end;gap:5px;font-size:11px;color:var(--muted);margin:3px 0 18px}.heat-legend i{width:10px;height:10px;border-radius:2px}.heat-legend b{width:10px;height:10px;border:1px solid var(--success);margin-left:9px;border-radius:2px}.library-tabs{display:flex;gap:6px;overflow:auto;border-top:1px solid var(--line);padding-top:18px}.library-tabs button{display:flex;align-items:center;justify-content:center;gap:7px;min-height:44px;padding:0 17px;border:1px solid transparent;border-radius:var(--radius-control);background:var(--surface-muted);color:var(--ink-soft);cursor:pointer;white-space:nowrap;font:inherit;font-size:13px}.library-tabs button.active{background:var(--accent-soft);color:var(--accent);border-color:color-mix(in srgb,var(--accent) 25%,var(--line))}.library-selection{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-top:13px;color:var(--muted);font-size:12px}.library-calendar{margin-top:20px}.calendar-heading{display:flex;gap:12px;align-items:center;margin-bottom:15px}.all-dates{margin-left:auto}.calendar-week,.calendar-days{display:grid;grid-template-columns:repeat(7,minmax(0,1fr));gap:6px}.calendar-week{padding-bottom:9px;text-align:center;color:var(--muted);font-size:11px}.calendar-day{min-height:86px;min-width:0;padding:8px;border:1px solid var(--line);border-radius:10px;background:var(--surface);color:var(--ink-soft);cursor:pointer;text-align:left}.calendar-day>span{display:flex;align-items:center;justify-content:space-between;gap:2px}.calendar-day strong{font-size:12px;font-weight:550}.calendar-day small{border-radius:8px;background:var(--accent-soft);color:var(--accent);padding:1px 5px;font-size:10px}.calendar-day.today{border-color:var(--accent)}.calendar-day.selected{background:var(--accent-soft);box-shadow:inset 0 0 0 1px var(--accent)}.calendar-previews{display:flex;gap:3px;overflow:hidden;margin-top:6px}.calendar-previews :deep(figure),.calendar-previews :deep(.inspiration-thumbnail){min-width:0;flex:1;aspect-ratio:1;border-radius:5px}.facet-buttons{display:flex;align-items:center;flex-wrap:wrap;gap:8px;margin-top:18px}.facet-buttons .el-button{margin:0;min-height:44px}.facet-count{font-size:11px;opacity:.75;margin-left:7px}.empty-facet p,.color-options p,.facet-buttons p{font-size:13px;line-height:1.7;color:var(--muted)}.empty-facet{margin-top:16px}.color-panel{display:flex;align-items:center;gap:30px;margin:25px 4px}.color-wheel{position:relative;flex:none;width:222px;height:222px;border-radius:50%;box-shadow:0 0 0 1px var(--line)}.color-wheel>button{position:absolute;left:calc(50% - 22px);top:calc(50% - 22px);width:44px;height:44px;border:0;border-radius:50%;background:transparent;color:#fff;font-size:11px;font-weight:750;text-shadow:0 1px 3px #0008;cursor:pointer}.color-wheel>button.selected{background:#0005;outline:2px solid #fff;outline-offset:-5px}.wheel-center{position:absolute;left:50%;top:50%;transform:translate(-50%,-50%);width:78px;height:78px;display:grid;place-items:center;background:var(--surface);border-radius:50%;font-size:12px;color:var(--ink);pointer-events:none}.color-options{min-width:0;flex:1}.color-options .facet-buttons{gap:6px;margin-top:10px}.color-swatch{width:13px;height:13px;border-radius:50%;border:1px solid var(--line);margin-right:6px}.status-facets{display:flex;align-items:center;flex-wrap:wrap;gap:3px;margin-top:14px}.status-facets .el-button{margin:0;font-size:11px}.library-error{font-size:13px;color:var(--danger);padding:8px 0}.library-board button:focus-visible{outline:2px solid var(--accent);outline-offset:3px}.calendar-previews :deep(img){height:44px;width:100%;object-fit:cover}
@media(max-width:700px){.library-board{padding:15px;margin-bottom:16px}.goal-copy{min-width:100%}.goal-copy h2{font-size:17px}.library-goal{gap:10px}.goal-copy small{font-size:11px}.activity-grid{grid-template-rows:repeat(7,10px);grid-auto-columns:10px;gap:3px}.activity-cell{width:10px;height:10px}.library-tabs{gap:5px}.library-tabs button{padding:0 13px;font-size:12px}.calendar-week,.calendar-days{gap:3px}.calendar-day{min-height:48px;padding:5px 3px;border-radius:7px}.calendar-day strong{font-size:11px}.calendar-day small{font-size:9px;padding:1px 3px}.calendar-previews{display:none}.color-panel{flex-direction:column;align-items:center;gap:14px}.color-options{width:100%}.calendar-heading{gap:6px}.calendar-heading h3{font-size:12px}.all-dates{font-size:11px}.activity-heading{margin-top:18px}.heat-legend{justify-content:flex-start}.library-selection>span:first-child{width:100%}}
</style>
