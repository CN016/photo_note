<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import AppHeader from '../components/AppHeader.vue'
import { api } from '../api/client'
import type { ScheduleBooking, ScheduleFields, ScheduleOccurrence, ScheduleShare } from '../api/types'
import { addDateKey, chinaDateKey, chinaDateTimeInput, clampScheduleDays, formatChinaDate, formatChinaTime, monthCalendarKeys, toChinaTimestamp } from '../schedule/scheduleUtils'

const { t: translate, locale } = useI18n()
function t(key: string, parameters?: Record<string, string | number>): string {
  return translate(key.startsWith('common.') ? key : `schedule.${key}`, parameters ?? {})
}
// 日历只持有当前用户请求返回的发生项；访客资料仅显示在拥有者的申请队列中。
const loading = ref(true)
const failed = ref(false)
const items = ref<ScheduleOccurrence[]>([])
const bookings = ref<ScheduleBooking[]>([])
const share = ref<ScheduleShare>({ enabled: false })
const view = ref<'month' | 'seven' | 'fourteen' | 'custom'>('month')
const customDays = ref(21)
const focusDate = ref(chinaDateKey())
const selectedDate = ref(chinaDateKey())
const dialogOpen = ref(false)
const editing = ref<ScheduleOccurrence | null>(null)
const editScope = ref<'series' | 'occurrence'>('series')
const saving = ref(false)
const shareBusy = ref(false)
const scopeTip = ref('')
const form = reactive<ScheduleFields>(emptySchedule())

const effectiveDays = computed(() => view.value === 'seven' ? 7 : view.value === 'fourteen' ? 14 : view.value === 'custom' ? clampScheduleDays(customDays.value) : 0)
const monthKey = computed(() => focusDate.value.slice(0, 7))
const visibleDates = computed(() => {
  if (view.value === 'month') return monthCalendarKeys(monthKey.value)
  const days = effectiveDays.value
  const start = addDateKey(focusDate.value, -(days - 1))
  return Array.from({ length: days }, (_, index) => addDateKey(start, index))
})
const fetchRange = computed(() => {
  const dates = visibleDates.value
  return { from: dates[0], to: dates[dates.length - 1] }
})
const eventsByDate = computed(() => {
  const map = new Map<string, ScheduleOccurrence[]>()
  for (const item of items.value) {
    const date = chinaDateKey(new Date(item.start_at))
    map.set(date, [...(map.get(date) ?? []), item])
  }
  for (const day of map.values()) day.sort((a, b) => a.start_at.localeCompare(b.start_at))
  return map
})
const selectedItems = computed(() => eventsByDate.value.get(selectedDate.value) ?? [])
const bookingList = computed(() => [...bookings.value].sort((a, b) => Number(b.status === 'pending') - Number(a.status === 'pending') || b.created_at.localeCompare(a.created_at)))
const shareURL = computed(() => share.value.url ? new URL(share.value.url, window.location.origin).toString() : '')
const weekdays = computed(() => translate('schedule.weekdays') as unknown as string[])
const canEdit = computed(() => !saving.value)

function emptySchedule(date = focusDate.value): ScheduleFields {
  const start = `${date}T09:00`
  const end = `${date}T10:00`
  return { kind: 'work', title: '', content: '', start_at: start, end_at: end, recurrence: 'none', weekdays: [], recurrence_end: '', capacity: 1 }
}

/** 将发生项映射为编辑字段；编辑整个系列时使用其原始开始和结束时刻。 */
function toFields(item: ScheduleOccurrence, occurrenceOnly: boolean): ScheduleFields {
  return {
    kind: item.kind,
    title: item.title,
    content: item.content,
    start_at: chinaDateTimeInput(occurrenceOnly ? item.start_at : item.series_start_at),
    end_at: chinaDateTimeInput(occurrenceOnly ? item.end_at : item.series_end_at),
    recurrence: occurrenceOnly ? 'none' : (item.recurrence as ScheduleFields['recurrence']),
    weekdays: occurrenceOnly ? [] : [...(item.weekdays ?? [])],
    recurrence_end: occurrenceOnly ? '' : item.recurrence_end,
    capacity: item.capacity,
  }
}

function itemsFor(date: string): ScheduleOccurrence[] { return eventsByDate.value.get(date) ?? [] }
function isCurrentMonth(date: string): boolean { return date.startsWith(monthKey.value) }
function dateLabel(date: string, options: Intl.DateTimeFormatOptions = { weekday: 'short', month: 'short', day: 'numeric' }): string { return formatChinaDate(date, locale.value, options) }
function timeRange(item: ScheduleOccurrence): string { return `${formatChinaTime(item.start_at, locale.value)}–${formatChinaTime(item.end_at, locale.value)}` }
function itemKind(item: ScheduleOccurrence): string { return item.kind === 'work' ? t('form.work') : t('form.availability') }
function bookingStatusLabel(status: string): string { return t(`bookings.${status === 'pending' ? status : `${status}Status`}`) }

/** 加载所选日期范围、拥有者申请队列和分享状态，接口根据当前 token 隔离数据。 */
async function load(): Promise<void> {
  loading.value = true
  failed.value = false
  const { from, to } = fetchRange.value
  try {
    const [calendar, requestList, shareStatus] = await Promise.all([api.scheduleItems(from, to), api.scheduleBookings('all'), api.scheduleShare()])
    items.value = calendar.items
    bookings.value = requestList.items ?? []
    share.value = shareStatus
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

/** 切换月历或日期窗口后重新请求对应的日期范围。 */
function changeView(next: typeof view.value): void {
  view.value = next
  if (next === 'month') focusDate.value = `${focusDate.value.slice(0, 7)}-15`
  void load()
}

/** 月历按月翻页，日数视图按当前窗口长度滚动。 */
function shiftWindow(direction: number): void {
  if (view.value === 'month') {
    const [year, month] = focusDate.value.slice(0, 7).split('-').map(Number)
    const next = new Date(Date.UTC(year, month - 1 + direction, 15, 12))
    focusDate.value = `${next.getUTCFullYear()}-${String(next.getUTCMonth() + 1).padStart(2, '0')}-15`
  } else {
    focusDate.value = addDateKey(focusDate.value, direction * effectiveDays.value)
  }
  selectedDate.value = focusDate.value
  void load()
}

function goToday(): void {
  focusDate.value = chinaDateKey()
  selectedDate.value = focusDate.value
  void load()
}

function selectDate(date: string): void {
  selectedDate.value = date
  if (view.value === 'month') focusDate.value = date
}

/** 打开新建表单时按选定的中国标准时间日期生成一小时默认档期。 */
function openNew(kind: ScheduleFields['kind'] = 'work', date = selectedDate.value): void {
  editing.value = null
  editScope.value = 'series'
  Object.assign(form, emptySchedule(date), { kind })
  scopeTip.value = ''
  dialogOpen.value = true
}

function openEdit(item: ScheduleOccurrence): void {
  editing.value = item
  editScope.value = 'series'
  Object.assign(form, toFields(item, false))
  scopeTip.value = ''
  dialogOpen.value = true
}

function setEditScope(scope: 'series' | 'occurrence'): void {
  if (!editing.value) return
  editScope.value = scope
  Object.assign(form, toFields(editing.value, scope === 'occurrence'))
  scopeTip.value = scope === 'occurrence' ? t('form.occurrenceEdit', { date: editing.value.series_date }) : ''
}

function handleEditScopeChange(value: string | number): void {
  if (value === 'series' || value === 'occurrence') setEditScope(value)
}

function payload(): ScheduleFields {
  return {
    ...form,
    start_at: toChinaTimestamp(form.start_at),
    end_at: toChinaTimestamp(form.end_at),
    weekdays: form.recurrence === 'weekly' ? [...form.weekdays].sort() : [],
    recurrence_end: form.recurrence === 'none' ? '' : form.recurrence_end,
    capacity: form.kind === 'work' ? 1 : Number(form.capacity || 1),
  }
}

/** 保存前执行表单校验；服务端再次检查重复规则、时间重叠和用户归属。 */
async function save(): Promise<void> {
  if (!form.title.trim() || !form.start_at || !form.end_at || form.recurrence === 'weekly' && form.weekdays.length === 0 || form.kind === 'availability' && form.recurrence !== 'none' && !form.recurrence_end) {
    ElMessage.warning(t('form.validation'))
    return
  }
  saving.value = true
  try {
    const body = payload()
    if (!editing.value) await api.createScheduleItem(body)
    else if (editScope.value === 'occurrence') await api.updateScheduleOccurrence(editing.value.business_id, editing.value.series_date, body)
    else await api.updateScheduleItem(editing.value.business_id, body)
    dialogOpen.value = false
    ElMessage.success(t('form.saved'))
    await load()
  } catch {
    ElMessage.error(t('form.conflict'))
  } finally {
    saving.value = false
  }
}

/** 删除整个系列前提示确认；后端保留已有客户申请记录。 */
async function remove(item: ScheduleOccurrence): Promise<void> {
  try {
    await ElMessageBox.confirm(t('form.confirmDelete'), t('actions.delete'), { type: 'warning', confirmButtonText: t('actions.delete'), cancelButtonText: t('actions.close') })
    await api.deleteScheduleItem(item.business_id)
    ElMessage.success(t('form.deleted'))
    await load()
  } catch { /* 用户取消或请求失败时保留当前档期 */ }
}

/** 写入当前重复发生日期的取消例外，不修改其他日期。 */
async function cancelOccurrence(item: ScheduleOccurrence): Promise<void> {
  try {
    await ElMessageBox.confirm(t('actions.cancelOccurrence') + ` · ${item.series_date}`, t('actions.cancelOccurrence'), { type: 'warning' })
    await api.cancelScheduleOccurrence(item.business_id, item.series_date)
    await load()
  } catch { /* 用户取消时不更改档期 */ }
}

/** 创建或轮换分享链接；服务端只保存令牌摘要，明文链接由本次响应交付。 */
async function updateShare(): Promise<void> {
  shareBusy.value = true
  try {
    const result = share.value.enabled ? await api.createScheduleShare() : await api.createScheduleShare()
    share.value = result
    ElMessage.success(t('share.created'))
  } catch { ElMessage.error(t('share.failed')) }
  finally { shareBusy.value = false }
}

async function revokeShare(): Promise<void> {
  shareBusy.value = true
  try {
    await api.revokeScheduleShare()
    share.value = { enabled: false }
    ElMessage.success(t('share.revoked'))
  } catch { ElMessage.error(t('share.failed')) }
  finally { shareBusy.value = false }
}

async function copyShare(): Promise<void> {
  if (!shareURL.value) return
  try { await navigator.clipboard.writeText(shareURL.value); ElMessage.success(t('share.copied')) }
  catch { ElMessage.error(t('share.failed')) }
}

async function decide(booking: ScheduleBooking, choice: 'confirm' | 'reject' | 'cancel'): Promise<void> {
  try {
    if (choice === 'cancel') await api.cancelScheduleBooking(booking.business_id)
    else await api.decideScheduleBooking(booking.business_id, choice)
    await load()
  } catch { ElMessage.error(t('bookings.failed')) }
}

function bookingDate(booking: ScheduleBooking): string { return `${formatChinaDate(chinaDateKey(new Date(booking.start_at)), locale.value)} ${formatChinaTime(booking.start_at, locale.value)}` }

onMounted(() => { void load() })
</script>

<template>
  <AppHeader />
  <main class="schedule-main">
    <header class="schedule-heading">
      <div><p class="schedule-eyebrow">{{ t('title') }}</p><h1>{{ t('title') }}</h1><p>{{ t('subtitle') }}</p></div>
      <el-button type="primary" @click="openNew('work')">＋ {{ t('actions.add') }}</el-button>
    </header>

    <section class="schedule-share-card">
      <div class="share-copy"><span class="share-icon" aria-hidden="true">↗</span><div><h2>{{ t('share.title') }}</h2><p>{{ t('share.help') }}</p><small>{{ share.enabled ? t('share.enabled') : t('share.disabled') }}</small></div></div>
      <div class="share-actions">
        <a v-if="shareURL" class="share-url" :href="shareURL" target="_blank" rel="noreferrer">{{ shareURL }}</a>
        <el-button v-if="shareURL" @click="copyShare">{{ t('actions.copy') }}</el-button>
        <el-button type="primary" :loading="shareBusy" @click="updateShare">{{ share.enabled ? t('actions.rotateShare') : t('actions.share') }}</el-button>
        <el-button v-if="share.enabled" type="danger" plain :loading="shareBusy" @click="revokeShare">{{ t('actions.revokeShare') }}</el-button>
      </div>
    </section>

    <div class="schedule-layout">
      <section class="schedule-calendar-panel">
        <div class="schedule-toolbar">
          <div class="schedule-view-switch" role="group" :aria-label="t('title')">
            <button :class="{ active: view === 'month' }" @click="changeView('month')">{{ t('views.month') }}</button>
            <button :class="{ active: view === 'seven' }" @click="changeView('seven')">{{ t('views.seven') }}</button>
            <button :class="{ active: view === 'fourteen' }" @click="changeView('fourteen')">{{ t('views.fourteen') }}</button>
            <button :class="{ active: view === 'custom' }" @click="changeView('custom')">{{ t('views.custom') }}</button>
          </div>
          <el-input-number v-if="view === 'custom'" v-model="customDays" :min="1" :max="90" :step="1" controls-position="right" :aria-label="t('views.custom')" @change="customDays = clampScheduleDays(customDays); load()" />
          <div class="schedule-date-nav"><el-button circle plain :aria-label="t('actions.previous')" @click="shiftWindow(-1)">‹</el-button><strong>{{ view === 'month' ? formatChinaDate(`${monthKey}-01`, locale, { year: 'numeric', month: 'long' }) : `${dateLabel(fetchRange.from, { month: 'short', day: 'numeric' })} – ${dateLabel(fetchRange.to, { month: 'short', day: 'numeric' })}` }}</strong><el-button circle plain :aria-label="t('actions.next')" @click="shiftWindow(1)">›</el-button><el-button plain @click="goToday">{{ t('actions.today') }}</el-button></div>
        </div>

        <div v-if="loading" class="schedule-state" role="status">{{ t('loading') }}</div>
        <div v-else-if="failed" class="schedule-state is-error"><p>{{ t('loadError') }}</p><el-button @click="load">{{ t('retry') }}</el-button></div>
        <template v-else>
          <div v-if="view === 'month'" class="month-calendar">
            <div class="month-weekdays"><span v-for="day in weekdays" :key="day">{{ day }}</span></div>
            <div class="month-grid">
              <button v-for="date in visibleDates" :key="date" class="month-day" :class="{ outside: !isCurrentMonth(date), selected: selectedDate === date, today: date === chinaDateKey() }" @click="selectDate(date)">
                <span class="month-day-number">{{ Number(date.slice(-2)) }}</span>
                <span v-for="item in itemsFor(date).slice(0, 3)" :key="`${item.business_id}-${item.series_date}`" class="month-event" :class="`kind-${item.kind}`"><b>{{ formatChinaTime(item.start_at, locale) }}</b> {{ item.title }}</span>
                <small v-if="itemsFor(date).length > 3" class="month-more">{{ t('calendar.more', { count: itemsFor(date).length - 3 }) }}</small>
              </button>
            </div>
          </div>
          <div v-else class="schedule-days" :class="`days-${view}`" :style="{ '--schedule-day-count': String(effectiveDays) }">
            <article v-for="date in visibleDates" :key="date" class="schedule-day-card" :class="{ selected: selectedDate === date, today: date === chinaDateKey() }" @click="selectDate(date)">
              <button class="schedule-day-heading" @click.stop="selectDate(date)"><strong>{{ dateLabel(date, { weekday: 'long', month: 'long', day: 'numeric' }) }}</strong><span>{{ itemsFor(date).length }}</span></button>
              <button v-if="!itemsFor(date).length" class="day-add-empty" @click="openNew('work', date)">＋ {{ t('actions.add') }}</button>
              <button v-for="item in itemsFor(date)" :key="`${item.business_id}-${item.series_date}`" class="schedule-event-card" :class="`kind-${item.kind}`" @click.stop="selectDate(date)">
                <span class="event-card-time">{{ timeRange(item) }}</span><strong>{{ item.title }}</strong><span class="event-card-kind">{{ itemKind(item) }}<template v-if="item.kind === 'availability'"> · {{ item.capacity }} {{ t('form.capacity') }}</template></span><p v-if="item.content">{{ item.content }}</p>
              </button>
            </article>
          </div>

          <section class="schedule-selected-details" aria-live="polite">
            <div class="selected-details-heading"><div><small>{{ t('calendar.details') }}</small><h2>{{ dateLabel(selectedDate, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' }) }}</h2></div><div><el-button @click="openNew('work', selectedDate)">{{ t('actions.addWork') }}</el-button><el-button type="primary" plain @click="openNew('availability', selectedDate)">{{ t('actions.addAvailability') }}</el-button></div></div>
            <p v-if="!selectedItems.length" class="empty-day">{{ t('calendar.noItems') }}</p>
            <article v-for="item in selectedItems" :key="`${item.business_id}-${item.series_date}`" class="detail-event" :class="`kind-${item.kind}`">
              <div><small>{{ itemKind(item) }} · {{ timeRange(item) }}</small><h3>{{ item.title }}</h3><p v-if="item.content">{{ item.content }}</p><small v-if="item.recurrence !== 'none'">{{ t(`form.${item.recurrence}`) }}<template v-if="item.recurrence_end"> · {{ item.recurrence_end }}</template></small></div>
              <div class="detail-actions"><el-button text @click="openEdit(item)">{{ t('actions.edit') }}</el-button><el-button v-if="item.recurrence !== 'none'" text type="warning" @click="cancelOccurrence(item)">{{ t('actions.cancelOccurrence') }}</el-button><el-button text type="danger" @click="remove(item)">{{ t('actions.delete') }}</el-button></div>
            </article>
          </section>
        </template>
      </section>

      <aside class="schedule-bookings-panel">
        <div class="bookings-heading"><div><small>{{ t('bookings.title') }}</small><span v-if="bookings.length">{{ bookings.length }}</span></div><el-button text @click="load">{{ t('actions.refresh') }}</el-button></div>
        <p v-if="!bookings.length" class="empty-bookings">{{ t('bookings.empty') }}</p>
        <article v-for="booking in bookingList" :key="booking.business_id" class="booking-card">
          <div class="booking-card-heading"><strong>{{ booking.guest_name }}</strong><span>{{ bookingStatusLabel(booking.status) }}</span></div>
          <p>{{ bookingDate(booking) }}</p><p><a :href="`tel:${booking.guest_phone}`">{{ booking.guest_phone }}</a></p><p v-if="booking.note">{{ booking.note }}</p>
          <small>{{ t('bookings.requestCode') }}: {{ booking.request_code }}</small><small v-if="booking.expires_at">{{ t('bookings.expires') }}: {{ formatChinaDate(chinaDateKey(new Date(booking.expires_at)), locale) }} {{ formatChinaTime(booking.expires_at, locale) }}</small>
          <div v-if="booking.status === 'pending'" class="booking-actions"><el-button type="primary" size="small" @click="decide(booking, 'confirm')">{{ t('actions.confirm') }}</el-button><el-button type="danger" plain size="small" @click="decide(booking, 'reject')">{{ t('actions.reject') }}</el-button></div>
          <div v-else-if="booking.status === 'confirmed'" class="booking-actions"><el-button type="danger" plain size="small" @click="decide(booking, 'cancel')">{{ t('actions.cancelBooking') }}</el-button></div>
        </article>
      </aside>
    </div>

    <el-dialog v-model="dialogOpen" :title="editing ? t('form.editTitle') : t('form.newTitle')" width="min(620px, calc(100vw - 28px))" destroy-on-close>
      <div v-if="editing?.recurrence !== 'none' && editing" class="edit-scope-switch">
        <el-radio-group :model-value="editScope" @change="handleEditScopeChange">
          <el-radio-button value="series">{{ t('actions.edit') }} · {{ t('form.recurrence') }}</el-radio-button>
          <el-radio-button value="occurrence">{{ t('form.occurrenceEdit', { date: editing.series_date }) }}</el-radio-button>
        </el-radio-group>
      </div>
      <p v-if="scopeTip" class="schedule-form-hint">{{ scopeTip }}</p>
      <el-form label-position="top" @submit.prevent="save">
        <el-form-item :label="t('form.kind')"><el-radio-group v-model="form.kind" :disabled="Boolean(editing)"><el-radio-button value="work">{{ t('form.work') }}</el-radio-button><el-radio-button value="availability">{{ t('form.availability') }}</el-radio-button></el-radio-group></el-form-item>
        <el-form-item :label="t('form.title')" required><el-input v-model="form.title" maxlength="120" show-word-limit :placeholder="t('form.titlePlaceholder')" /></el-form-item>
        <el-form-item :label="t('form.content')"><el-input v-model="form.content" type="textarea" maxlength="4000" show-word-limit :rows="3" :placeholder="t('form.contentPlaceholder')" /></el-form-item>
        <div class="schedule-form-grid"><el-form-item :label="t('form.start')" required><el-input v-model="form.start_at" type="datetime-local" /></el-form-item><el-form-item :label="t('form.end')" required><el-input v-model="form.end_at" type="datetime-local" /></el-form-item></div>
        <el-form-item v-if="editScope === 'series'" :label="t('form.recurrence')"><el-select v-model="form.recurrence"><el-option v-for="key in ['none', 'daily', 'weekly', 'monthly']" :key="key" :label="t(`form.${key}`)" :value="key" /></el-select></el-form-item>
        <el-form-item v-if="editScope === 'series' && form.recurrence === 'weekly'" :label="t('form.weekdays')"><el-checkbox-group v-model="form.weekdays"><el-checkbox v-for="day in 7" :key="day" :value="day">{{ weekdays[day - 1] }}</el-checkbox></el-checkbox-group></el-form-item>
        <el-form-item v-if="editScope === 'series' && form.recurrence !== 'none'" :label="t('form.recurrenceEnd')"><el-input v-model="form.recurrence_end" type="date" /><small class="schedule-form-hint">{{ t('form.recurrenceEndHelp') }}</small></el-form-item>
        <el-form-item v-if="form.kind === 'availability'" :label="t('form.capacity')"><el-input-number v-model="form.capacity" :min="1" :max="100" /><small class="schedule-form-hint">{{ t('form.capacityHelp') }}</small></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialogOpen = false">{{ t('actions.close') }}</el-button><el-button type="primary" :loading="saving" :disabled="!canEdit" @click="save">{{ t('actions.save') }}</el-button></template>
    </el-dialog>
  </main>
</template>

<style scoped>
.schedule-main{width:min(1440px,calc(100vw - 310px));margin:0 42px 0 calc(226px + max(0px,(100vw - 1708px)/2));padding:42px 0 90px;color:var(--ink)}
.schedule-heading{display:flex;align-items:flex-end;justify-content:space-between;gap:20px;margin-bottom:22px}.schedule-eyebrow,.schedule-heading>div>p:last-child{margin:0;color:var(--muted);font-size:12px}.schedule-heading h1{margin:7px 0;font-size:clamp(27px,3vw,38px);letter-spacing:-.04em}.schedule-heading>div>p:last-child{font-size:13px}
.schedule-share-card,.schedule-calendar-panel,.schedule-bookings-panel{border:1px solid var(--line);border-radius:8px;background:var(--surface);box-shadow:var(--soft-shadow)}.schedule-share-card{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:17px 20px;margin-bottom:17px}.share-copy{display:flex;align-items:center;gap:12px;min-width:0}.share-icon{display:grid;width:39px;height:39px;flex:none;place-items:center;border-radius:6px;background:var(--accent-soft);color:var(--accent);font-size:20px}.share-copy h2{margin:0 0 3px;font-size:14px}.share-copy p,.share-copy small{margin:0;color:var(--muted);font-size:11px}.share-copy small{display:block;margin-top:4px;color:var(--accent)}.share-actions{display:flex;align-items:center;justify-content:flex-end;gap:8px;flex-wrap:wrap}.share-url{max-width:250px;overflow:hidden;color:var(--accent);font-size:11px;text-overflow:ellipsis;white-space:nowrap}
.schedule-layout{display:grid;grid-template-columns:minmax(0,1fr) 300px;align-items:start;gap:17px}.schedule-calendar-panel{min-width:0;padding:17px}.schedule-toolbar{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;margin-bottom:17px}.schedule-view-switch{display:flex;gap:3px;padding:3px;border-radius:5px;background:var(--surface-soft)}.schedule-view-switch button{min-height:35px;padding:0 11px;border:0;border-radius:4px;background:transparent;color:var(--muted);cursor:pointer;font:inherit;font-size:11px}.schedule-view-switch button.active{background:var(--surface);color:var(--accent-hover);box-shadow:0 1px 4px #17282116}.schedule-date-nav{display:flex;align-items:center;gap:7px}.schedule-date-nav strong{min-width:135px;color:var(--ink-soft);font-size:12px;text-align:center}.schedule-date-nav :deep(.el-button.is-circle){width:38px;height:38px}.schedule-state{display:grid;min-height:250px;place-items:center;color:var(--muted);text-align:center}.schedule-state.is-error{align-content:center;gap:8px;color:var(--danger)}
.month-calendar{width:100%;overflow:auto}.month-weekdays,.month-grid{display:grid;min-width:570px;grid-template-columns:repeat(7,minmax(0,1fr))}.month-weekdays{border-bottom:1px solid var(--line);color:var(--muted);font-size:10px;text-align:center}.month-weekdays span{padding:9px 2px}.month-day{display:flex;min-width:0;min-height:108px;flex-direction:column;align-items:stretch;gap:4px;padding:7px 6px;border:0;border-right:1px solid var(--line);border-bottom:1px solid var(--line);background:transparent;color:var(--ink);text-align:left;cursor:pointer}.month-day:hover,.month-day.selected{background:var(--surface-soft)}.month-day.outside{color:var(--muted);opacity:.65}.month-day.today .month-day-number{color:var(--accent);font-weight:800}.month-day-number{align-self:flex-end;font-size:11px}.month-event{display:block;overflow:hidden;padding:3px 4px;border-radius:3px;background:var(--accent-soft);color:var(--accent-hover);font-size:9px;text-overflow:ellipsis;white-space:nowrap}.month-event.kind-work{background:color-mix(in srgb,#82918b 16%,var(--surface));color:var(--ink-soft)}.month-event b{font-size:8px}.month-more{color:var(--muted);font-size:9px}
.schedule-days{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,260px),1fr));align-items:start;gap:11px}.schedule-days.days-seven{grid-template-columns:repeat(3,minmax(0,1fr))}.schedule-days.days-fourteen{grid-template-columns:repeat(5,minmax(0,1fr))}.schedule-day-card{min-width:0;min-height:190px;padding:13px;border:1px solid var(--line);border-radius:6px;background:var(--surface)}.schedule-day-card.selected{border-color:var(--accent);box-shadow:0 0 0 1px var(--accent) inset}.schedule-day-card.today .schedule-day-heading{color:var(--accent)}.schedule-day-heading{display:flex;width:100%;min-height:37px;align-items:center;justify-content:space-between;gap:8px;padding:0 0 9px;border:0;border-bottom:1px solid var(--line);background:none;color:var(--ink);text-align:left;cursor:pointer}.schedule-day-heading strong{font-size:12px}.schedule-day-heading span{color:var(--muted);font-size:10px}.schedule-event-card{display:flex;width:100%;flex-direction:column;align-items:flex-start;gap:5px;margin-top:9px;padding:10px;border:1px solid color-mix(in srgb,var(--accent) 25%,var(--line));border-left:3px solid var(--accent);border-radius:4px;background:var(--accent-soft);color:var(--ink);text-align:left;cursor:pointer}.schedule-event-card.kind-work{border-color:var(--line);border-left-color:#87938b;background:var(--surface-soft)}.event-card-time,.event-card-kind{color:var(--muted);font-size:10px}.event-card-time{font-weight:750}.schedule-event-card strong{font-size:12px;overflow-wrap:anywhere}.schedule-event-card p{margin:0;color:var(--ink-soft);font-size:11px;line-height:1.55;white-space:pre-wrap;overflow-wrap:anywhere}.day-add-empty{width:100%;min-height:88px;margin-top:10px;border:1px dashed var(--line-strong);border-radius:4px;background:transparent;color:var(--muted);cursor:pointer;font-size:11px}
.schedule-selected-details{margin-top:21px;padding-top:18px;border-top:1px solid var(--line)}.selected-details-heading{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:10px}.selected-details-heading small,.detail-event small{color:var(--muted);font-size:10px}.selected-details-heading h2{margin:4px 0;font-size:16px}.selected-details-heading>div:last-child{display:flex;gap:7px;flex-wrap:wrap}.empty-day,.empty-bookings{padding:14px 0;color:var(--muted);font-size:12px}.detail-event{display:flex;justify-content:space-between;gap:14px;padding:12px 0;border-top:1px solid var(--line)}.detail-event h3{margin:5px 0;font-size:13px}.detail-event p{margin:4px 0;color:var(--ink-soft);font-size:12px;line-height:1.6;white-space:pre-wrap}.detail-actions{display:flex;flex-wrap:wrap;align-items:flex-start;justify-content:flex-end}.schedule-bookings-panel{position:sticky;top:18px;padding:16px}.bookings-heading{display:flex;align-items:center;justify-content:space-between;padding-bottom:12px;border-bottom:1px solid var(--line)}.bookings-heading>div{display:flex;align-items:center;gap:8px}.bookings-heading small{font-size:13px;font-weight:750}.bookings-heading span{display:grid;min-width:20px;height:20px;place-items:center;border-radius:4px;background:var(--accent-soft);color:var(--accent);font-size:10px}.booking-card{display:grid;gap:7px;padding:13px 0;border-bottom:1px solid var(--line)}.booking-card-heading{display:flex;align-items:center;justify-content:space-between;gap:8px}.booking-card-heading strong{font-size:12px}.booking-card-heading span{padding:4px 6px;border-radius:4px;background:var(--accent-soft);color:var(--accent);font-size:9px}.booking-card p{margin:0;color:var(--ink-soft);font-size:11px;white-space:pre-wrap;overflow-wrap:anywhere}.booking-card a{color:inherit}.booking-card>small{color:var(--muted);font-size:9px}.booking-actions{display:flex;gap:7px;margin-top:3px}
.edit-scope-switch{margin-bottom:13px}.schedule-form-grid{display:grid;grid-template-columns:1fr 1fr;gap:12px}.schedule-form-hint{display:block;margin-top:5px;color:var(--muted);font-size:10px;line-height:1.5}
@media(max-width:1200px){.schedule-layout{grid-template-columns:minmax(0,1fr) 260px}.schedule-days.days-seven{grid-template-columns:repeat(3,minmax(0,1fr))}.schedule-days.days-fourteen{grid-template-columns:repeat(5,minmax(0,1fr))}.schedule-toolbar{align-items:flex-start}.schedule-date-nav{margin-left:auto}}
@media(max-width:1100px){.schedule-layout{grid-template-columns:1fr}.schedule-bookings-panel{position:static}.schedule-days.days-fourteen{grid-template-columns:repeat(3,minmax(0,1fr))}}
@media(max-width:900px){.schedule-main{width:calc(100% - 34px);margin:0 auto;padding:30px 0 calc(105px + env(safe-area-inset-bottom))}.schedule-layout{grid-template-columns:1fr}.schedule-bookings-panel{position:static}.schedule-days.days-seven{grid-template-columns:repeat(2,minmax(0,1fr))}.schedule-days.days-fourteen{grid-template-columns:repeat(3,minmax(0,1fr))}.schedule-share-card{align-items:flex-start;flex-direction:column}.share-actions{justify-content:flex-start}.month-day{min-height:90px}}
@media(max-width:560px){.schedule-main{width:calc(100% - 24px);padding-top:24px}.schedule-heading{align-items:flex-start}.schedule-heading h1{font-size:27px}.schedule-heading>el-button{flex:none}.schedule-heading :deep(.el-button){min-height:44px;padding:0 11px;font-size:11px}.schedule-calendar-panel{padding:11px}.schedule-toolbar{gap:9px}.schedule-view-switch{width:100%;overflow:auto}.schedule-view-switch button{flex:1;min-width:max-content;padding:0 8px;font-size:10px}.schedule-date-nav{width:100%;justify-content:center;margin-left:0}.schedule-date-nav strong{flex:1}.schedule-days,.schedule-days.days-seven,.schedule-days.days-fourteen{grid-template-columns:1fr}.schedule-day-card{min-height:0;padding:12px}.schedule-event-card{padding:12px}.schedule-event-card p{font-size:12px}.selected-details-heading{align-items:flex-start;flex-direction:column}.selected-details-heading>div:last-child{width:100%}.selected-details-heading :deep(.el-button){flex:1;min-height:44px;margin:0}.detail-event{flex-direction:column}.detail-actions{justify-content:flex-start}.detail-actions :deep(.el-button){min-height:40px}.share-actions{width:100%}.share-actions :deep(.el-button){min-height:42px;margin:0}.share-url{max-width:100%;flex:1}.schedule-form-grid{grid-template-columns:1fr;gap:0}.edit-scope-switch :deep(.el-radio-group){display:flex;width:100%}.edit-scope-switch :deep(.el-radio-button){flex:1}.edit-scope-switch :deep(.el-radio-button__inner){width:100%;height:auto;min-height:44px;white-space:normal}.month-day{min-height:84px}}
</style>
