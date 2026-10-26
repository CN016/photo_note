<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown, ArrowLeft, ArrowRight, BottomRight, Calendar, Close, EditPen, Plus, Search, Switch, TopRight, TrendCharts } from '@element-plus/icons-vue'
import AppHeader from '../components/AppHeader.vue'
import LedgerAnalytics from '../ledger/LedgerAnalytics.vue'
import { exportFile, FileExportError } from '../platform'
import { api } from '../api/client'
import { type LedgerFields, type LedgerImportItem, type LedgerRecord } from '../api/types'
import {
  amountToFen, csvCell, editableFields, formatFen, fromDateKey, hasPendingDraft,
  ledgerFingerprint, parseLedgerCSV, parseLedgerJSON, toDateKey, toMonthKey,
} from '../ledger/ledgerUtils'

const { t: translate, locale, tm: translateMessage } = useI18n()
type SaveState = 'saved' | 'saving' | 'error'

/** 默认查询账本命名空间，共享文案继续从公共语言包读取。 */
function t(key: string, parameters?: Record<string, string | number>): string {
  return translate(key.startsWith('common.') ? key : `ledger.${key}`, parameters ?? {})
}

/** 读取账本语言 JSON 中的数组等非字符串消息。 */
function tm(key: string): unknown { return translateMessage(`ledger.${key}`) }

const records = ref<LedgerRecord[]>([])
const loading = ref(true)
const loadError = ref(false)
const activeMonth = ref(toMonthKey(new Date()))
const selectedDate = ref(toDateKey(new Date()))
const searchText = ref('')
const typeFilter = ref<'' | LedgerFields['type']>('')
const categoryFilter = ref('')
const methodFilter = ref('')
const flagFilter = ref<LedgerFields['flag'] | ''>('')
const minimumAmount = ref('')

const dayPanelOpen = ref(false)
const editorOpen = ref(false)
const editorID = ref<string | null>(null)
const saveState = ref<SaveState>('saved')
const editorSequence = ref(0)
const editorSavedSequence = ref(0)
const savingDelete = ref(false)
const fields = reactive<LedgerFields>(emptyFields(selectedDate.value))
const hydrating = ref(false)
let saveTimer: number | undefined

const latestSequence = new Map<string, number>()
const savedSequence = new Map<string, number>()
const revisionByID = new Map<string, number>()
const saveQueues = new Map<string, Promise<void>>()
const yearLoads = new Map<number, Promise<void>>()

const importOpen = ref(false)
const importRecords = ref<LedgerImportItem[]>([])
const importInvalidCount = ref(0)
const importTooMany = ref(false)
const importBusy = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

const compareMode = ref<'month' | 'year'>('month')
const periodA = ref(toMonthKey(new Date()))
const periodB = ref(toMonthKey(new Date(new Date().getFullYear(), new Date().getMonth() - 1, 1)))

/** 创建一份可立即保存的空草稿字段。 */
function emptyFields(date: string): LedgerFields {
  return {
    date,
    time: new Date().toTimeString().slice(0, 5),
    type: 'income',
    amount: '',
    category: '',
    payment_method: 'wechat',
    flag: '',
    note: '',
  }
}

/** 以服务端业务 UUID 合并记录，保留不同日期范围已加载的本地缓存。 */
function upsertRecord(record: LedgerRecord): void {
  const index = records.value.findIndex(item => item.business_id === record.business_id)
  if (index < 0) records.value = [record, ...records.value]
  else records.value = records.value.map(item => item.business_id === record.business_id ? record : item)
  revisionByID.set(record.business_id, record.draft_version)
}

/** 读取某年的正式账目与当前用户全部未完成草稿。 */
async function loadYear(year: number): Promise<void> {
  const active = yearLoads.get(year)
  if (active) return active
  const request = (async () => {
    const result = await api.ledgerRecords(`${year}-01-01`, `${year}-12-31`)
    const rows = result.items
    const next = new Map<string, LedgerRecord>()
    for (const item of records.value) {
      if (!(item.status === 'complete' && item.date.startsWith(`${year}-`))) next.set(item.business_id, item)
    }
    for (const item of rows) next.set(item.business_id, item)
    records.value = [...next.values()]
  })()
  yearLoads.set(year, request)
  try { await request }
  finally { yearLoads.delete(year) }
}

/** 读取用户全部日期范围，用于完整导出和准确的导入去重预览。 */
async function loadAllRecords(): Promise<LedgerRecord[]> {
  const result = await api.ledgerRecords('0001-01-01', '9999-12-31')
  records.value = result.items
  return result.items
}

/** 对正式字段应用统一筛选，草稿永远不进入日历、汇总或分析。 */
function matchesFilters(record: LedgerRecord): boolean {
  if (record.status !== 'complete') return false
  if (typeFilter.value && record.type !== typeFilter.value) return false
  if (categoryFilter.value && record.category !== categoryFilter.value) return false
  if (methodFilter.value && record.payment_method !== methodFilter.value) return false
  if (flagFilter.value && record.flag !== flagFilter.value) return false
  const minFen = minimumAmount.value ? amountToFen(minimumAmount.value) : null
  const recordFen = amountToFen(record.amount) ?? 0
  if (minFen !== null && recordFen < minFen) return false
  const keyword = searchText.value.trim().toLocaleLowerCase()
  if (keyword && !`${record.date} ${record.note} ${record.category} ${record.payment_method}`.toLocaleLowerCase().includes(keyword)) return false
  return true
}

const completeRecords = computed(() => records.value.filter(record => record.status === 'complete'))
const pendingRecords = computed(() => records.value.filter(hasPendingDraft).sort((a, b) => b.updated_at.localeCompare(a.updated_at)))
const categories = computed(() => [...new Set(completeRecords.value.map(record => record.category).filter(Boolean))].sort((a, b) => a.localeCompare(b, locale.value)))
const methods = computed(() => [...new Set(completeRecords.value.map(record => record.payment_method).filter(Boolean))].sort((a, b) => a.localeCompare(b, locale.value)))
const categorySuggestions = computed(() => (tm(fields.type === 'income' ? 'record.incomeCategories' : 'record.expenseCategories') as string[]))
const selectedMonthRecords = computed(() => completeRecords.value.filter(record => record.date.startsWith(activeMonth.value) && matchesFilters(record)))

/** 生成从周日开始的六周日历网格，日期键均使用本地日历日期。 */
const calendarCells = computed(() => {
  const [year, month] = activeMonth.value.split('-').map(Number)
  const first = new Date(year, month - 1, 1)
  const start = new Date(year, month - 1, 1 - first.getDay())
  return Array.from({ length: 42 }, (_, index) => {
    const date = new Date(start)
    date.setDate(start.getDate() + index)
    return { key: toDateKey(date), number: date.getDate(), inMonth: date.getMonth() === month - 1 }
  })
})

const weekdayNames = computed(() => Array.from({ length: 7 }, (_, index) =>
  new Intl.DateTimeFormat(locale.value, { weekday: 'short' }).format(new Date(2024, 0, 7 + index)),
))

/** 根据记录日期过滤列表，金额始终以整数分累加。 */
function dateRecords(date: string): LedgerRecord[] {
  return selectedMonthRecords.value.filter(record => record.date === date).sort((a, b) => a.time.localeCompare(b.time) || b.created_at.localeCompare(a.created_at))
}

/** 汇总一组正式记录，统计支出时从收入中独立扣除。 */
function statsOf(items: LedgerRecord[]) {
  let incomeFen = 0
  let expenseFen = 0
  const activeDays = new Set<string>()
  const incomeByDay = new Map<string, number>()
  for (const item of items) {
    const fen = amountToFen(item.amount) ?? 0
    if (item.type === 'income') { incomeFen += fen; incomeByDay.set(item.date, (incomeByDay.get(item.date) ?? 0) + fen) }
    else expenseFen += fen
    activeDays.add(item.date)
  }
  return {
    incomeFen, expenseFen, netFen: incomeFen - expenseFen, count: items.length,
    ticketFen: items.length ? Math.round((incomeFen + expenseFen) / items.length) : 0,
    bestIncomeFen: Math.max(0, ...incomeByDay.values()),
    activeDays: activeDays.size, averageFen: activeDays.size ? Math.round((incomeFen - expenseFen) / activeDays.size) : 0,
  }
}

const monthStats = computed(() => statsOf(selectedMonthRecords.value))
const selectedDayRecords = computed(() => dateRecords(selectedDate.value))
const agendaDays = computed(() => {
  const days = [...new Set(selectedMonthRecords.value.map(record => record.date))].sort((a, b) => b.localeCompare(a))
  return days.map(date => ({ date, records: dateRecords(date), stats: statsOf(dateRecords(date)) }))
})
const calendarMaxFen = computed(() => Math.max(1, ...agendaDays.value.map(day => day.stats.incomeFen + day.stats.expenseFen)))

/** 生成当前月按日净额序列，用于趋势图和月报图片。 */
const dailyTrend = computed(() => {
  const [year, month] = activeMonth.value.split('-').map(Number)
  const count = new Date(year, month, 0).getDate()
  return Array.from({ length: count }, (_, index) => {
    const date = toDateKey(new Date(year, month - 1, index + 1))
    return { date, day: index + 1, ...statsOf(dateRecords(date)) }
  })
})

/** 按星期、分类和收付款方式聚合当前所选月份。 */
const weekdayTotals = computed(() => Array.from({ length: 7 }, (_, weekday) => {
  const items = selectedMonthRecords.value.filter(record => fromDateKey(record.date).getDay() === weekday)
  const stats = statsOf(items)
  return { weekday, amount: stats.incomeFen + stats.expenseFen, count: items.length }
}))
const weekdayMax = computed(() => Math.max(1, ...weekdayTotals.value.map(item => item.amount)))
const categoryTotals = computed(() => aggregateBy(selectedMonthRecords.value, record => record.category || '—'))
const methodTotals = computed(() => aggregateBy(selectedMonthRecords.value, record => record.payment_method || 'other'))

/** 按指定字段合计分类或方式的收入、支出和净额。 */
function aggregateBy(items: LedgerRecord[], keyOf: (record: LedgerRecord) => string) {
  const grouped = new Map<string, { key: string; incomeFen: number; expenseFen: number; count: number }>()
  for (const record of items) {
    const key = keyOf(record)
    const row = grouped.get(key) ?? { key, incomeFen: 0, expenseFen: 0, count: 0 }
    const amount = amountToFen(record.amount) ?? 0
    if (record.type === 'income') row.incomeFen += amount
    else row.expenseFen += amount
    row.count += 1
    grouped.set(key, row)
  }
  return [...grouped.values()].sort((a, b) => (b.incomeFen + b.expenseFen) - (a.incomeFen + a.expenseFen)).slice(0, 8)
}

/** 返回当前年份所有每日记录，用于年度月度汇总和热力图。 */
const activeYear = computed(() => Number(activeMonth.value.slice(0, 4)))
const yearlyRecords = computed(() => completeRecords.value.filter(record => record.date.startsWith(`${activeYear.value}-`) && matchesFilters(record)))
const yearlyMonths = computed(() => Array.from({ length: 12 }, (_, month) => {
  const prefix = `${activeYear.value}-${String(month + 1).padStart(2, '0')}`
  return { month: month + 1, stats: statsOf(yearlyRecords.value.filter(record => record.date.startsWith(prefix))) }
}))
const monthMaxAbsNet = computed(() => Math.max(1, ...yearlyMonths.value.map(item => Math.abs(item.stats.netFen))))
const yearlyHeat = computed(() => {
  const count = new Date(activeYear.value, 1, 29).getMonth() === 1 ? 366 : 365
  const max = Math.max(1, ...yearlyRecords.value.map(record => Math.abs(amountToFen(record.amount) ?? 0)))
  return Array.from({ length: count }, (_, index) => {
    const date = toDateKey(new Date(activeYear.value, 0, index + 1))
    const items = yearlyRecords.value.filter(record => record.date === date)
    const stats = statsOf(items)
    const intensity = items.length ? Math.min(4, Math.max(1, Math.ceil(Math.abs(stats.netFen) / max * 4))) : 0
    return { date, count: items.length, stats, intensity }
  })
})
const recentRecords = computed(() => [...yearlyRecords.value].sort((a, b) => `${b.date} ${b.time}`.localeCompare(`${a.date} ${a.time}`)).slice(0, 6))

/** 把所选比较周期转成完整日期范围。 */
function periodBounds(mode: 'month' | 'year', key: string): [string, string] {
  if (mode === 'year') return [`${key}-01-01`, `${key}-12-31`]
  const [year, month] = key.split('-').map(Number)
  return [`${key}-01`, `${key}-${String(new Date(year, month, 0).getDate()).padStart(2, '0')}`]
}

/** 返回符合周期键且通过当前筛选的正式记录。 */
function periodRecords(mode: 'month' | 'year', key: string): LedgerRecord[] {
  const [from, to] = periodBounds(mode, key)
  return completeRecords.value.filter(record => record.date >= from && record.date <= to && matchesFilters(record))
}

const compareStatsA = computed(() => statsOf(periodRecords(compareMode.value, periodA.value)))
const compareStatsB = computed(() => statsOf(periodRecords(compareMode.value, periodB.value)))
const compareDelta = computed(() => ({
  income: compareStatsA.value.incomeFen - compareStatsB.value.incomeFen,
  expense: compareStatsA.value.expenseFen - compareStatsB.value.expenseFen,
  net: compareStatsA.value.netFen - compareStatsB.value.netFen,
  count: compareStatsA.value.count - compareStatsB.value.count,
}))

const editorRecord = computed(() => records.value.find(record => record.business_id === editorID.value) ?? null)
const editorDirty = computed(() => Boolean(editorID.value && editorSequence.value > editorSavedSequence.value))
const canComplete = computed(() => {
  const record = editorRecord.value
  const amount = amountToFen(fields.amount)
  const valid = Boolean(amount !== null && isValidDate(fields.date) && (!fields.time || /^([01]\d|2[0-3]):[0-5]\d$/.test(fields.time)) && Array.from(fields.category).length <= 64 && Array.from(fields.note).length <= 2000)
  const changedOrDraft = Boolean(record && (record.status === 'draft' || record.draft !== null || editorDirty.value))
  return Boolean(valid && changedOrDraft && saveState.value === 'saved' && !editorDirty.value)
})

/** 记录筛选或月份变化后读取所需年份，分析周期变化后补齐比较年份。 */
watch(activeYear, year => {
  loading.value = true
  void loadYear(year).then(() => { loadError.value = false }).catch(() => { loadError.value = true }).finally(() => { loading.value = false })
})
watch([compareMode, periodA, periodB], ([mode, a, b]) => {
  const yearA = Number(mode === 'month' ? a.slice(0, 4) : a)
  const yearB = Number(mode === 'month' ? b.slice(0, 4) : b)
  for (const year of new Set([yearA, yearB])) if (Number.isInteger(year) && year >= 1 && year <= 9999) void loadYear(year).catch(() => { loadError.value = true })
})

/** 表单任一字段变化后 400ms 合并为一份完整草稿快照。 */
watch(() => JSON.stringify(fields), () => {
  if (!hydrating.value && editorID.value) scheduleAutoSave()
}, { flush: 'post' })

/** 在首次进入时读取当前年份及默认对比年份。 */
onMounted(async () => {
  loading.value = true
  try {
    await loadAllRecords()
    loadError.value = false
  } catch { loadError.value = true }
  finally { loading.value = false }
})

/** 页面切换前提交当前编辑快照，保存失败时留在页面供用户重试。 */
onBeforeRouteLeave(async () => {
  if (!editorID.value) return true
  try { await flushEditor(); return true }
  catch { return false }
})

onBeforeUnmount(() => { if (saveTimer !== undefined) window.clearTimeout(saveTimer) })

/** 返回本地化货币文本。 */
function money(fen: number): string { return formatFen(fen, locale.value) }

/** 返回当前语言的日期和月份标题。 */
function dateLabel(date: string, options: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric' }): string {
  return new Intl.DateTimeFormat(locale.value, options).format(fromDateKey(date))
}
function monthLabel(key: string): string {
  const [year, month] = key.split('-').map(Number)
  return new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: 'long' }).format(new Date(year, month - 1, 1))
}

/** 取选中日期当日的收入、支出与净额。 */
function dayStats(date: string) { return statsOf(dateRecords(date)) }

/** 返回完成记录的支付方式、标记及收支本地化文案。 */
function methodLabel(method: string): string {
  const known = ['wechat', 'alipay', 'cash', 'bank_card', 'other']
  return known.includes(method) ? t(`record.methods.${method === 'bank_card' ? 'bankCard' : method}`) : method
}
function flagLabel(flag: string): string {
  if (flag === 'important') return t('record.important')
  if (flag === 'anomaly') return t('record.anomaly')
  if (flag === 'pending_check') return t('record.pendingCheck')
  return t('record.ordinary')
}

/** 将日期光标切换到相邻月份。 */
function shiftMonth(delta: number): void {
  const [year, month] = activeMonth.value.split('-').map(Number)
  const date = new Date(year, month - 1 + delta, 1)
  activeMonth.value = toMonthKey(date)
  selectedDate.value = `${activeMonth.value}-01`
}
function jumpToday(): void {
  activeMonth.value = toMonthKey(new Date())
  selectedDate.value = toDateKey(new Date())
}

/** 打开日期详情抽屉。 */
function openDay(date: string): void {
  const nextMonth = toMonthKey(fromDateKey(date))
  if (nextMonth !== activeMonth.value) activeMonth.value = nextMonth
  selectedDate.value = date
  dayPanelOpen.value = true
}

/** 载入已有记录的正式字段或待应用草稿，避免编辑期间覆盖用户输入。 */
async function openRecord(record: LedgerRecord): Promise<void> {
  if (editorID.value && editorID.value !== record.business_id) await closeEditor()
  dayPanelOpen.value = false
  editorID.value = record.business_id
  revisionByID.set(record.business_id, record.draft_version)
  latestSequence.set(record.business_id, 0)
  savedSequence.set(record.business_id, 0)
  editorSequence.value = 0
  editorSavedSequence.value = 0
  saveState.value = 'saved'
  hydrating.value = true
  Object.assign(fields, editableFields(record))
  editorOpen.value = true
  await nextTick()
  hydrating.value = false
}

/** 先在服务端创建空草稿，再打开编辑器，保证新记录能跨设备恢复。 */
async function createRecord(date = selectedDate.value, type: LedgerFields['type'] = 'income'): Promise<void> {
  try {
    const initial = emptyFields(date)
    initial.type = type
    const created = await api.createLedgerDraft(initial)
    upsertRecord(created)
    await openRecord(created)
  } catch { ElMessage.error(t('common.errors.requestFailed')) }
}

/** 对日期、时间和文本长度进行草稿接口前置校验。 */
function validDraft(fieldsToSave: LedgerFields): boolean {
  const amountIsPartial = fieldsToSave.amount === '' || /^\d{1,12}(?:\.\d{0,2})?$/.test(fieldsToSave.amount)
  return isValidDate(fieldsToSave.date)
    && (!fieldsToSave.time || /^([01]\d|2[0-3]):[0-5]\d$/.test(fieldsToSave.time))
    && amountIsPartial
    && Array.from(fieldsToSave.category).length <= 64
    && Array.from(fieldsToSave.note).length <= 2000
}

/** 启动防抖自动保存，并为新快照分配递增的客户端序号。 */
function scheduleAutoSave(): void {
  if (!editorID.value) return
  const id = editorID.value
  const sequence = (latestSequence.get(id) ?? 0) + 1
  latestSequence.set(id, sequence)
  editorSequence.value = sequence
  saveState.value = validDraft({ ...fields }) ? 'saving' : 'error'
  if (saveTimer !== undefined) window.clearTimeout(saveTimer)
  if (!validDraft({ ...fields })) return
  const snapshot = { ...fields }
  saveTimer = window.setTimeout(() => { void persistSnapshot(id, sequence, snapshot).catch(() => undefined) }, 400)
}

/** 每條記錄串行保存，以服務端修訂號拒絕較舊請求覆蓋新內容。 */
function persistSnapshot(id: string, sequence: number, snapshot: LedgerFields): Promise<void> {
  const preceding = saveQueues.get(id) ?? Promise.resolve()
  const request = preceding.catch(() => undefined).then(async () => {
    if (sequence <= (savedSequence.get(id) ?? 0)) return
    const revision = revisionByID.get(id) ?? records.value.find(item => item.business_id === id)?.draft_version ?? 0
    const saved = await api.saveLedgerDraft(id, revision, snapshot)
    upsertRecord(saved)
    savedSequence.set(id, Math.max(savedSequence.get(id) ?? 0, sequence))
    if (editorID.value === id) editorSavedSequence.value = Math.max(editorSavedSequence.value, sequence)
    if (editorID.value === id && latestSequence.get(id) === sequence) saveState.value = 'saved'
  })
  saveQueues.set(id, request.then(() => undefined, () => undefined))
  return request.catch(cause => {
    if (editorID.value === id && latestSequence.get(id) === sequence) saveState.value = 'error'
    throw cause
  })
}

/** 提交当前未保存版本；完成记录或离开页面前会等待该 Promise。 */
async function flushEditor(): Promise<void> {
  const id = editorID.value
  if (!id) return
  if (saveTimer !== undefined) { window.clearTimeout(saveTimer); saveTimer = undefined }
  const sequence = latestSequence.get(id) ?? 0
  if (sequence <= (savedSequence.get(id) ?? 0)) return
  await persistSnapshot(id, sequence, { ...fields })
}

/** 用户明确点击重试时先同步服务端修订号，再保存仍留在表单中的快照。 */
async function retryAutoSave(): Promise<void> {
  const id = editorID.value
  if (!id || !validDraft({ ...fields })) return
  if (saveTimer !== undefined) { window.clearTimeout(saveTimer); saveTimer = undefined }
  saveState.value = 'saving'
  try {
    await (saveQueues.get(id) ?? Promise.resolve()).catch(() => undefined)
    const result = await api.ledgerRecords('0001-01-01', '9999-12-31')
    const current = result.items.find(item => item.business_id === id)
    if (!current) throw new Error('ledger_record_missing')
    upsertRecord(current)
    const snapshot = { ...fields }
    const sequence = (latestSequence.get(id) ?? 0) + 1
    latestSequence.set(id, sequence)
    editorSequence.value = sequence
    await persistSnapshot(id, sequence, snapshot)
  } catch { saveState.value = 'error' }
}

/** 暫存並關閉表單；若保存失敗則保持開啟，避免丟失本機輸入。 */
async function closeEditor(): Promise<void> {
  try { await flushEditor() }
  catch { saveState.value = 'error'; return }
  editorOpen.value = false
  editorID.value = null
  saveState.value = 'saved'
}

/** 將有效草稿原子應用為正式記錄，成功後才更新月曆與分析。 */
async function completeEditor(): Promise<void> {
  const id = editorID.value
  if (!id || !canComplete.value) return
  try {
    await flushEditor()
    const revision = revisionByID.get(id) ?? editorRecord.value?.draft_version ?? 0
    const completed = await api.completeLedgerRecord(id, revision)
    upsertRecord(completed)
    editorOpen.value = false
    editorID.value = null
    ElMessage.success(t('actions.complete'))
  } catch { saveState.value = 'error'; ElMessage.error(t('saveFailed')) }
}

/** 删除前确认，确认后清除排队保存并立即删除服务端记录。 */
async function deleteRecord(record: LedgerRecord): Promise<void> {
  try {
    await ElMessageBox.confirm(t('record.confirmDelete'), t('record.confirmDeleteTitle'), {
      confirmButtonText: t('actions.delete'), cancelButtonText: t('actions.cancel'), type: 'warning',
    })
  } catch { return }
  savingDelete.value = true
  try {
    if (saveTimer !== undefined && editorID.value === record.business_id) { window.clearTimeout(saveTimer); saveTimer = undefined }
    await (saveQueues.get(record.business_id) ?? Promise.resolve()).catch(() => undefined)
    await api.deleteLedgerRecord(record.business_id)
    records.value = records.value.filter(item => item.business_id !== record.business_id)
    if (editorID.value === record.business_id) { editorOpen.value = false; editorID.value = null }
    ElMessage.success(t('actions.delete'))
  } catch { ElMessage.error(t('common.errors.requestFailed')) }
  finally { savingDelete.value = false }
}

/** 按当天收入支出模板复制为新草稿，保留时间、分类、方式和标记并清空金额备注。 */
async function copyYesterday(): Promise<void> {
  const yesterday = fromDateKey(selectedDate.value)
  yesterday.setDate(yesterday.getDate() - 1)
  const sourceDate = toDateKey(yesterday)
  const sources = completeRecords.value.filter(record => record.date === sourceDate)
  if (!sources.length) { ElMessage.info(t('record.copyEmpty')); return }
  let created = 0
  for (const source of sources) {
    const draft: LedgerFields = { ...editableFields(source), date: selectedDate.value, amount: '', note: '' }
    try { upsertRecord(await api.createLedgerDraft(draft)); created += 1 }
    catch { ElMessage.error(t('common.errors.requestFailed')); break }
  }
  if (created) ElMessage.success(t('record.copyDone', { count: created }))
}

/** 经系统面板或浏览器导出 JSON，业务 UUID 支持后续导入更新本人记录。 */
async function exportJSON(): Promise<void> {
  try {
    const all = await loadAllRecords()
    const completed = all.filter(item => item.status === 'complete').map(exportItem)
    const result = await exportFile(new Blob([JSON.stringify({ formatVersion: 1, currency: 'CNY', records: completed }, null, 2)], { type: 'application/json' }), `ledger-${toDateKey(new Date())}.json`)
    if (result === 'cancelled') return
    ElMessage.info(t('export.draftsExcluded'))
  } catch (error) { if (!(error instanceof FileExportError)) ElMessage.error(t('export.failed')) }
}

/** 经系统面板或浏览器导出可重复导入的 UTF-8 CSV 文件。 */
async function exportCSV(): Promise<void> {
  try {
    const all = await loadAllRecords()
    const completed = all.filter(item => item.status === 'complete')
    const header = ['business_id', 'date', 'time', 'type', 'amount', 'category', 'payment_method', 'flag', 'note']
    const rows = completed.map(item => exportItem(item))
    const csv = [header.join(','), ...rows.map(row => header.map(key => csvCell(String(row[key as keyof typeof row] ?? ''))).join(','))].join('\r\n')
    const result = await exportFile(new Blob([`\uFEFF${csv}`], { type: 'text/csv;charset=utf-8' }), `ledger-${toDateKey(new Date())}.csv`)
    if (result === 'cancelled') return
    ElMessage.info(t('export.draftsExcluded'))
  } catch (error) { if (!(error instanceof FileExportError)) ElMessage.error(t('export.failed')) }
}

/** 为已完成记录生成不含内部数据库 ID 或用户标识的导出字段。 */
function exportItem(record: LedgerRecord) {
  return {
    business_id: record.business_id, date: record.date, time: record.time, type: record.type,
    amount: record.amount, category: record.category, payment_method: record.payment_method,
    flag: record.flag, note: record.note,
  }
}

/** 导出报告使用固定的淡蓝白主题；不读取界面暗色变量，收支仍保留业务状态色。 */
const reportColors = {
  page: '#f3f7fe', surface: '#ffffff', wash: '#e5edff', line: '#dbe6f5',
  ink: '#15243b', inkSoft: '#475b76', muted: '#61738c',
  accent: '#2563eb', secondary: '#8eaff0', income: '#147a56', expense: '#c2410c',
} as const

/** 使用 Canvas 生成独立月报图片，便于保存和分享。 */
function exportMonthImage(): void {
  const canvas = document.createElement('canvas')
  canvas.width = 1200
  canvas.height = 1400
  const context = canvas.getContext('2d')
  if (!context) { ElMessage.error(t('export.failed')); return }
  paintReportBackground(context, canvas.width, canvas.height)
  context.fillStyle = reportColors.ink
  context.font = '700 48px sans-serif'
  context.fillText(`${monthLabel(activeMonth.value)} · ${t('title')}`, 78, 110)
  drawReportSummary(context, 78, 170, monthStats.value)
  drawReportTrend(context, 78, 470, 1044, 300, dailyTrend.value)
  context.fillStyle = reportColors.ink
  context.font = '600 28px sans-serif'
  context.fillText(t('analysis.categories'), 78, 850)
  drawReportGroups(context, 78, 900, categoryTotals.value.map(row => ({ label: row.key, value: row.incomeFen + row.expenseFen })))
  context.fillStyle = reportColors.muted
  context.font = '20px sans-serif'
  context.fillText(t('export.draftsExcluded'), 78, 1320)
  downloadCanvas(canvas, `ledger-${activeMonth.value}.png`)
}

/** 导出两个所选期间的收入、支出与净额对比图。 */
function exportCompareImage(): void {
  const canvas = document.createElement('canvas')
  canvas.width = 1200
  canvas.height = 900
  const context = canvas.getContext('2d')
  if (!context) { ElMessage.error(t('export.failed')); return }
  paintReportBackground(context, canvas.width, canvas.height)
  context.fillStyle = reportColors.ink
  context.font = '700 46px sans-serif'
  context.fillText(t('analysis.comparison'), 78, 110)
  const rows = [
    { label: t('summary.income'), a: compareStatsA.value.incomeFen, b: compareStatsB.value.incomeFen },
    { label: t('summary.expense'), a: compareStatsA.value.expenseFen, b: compareStatsB.value.expenseFen },
    { label: t('summary.net'), a: compareStatsA.value.netFen, b: compareStatsB.value.netFen },
  ]
  context.font = '600 26px sans-serif'
  context.fillText(periodLabel(periodA.value), 390, 190)
  context.fillText(periodLabel(periodB.value), 780, 190)
  rows.forEach((row, index) => {
    const y = 300 + index * 165
    context.fillStyle = reportColors.inkSoft
    context.fillText(row.label, 80, y)
    context.fillStyle = reportColors.accent
    context.fillText(money(row.a), 390, y)
    context.fillStyle = reportColors.inkSoft
    context.fillText(money(row.b), 780, y)
    context.fillStyle = reportColors.line
    roundRect(context, 80, y + 28, 1040, 46, 18)
    const max = Math.max(1, row.a, row.b)
    context.fillStyle = reportColors.accent
    roundRect(context, 80, y + 28, Math.max(6, 1040 * row.a / max), 46, 18)
    context.fillStyle = reportColors.secondary
    roundRect(context, 80, y + 83, Math.max(6, 1040 * row.b / max), 26, 13)
  })
  context.fillStyle = reportColors.muted
  context.font = '20px sans-serif'
  context.fillText(t('export.draftsExcluded'), 80, 830)
  downloadCanvas(canvas, `ledger-compare-${periodA.value}-${periodB.value}.png`)
}

/** 绘制报告画布的底色和装饰分区。 */
function paintReportBackground(context: CanvasRenderingContext2D, width: number, height: number): void {
  context.fillStyle = reportColors.page
  context.fillRect(0, 0, width, height)
  context.fillStyle = reportColors.wash
  context.beginPath(); context.arc(width - 80, 40, 240, 0, Math.PI * 2); context.fill()
}

/** 绘制收入、支出与净额摘要卡片。 */
function drawReportSummary(context: CanvasRenderingContext2D, x: number, y: number, stats: ReturnType<typeof statsOf>): void {
  const cards = [
    { label: t('summary.income'), value: stats.incomeFen, color: reportColors.income },
    { label: t('summary.expense'), value: stats.expenseFen, color: reportColors.expense },
    { label: t('summary.net'), value: stats.netFen, color: reportColors.ink },
  ]
  cards.forEach((card, index) => {
    const left = x + index * 350
    context.fillStyle = reportColors.surface; roundRect(context, left, y, 320, 210, 24)
    context.fillStyle = reportColors.muted; context.font = '24px sans-serif'; context.fillText(card.label, left + 26, y + 52)
    context.fillStyle = card.color; context.font = '700 36px sans-serif'; context.fillText(money(card.value), left + 26, y + 124)
    context.fillStyle = reportColors.muted; context.font = '20px sans-serif'; context.fillText(t('summary.count', { count: stats.count }), left + 26, y + 170)
  })
}

/** 绘制按日净额柱状趋势。 */
function drawReportTrend(context: CanvasRenderingContext2D, x: number, y: number, width: number, height: number, points: typeof dailyTrend.value): void {
  context.fillStyle = reportColors.surface; roundRect(context, x, y, width, height, 24)
  context.fillStyle = reportColors.ink; context.font = '600 28px sans-serif'; context.fillText(t('analysis.trend'), x + 28, y + 52)
  const max = Math.max(1, ...points.map(point => Math.abs(point.netFen)))
  const chartTop = y + 100
  const chartHeight = height - 150
  const barWidth = width / Math.max(1, points.length) * 0.62
  points.forEach((point, index) => {
    const amount = Math.abs(point.netFen)
    const barHeight = Math.max(4, chartHeight * amount / max)
    context.fillStyle = point.netFen < 0 ? reportColors.expense : reportColors.income
    const barX = x + index * width / points.length + (width / points.length - barWidth) / 2
    roundRect(context, barX, chartTop + chartHeight - barHeight, barWidth, barHeight, Math.min(8, barWidth / 2))
  })
}

/** 绘制分类排行的金额和名称。 */
function drawReportGroups(context: CanvasRenderingContext2D, x: number, y: number, items: Array<{ label: string; value: number }>): void {
  const visible = items.slice(0, 8)
  const max = Math.max(1, ...visible.map(item => item.value))
  visible.forEach((item, index) => {
    const top = y + index * 48
    context.fillStyle = reportColors.inkSoft; context.font = '21px sans-serif'; context.fillText(item.label.slice(0, 22), x, top + 22)
    context.fillStyle = reportColors.line; roundRect(context, x + 300, top, 570, 18, 9)
    context.fillStyle = reportColors.accent; roundRect(context, x + 300, top, Math.max(8, 570 * item.value / max), 18, 9)
    context.fillStyle = reportColors.inkSoft; context.fillText(money(item.value), x + 900, top + 22)
  })
}

/** 以圆角矩形画图 API 兼容环境提供的路径方法。 */
function roundRect(context: CanvasRenderingContext2D, x: number, y: number, width: number, height: number, radius: number): void {
  context.beginPath()
  context.roundRect(x, y, width, height, Math.min(radius, height / 2, width / 2))
  context.fill()
}

/** PNG 经平台适配层导出；原生面板取消不作为失败，失败反馈由统一入口处理。 */
function downloadCanvas(canvas: HTMLCanvasElement, filename: string): void {
  canvas.toBlob(blob => {
    if (!blob) { ElMessage.error(t('export.failed')); return }
    void exportFile(blob, filename).catch(() => {})
  }, 'image/png')
}

/** 根据语言格式化比较周期标题。 */
function periodLabel(key: string): string { return compareMode.value === 'month' ? monthLabel(key) : key }

/** 切换月度/年度对比并使用当前周期与其前一期作为默认值。 */
function changeCompareMode(mode: 'month' | 'year'): void {
  if (mode === compareMode.value) return
  compareMode.value = mode
  if (mode === 'year') {
    periodA.value = String(activeYear.value)
    periodB.value = String(activeYear.value - 1)
  } else {
    const [year, month] = activeMonth.value.split('-').map(Number)
    periodA.value = activeMonth.value
    periodB.value = toMonthKey(new Date(year, month - 2, 1))
  }
}

/** 选择 JSON 或 CSV 文件，转换为预览记录并统计潜在重复。 */
async function chooseImportFile(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  try {
    const content = await file.text()
    const parsed = file.name.toLowerCase().endsWith('.csv') ? parseLedgerCSV(content) : parseLedgerJSON(content)
    importRecords.value = parsed.records.slice(0, 1000)
    importInvalidCount.value = parsed.invalid.length + Math.max(0, parsed.records.length - 1000)
    importTooMany.value = parsed.records.length > 1000
    if (!importRecords.value.length) { ElMessage.info(t('import.empty')); return }
    await loadAllRecords()
    importOpen.value = true
  } catch { ElMessage.error(t('import.failed')) }
}

const matchedImportCount = computed(() => importRecords.value.filter(item => item.business_id && records.value.some(record => record.business_id === item.business_id)).length)
const duplicateImportCount = computed(() => {
  const knownIDs = new Set(importRecords.value.map(item => item.business_id).filter(Boolean))
  const fingerprints = new Set(records.value.filter(record => record.status === 'complete' && !knownIDs.has(record.business_id)).map(record => ledgerFingerprint(record)))
  return importRecords.value.filter(item => !item.business_id || !records.value.some(record => record.business_id === item.business_id)).filter(item => fingerprints.has(ledgerFingerprint(item))).length
})

/** 将预览中的有效记录事务式合并到当前用户账本。 */
async function confirmImport(): Promise<void> {
  if (!importRecords.value.length || importTooMany.value) return
  importBusy.value = true
  try {
    const result = await api.importLedgerRecords(importRecords.value)
    await loadAllRecords()
    importOpen.value = false
    ElMessage.success(t('import.done', { created: result.created, updated: result.updated }))
  } catch { ElMessage.error(t('import.failed')) }
  finally { importBusy.value = false }
}

/** 清除所有已选筛选条件。 */
function resetFilters(): void {
  searchText.value = ''
  typeFilter.value = ''
  categoryFilter.value = ''
  methodFilter.value = ''
  flagFilter.value = ''
  minimumAmount.value = ''
}

/** 原生月份输入允许清空；只接受有效月份，避免日历和历史分析进入无效日期状态。 */
function chooseMonth(event: Event): void {
  const value = (event.target as HTMLInputElement).value
  if (/^\d{4}-(0[1-9]|1[0-2])$/.test(value) && Number(value.slice(0, 4)) > 0) activeMonth.value = value
}

/** 校验日期键是否为真实公历日期。 */
function isValidDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  return toDateKey(fromDateKey(value)) === value
}

/** 使用净额百分比为月度与周几图表计算视觉宽度。 */
function barPercent(value: number, max: number): string { return `${Math.max(value ? 4 : 0, Math.round(Math.abs(value) / max * 100))}%` }

/** 以本地化日期和结余内容提示年度热力图单元格。 */
function heatTitle(day: typeof yearlyHeat.value[number]): string {
  return `${dateLabel(day.date, { year: 'numeric', month: 'short', day: 'numeric' })} · ${money(day.stats.netFen)}`
}
</script>

<template>
  <div class="workspace-page ledger-page">
    <AppHeader />
    <main class="ledger-main">
      <section class="ledger-hero">
        <div class="hero-copy">
          <p class="eyebrow">{{ t('common.product') }}</p>
          <h1>{{ t('title') }}</h1>
          <p>{{ t('subtitle') }}</p>
        </div>
        <div class="hero-actions">
          <el-button class="soft-button" @click="fileInput?.click()">{{ t('actions.import') }}</el-button>
          <input ref="fileInput" class="visually-hidden" type="file" accept=".json,.csv,application/json,text/csv" @change="chooseImportFile">
          <el-dropdown trigger="click">
            <el-button class="soft-button">{{ t('actions.exportJson') }}<el-icon class="dropdown-caret" aria-hidden="true"><ArrowDown /></el-icon></el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item @click="exportJSON">{{ t('actions.exportJson') }}</el-dropdown-item>
                <el-dropdown-item @click="exportCSV">{{ t('actions.exportCsv') }}</el-dropdown-item>
                <el-dropdown-item @click="exportMonthImage">{{ t('actions.exportMonth') }}</el-dropdown-item>
                <el-dropdown-item @click="exportCompareImage">{{ t('actions.exportCompare') }}</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
          <el-button type="primary" class="ledger-primary" @click="createRecord(selectedDate)"><el-icon aria-hidden="true"><Plus /></el-icon>{{ t('actions.add') }}</el-button>
        </div>
      </section>

      <section class="ledger-kpis" :aria-label="t('summary.count')">
        <article class="kpi-card income-kpi"><span>{{ t('summary.income') }}</span><strong>{{ money(monthStats.incomeFen) }}</strong><el-icon class="kpi-trend" aria-hidden="true"><TopRight /></el-icon></article>
        <article class="kpi-card expense-kpi"><span>{{ t('summary.expense') }}</span><strong>{{ money(monthStats.expenseFen) }}</strong><el-icon class="kpi-trend" aria-hidden="true"><BottomRight /></el-icon></article>
        <article class="kpi-card net-kpi"><span>{{ t('summary.net') }}</span><strong :class="monthStats.netFen < 0 ? 'negative' : ''">{{ money(monthStats.netFen) }}</strong><small>{{ t('summary.average') }} · {{ money(monthStats.averageFen) }}</small></article>
        <article class="kpi-card count-kpi"><span>{{ t('summary.count') }}</span><strong>{{ monthStats.count }}</strong><small>{{ t('summary.days') }} · {{ monthStats.activeDays }}</small></article>
      </section>

      <section v-if="pendingRecords.length" class="draft-banner">
        <div class="draft-banner-icon" aria-hidden="true"><el-icon><EditPen /></el-icon></div>
        <div><strong>{{ t('draftCount', { count: pendingRecords.length }) }}</strong><span>{{ t('drafts') }}</span></div>
        <div class="draft-banner-items">
          <button v-for="record in pendingRecords.slice(0, 4)" :key="record.business_id" type="button" @click="openRecord(record)">
            {{ dateLabel(record.status === 'draft' && record.draft ? record.draft.date : record.date) }} <span>{{ record.status === 'draft' ? t('record.draft') : t('record.edit') }}</span>
          </button>
        </div>
      </section>

      <section class="filter-panel">
        <div class="filter-title"><div><span class="section-kicker">{{ t('filters.title') }}</span><small>{{ t('calendar.records', { count: selectedMonthRecords.length }) }}</small></div><button type="button" class="text-action" @click="resetFilters">{{ t('filters.reset') }}</button></div>
        <div class="filter-controls">
          <el-input v-model="searchText" clearable :aria-label="t('filters.search')" :placeholder="t('filters.search')" class="search-filter"><template #prefix><el-icon aria-hidden="true"><Search /></el-icon></template></el-input>
          <el-select v-model="typeFilter" :aria-label="t('filters.allTypes')" :placeholder="t('filters.allTypes')" clearable><el-option :label="t('filters.income')" value="income" /><el-option :label="t('filters.expense')" value="expense" /></el-select>
          <el-select v-model="categoryFilter" :aria-label="t('filters.allCategories')" :placeholder="t('filters.allCategories')" clearable><el-option v-for="category in categories" :key="category" :label="category" :value="category" /></el-select>
          <el-select v-model="methodFilter" :aria-label="t('filters.allMethods')" :placeholder="t('filters.allMethods')" clearable><el-option v-for="method in methods" :key="method" :label="methodLabel(method)" :value="method" /></el-select>
          <el-select v-model="flagFilter" :aria-label="t('filters.allFlags')" :placeholder="t('filters.allFlags')" clearable><el-option :label="t('record.important')" value="important" /><el-option :label="t('record.anomaly')" value="anomaly" /><el-option :label="t('record.pendingCheck')" value="pending_check" /></el-select>
          <el-input v-model="minimumAmount" inputmode="decimal" :aria-label="t('filters.minAmount')" :placeholder="t('filters.minAmount')" class="minimum-filter" />
        </div>
      </section>

      <div v-if="loadError" class="ledger-error" role="alert"><span>{{ t('loadError') }}</span><el-button text @click="loadYear(activeYear).then(() => { loadError = false }).catch(() => { loadError = true })">{{ t('retry') }}</el-button></div>
      <div v-if="loading && !records.length" class="ledger-loading" role="status" aria-live="polite"><span class="loading-dot"></span>{{ t('loading') }}</div>

      <section class="calendar-section">
        <div class="calendar-heading">
          <div class="month-heading"><button class="icon-button" type="button" :aria-label="t('calendar.previous')" :title="t('calendar.previous')" @click="shiftMonth(-1)"><el-icon aria-hidden="true"><ArrowLeft /></el-icon></button><h2>{{ monthLabel(activeMonth) }}</h2><button class="icon-button" type="button" :aria-label="t('calendar.next')" :title="t('calendar.next')" @click="shiftMonth(1)"><el-icon aria-hidden="true"><ArrowRight /></el-icon></button><button class="today-button" type="button" @click="jumpToday">{{ t('calendar.today') }}</button></div>
          <div class="calendar-heading-actions"><input :value="activeMonth" @change="chooseMonth" type="month" :aria-label="t('calendar.month')" class="month-picker"><span class="month-record-count">{{ t('calendar.records', { count: selectedMonthRecords.length }) }}</span><el-button class="soft-button copy-button" @click="copyYesterday">{{ t('record.copyYesterday') }}</el-button></div>
        </div>

        <div class="weekday-row" aria-hidden="true"><span v-for="(name, index) in weekdayNames" :key="index">{{ name }}</span></div>
        <div class="calendar-grid">
          <button v-for="cell in calendarCells" :key="cell.key" type="button" class="calendar-cell" :aria-pressed="cell.key === selectedDate" :aria-current="cell.key === toDateKey(new Date()) ? 'date' : undefined" :aria-label="`${dateLabel(cell.key, { year: 'numeric', month: 'long', day: 'numeric' })} · ${t('calendar.records', { count: dateRecords(cell.key).length })}`" aria-haspopup="dialog" :class="{ 'outside-month': !cell.inMonth, 'is-today': cell.key === toDateKey(new Date()), 'is-selected': cell.key === selectedDate }" @click="openDay(cell.key)">
            <span class="cell-date">{{ cell.number }}</span>
            <span v-if="dateRecords(cell.key).length" class="cell-count">{{ dateRecords(cell.key).length }}</span>
            <span v-if="dayStats(cell.key).incomeFen" class="cell-income">+{{ money(dayStats(cell.key).incomeFen) }}</span>
            <span v-if="dayStats(cell.key).expenseFen" class="cell-expense">−{{ money(dayStats(cell.key).expenseFen) }}</span>
            <span v-if="dateRecords(cell.key).length" class="cell-net" :class="{ negative: dayStats(cell.key).netFen < 0 }">={{ money(dayStats(cell.key).netFen) }}</span>
            <span class="cell-records"><span v-for="record in dateRecords(cell.key).slice(0, 3)" :key="record.business_id" class="cell-record" :title="`${record.time} ${record.category} ${record.note}`"><span>{{ record.note || record.category || methodLabel(record.payment_method) }}</span><b :class="record.type">{{ record.type === 'income' ? '+' : '−' }}{{ money(amountToFen(record.amount) ?? 0) }}</b></span></span>
            <span v-if="dateRecords(cell.key).length > 3" class="cell-more">{{ t('calendar.more', { count: dateRecords(cell.key).length - 3 }) }}</span>
            <span v-if="dateRecords(cell.key).length" class="cell-bar"><i :style="{ width: barPercent(dayStats(cell.key).incomeFen + dayStats(cell.key).expenseFen, calendarMaxFen) }" /></span>
            <el-icon class="cell-open" aria-hidden="true"><TopRight /></el-icon>
          </button>
        </div>
        <div v-if="!selectedMonthRecords.length && !loading" class="calendar-empty"><el-icon class="empty-orbit" aria-hidden="true"><Calendar /></el-icon><p>{{ t('calendar.noRecords') }}</p><el-button type="primary" class="ledger-primary" @click="createRecord(selectedDate)"><el-icon aria-hidden="true"><Plus /></el-icon>{{ t('actions.add') }}</el-button></div>

        <div class="mobile-agenda">
          <div class="agenda-header"><h3>{{ t('calendar.mobileAgenda') }}</h3><span>{{ agendaDays.length }}</span></div>
          <button v-for="day in agendaDays" :key="day.date" class="agenda-day" type="button" @click="openDay(day.date)">
            <span class="agenda-date"><strong>{{ dateLabel(day.date, { day: '2-digit' }) }}</strong><small>{{ dateLabel(day.date, { weekday: 'short', month: 'short' }) }}</small></span>
            <span class="agenda-lines"><span v-for="record in day.records.slice(0, 2)" :key="record.business_id"><i :class="record.type">{{ record.type === 'income' ? '+' : '−' }}</i>{{ record.category || methodLabel(record.payment_method) }}</span><small v-if="day.records.length > 2">{{ t('calendar.records', { count: day.records.length }) }}</small></span>
            <span class="agenda-net" :class="{ negative: day.stats.netFen < 0 }">{{ money(day.stats.netFen) }}</span><el-icon class="agenda-arrow" aria-hidden="true"><ArrowRight /></el-icon>
          </button>
          <p v-if="!agendaDays.length" class="agenda-empty">{{ t('calendar.noRecords') }}</p>
        </div>
      </section>

      <section class="analysis-section">
        <div class="analysis-heading"><div><p class="eyebrow">{{ t('title') }}</p><h2>{{ t('analysis.title') }}</h2></div><span class="analysis-period">{{ monthLabel(activeMonth) }}</span></div>
        <div class="analysis-grid">
          <article class="analysis-card trend-card"><div class="card-heading"><h3>{{ t('analysis.trend') }}</h3><span>{{ t('summary.net') }}</span></div><div class="trend-chart" role="img" :aria-label="t('analysis.trend')"><div v-for="point in dailyTrend" :key="point.date" class="trend-column" :title="`${point.day} · ${money(point.netFen)}`"><span class="trend-bar" :class="{ loss: point.netFen < 0 }" :style="{ height: barPercent(point.netFen, Math.max(1, ...dailyTrend.map(item => Math.abs(item.netFen)))) }" /><small>{{ point.day % 5 === 1 || point.day === dailyTrend.length ? point.day : '' }}</small></div></div><div class="trend-baseline" /></article>
          <article class="analysis-card weekday-card"><div class="card-heading"><h3>{{ t('analysis.weekday') }}</h3><span>{{ t('analytics.amount') }}</span></div><div class="weekday-chart"><div v-for="item in weekdayTotals" :key="item.weekday" class="weekday-column" :title="money(item.amount)"><div class="weekday-track"><i :style="{ height: barPercent(item.amount, weekdayMax) }" /></div><span>{{ weekdayNames[item.weekday] }}</span><small>{{ item.count }}</small></div></div></article>
          <article class="analysis-card group-card"><div class="card-heading"><h3>{{ t('analysis.categories') }}</h3><span>{{ categoryTotals.length }}</span></div><div v-if="categoryTotals.length" class="group-list"><div v-for="item in categoryTotals" :key="item.key" class="group-row"><span>{{ item.key }}</span><div class="group-track"><i :style="{ width: barPercent(item.incomeFen + item.expenseFen, Math.max(1, ...categoryTotals.map(row => row.incomeFen + row.expenseFen))) }" /></div><strong>{{ money(item.incomeFen - item.expenseFen) }}</strong><small>+{{ money(item.incomeFen) }} / −{{ money(item.expenseFen) }}</small></div></div><p v-else class="analysis-empty">{{ t('analysis.empty') }}</p></article>
          <article class="analysis-card group-card"><div class="card-heading"><h3>{{ t('analysis.methods') }}</h3><span>{{ methodTotals.length }}</span></div><div v-if="methodTotals.length" class="group-list"><div v-for="item in methodTotals" :key="item.key" class="group-row"><span>{{ methodLabel(item.key) }}</span><div class="group-track method-track"><i :style="{ width: barPercent(item.incomeFen + item.expenseFen, Math.max(1, ...methodTotals.map(row => row.incomeFen + row.expenseFen))) }" /></div><strong>{{ money(item.incomeFen - item.expenseFen) }}</strong><small>+{{ money(item.incomeFen) }} / −{{ money(item.expenseFen) }}</small></div></div><p v-else class="analysis-empty">{{ t('analysis.empty') }}</p></article>
        </div>

        <LedgerAnalytics :records="completeRecords.filter(matchesFilters)" :month="activeMonth" @month="activeMonth = $event" />

        <article class="analysis-card compare-card">
          <div class="card-heading compare-heading"><div><h3>{{ t('analysis.comparison') }}</h3><p>{{ t('analysis.insights') }} · {{ compareStatsA.netFen >= compareStatsB.netFen ? t('analysis.positive') : t('analysis.negative') }}</p></div><div class="compare-mode" role="group" :aria-label="t('analysis.comparison')"><button type="button" :aria-pressed="compareMode === 'month'" :class="{ active: compareMode === 'month' }" @click="changeCompareMode('month')">{{ t('analysis.monthMode') }}</button><button type="button" :aria-pressed="compareMode === 'year'" :class="{ active: compareMode === 'year' }" @click="changeCompareMode('year')">{{ t('analysis.yearMode') }}</button></div></div>
          <div class="compare-selectors"><label>{{ t('analysis.periodA') }}<input v-model="periodA" :type="compareMode === 'month' ? 'month' : 'number'" :min="compareMode === 'year' ? 1 : undefined" :max="compareMode === 'year' ? 9999 : undefined"></label><button class="swap-button" type="button" :aria-label="t('analysis.swap')" :title="t('analysis.swap')" @click="[periodA, periodB] = [periodB, periodA]"><el-icon aria-hidden="true"><Switch /></el-icon></button><label>{{ t('analysis.periodB') }}<input v-model="periodB" :type="compareMode === 'month' ? 'month' : 'number'" :min="compareMode === 'year' ? 1 : undefined" :max="compareMode === 'year' ? 9999 : undefined"></label></div>
          <div class="compare-table"><div class="compare-table-head"><span></span><span>{{ periodLabel(periodA) }}</span><span>{{ periodLabel(periodB) }}</span><span>Δ</span></div>
            <div class="compare-row"><span>{{ t('summary.income') }}</span><strong>{{ money(compareStatsA.incomeFen) }}</strong><strong>{{ money(compareStatsB.incomeFen) }}</strong><em :class="{ negative: compareDelta.income < 0 }">{{ money(compareDelta.income) }}</em></div>
            <div class="compare-row"><span>{{ t('summary.expense') }}</span><strong>{{ money(compareStatsA.expenseFen) }}</strong><strong>{{ money(compareStatsB.expenseFen) }}</strong><em :class="{ negative: compareDelta.expense > 0 }">{{ money(compareDelta.expense) }}</em></div>
            <div class="compare-row"><span>{{ t('summary.net') }}</span><strong>{{ money(compareStatsA.netFen) }}</strong><strong>{{ money(compareStatsB.netFen) }}</strong><em :class="{ negative: compareDelta.net < 0 }">{{ money(compareDelta.net) }}</em></div>
            <div class="compare-row"><span>{{ t('summary.count') }}</span><strong>{{ compareStatsA.count }}</strong><strong>{{ compareStatsB.count }}</strong><em>{{ compareDelta.count > 0 ? '+' : '' }}{{ compareDelta.count }}</em></div>
            <div v-for="metric in ['activeDays', 'averageFen', 'ticketFen', 'bestIncomeFen'] as const" :key="metric" class="compare-row"><span>{{ t(metric === 'activeDays' ? 'analysis.activeDays' : metric === 'ticketFen' ? 'analytics.averageTicket' : metric === 'bestIncomeFen' ? 'analytics.bestDay' : 'analysis.dailyAverage') }}</span><strong>{{ metric === 'activeDays' ? compareStatsA[metric] : money(compareStatsA[metric]) }}</strong><strong>{{ metric === 'activeDays' ? compareStatsB[metric] : money(compareStatsB[metric]) }}</strong><em>{{ metric === 'activeDays' ? compareStatsA[metric] - compareStatsB[metric] : money(compareStatsA[metric] - compareStatsB[metric]) }}</em></div>
          </div>
          <div class="insight-strip"><el-icon class="insight-spark" aria-hidden="true"><TrendCharts /></el-icon><p>{{ compareStatsA.incomeFen > compareStatsA.expenseFen ? t('analysis.higherIncome') : t('analysis.higherExpense') }} · {{ compareStatsA.netFen >= 0 ? t('analysis.positive') : t('analysis.negative') }}</p><small>{{ t('analysis.dailyAverage') }} · {{ money(compareStatsA.averageFen) }}</small></div>
        </article>

        <article class="analysis-card yearly-card"><div class="card-heading"><h3>{{ t('analysis.yearly') }}</h3><span>{{ activeYear }}</span></div><div class="yearly-chart"><button v-for="item in yearlyMonths" :key="item.month" type="button" class="year-month" :aria-pressed="`${activeYear}-${String(item.month).padStart(2, '0')}` === activeMonth" :aria-label="`${monthLabel(`${activeYear}-${String(item.month).padStart(2, '0')}`)} · ${money(item.stats.netFen)}`" :class="{ selected: `${activeYear}-${String(item.month).padStart(2, '0')}` === activeMonth }" :title="money(item.stats.netFen)" @click="activeMonth = `${activeYear}-${String(item.month).padStart(2, '0')}`"><span>{{ item.month }}</span><i :class="{ loss: item.stats.netFen < 0 }" :style="{ height: barPercent(item.stats.netFen, monthMaxAbsNet) }" /><small>{{ money(item.stats.netFen) }}</small></button></div></article>

        <article class="analysis-card heatmap-card"><div class="card-heading"><h3>{{ t('analysis.heatmap') }}</h3><span>{{ activeYear }}</span></div><div class="heatmap-scroll"><div class="heatmap-grid"><span v-for="day in yearlyHeat" :key="day.date" class="heat-cell" :class="[`level-${day.intensity}`, { loss: day.stats.netFen < 0 }]" :title="heatTitle(day)" /></div></div></article>

        <article class="analysis-card recent-card"><div class="card-heading"><h3>{{ t('analysis.recent') }}</h3><span>{{ recentRecords.length }}</span></div><div v-if="recentRecords.length" class="recent-list"><button v-for="record in recentRecords" :key="record.business_id" type="button" class="recent-row" @click="openRecord(record)"><span class="recent-type" :class="record.type">{{ record.type === 'income' ? '＋' : '−' }}</span><span class="recent-main"><strong>{{ record.category || t('record.ordinary') }}</strong><small>{{ dateLabel(record.date, { year: 'numeric', month: 'short', day: 'numeric' }) }} · {{ methodLabel(record.payment_method) }}</small></span><b :class="record.type">{{ record.type === 'expense' ? '−' : '+' }}{{ money(amountToFen(record.amount) ?? 0) }}</b></button></div><p v-else class="analysis-empty">{{ t('analysis.empty') }}</p></article>
      </section>

      <footer class="workspace-footer"><span>{{ t('common.brand') }}</span><span>{{ t('common.footer') }}</span></footer>
    </main>

    <div v-if="dayPanelOpen" class="sheet-backdrop" @click.self="dayPanelOpen = false">
      <section class="ledger-sheet day-sheet" role="dialog" aria-modal="true" :aria-label="t('record.dayTitle', { date: dateLabel(selectedDate, { year: 'numeric', month: 'long', day: 'numeric' }) })">
        <div class="sheet-handle" /><header class="sheet-heading"><div><p class="eyebrow">{{ dateLabel(selectedDate, { weekday: 'long' }) }}</p><h2>{{ t('record.dayTitle', { date: dateLabel(selectedDate, { year: 'numeric', month: 'long', day: 'numeric' }) }) }}</h2></div><button type="button" class="icon-button close-button" :aria-label="t('actions.close')" :title="t('actions.close')" @click="dayPanelOpen = false"><el-icon aria-hidden="true"><Close /></el-icon></button></header>
        <div class="day-summary"><span>{{ t('record.dayNet') }}</span><strong :class="{ negative: dayStats(selectedDate).netFen < 0 }">{{ money(dayStats(selectedDate).netFen) }}</strong><small>+{{ money(dayStats(selectedDate).incomeFen) }} / −{{ money(dayStats(selectedDate).expenseFen) }}</small></div>
        <div class="day-sheet-actions"><el-button type="primary" class="ledger-primary" @click="createRecord(selectedDate)">{{ t('record.addForDay') }}</el-button><el-button class="soft-button" @click="copyYesterday">{{ t('record.copyYesterday') }}</el-button></div>
        <div v-if="selectedDayRecords.length" class="sheet-record-list"><article v-for="record in selectedDayRecords" :key="record.business_id" class="sheet-record"><span class="recent-type" :class="record.type">{{ record.type === 'income' ? '＋' : '−' }}</span><div class="sheet-record-main"><strong>{{ record.category || t('record.ordinary') }}</strong><small>{{ record.time || '—' }} · {{ methodLabel(record.payment_method) }} · {{ flagLabel(record.flag) }}</small><p v-if="record.note">{{ record.note }}</p></div><strong class="sheet-record-amount" :class="record.type">{{ record.type === 'expense' ? '−' : '+' }}{{ money(amountToFen(record.amount) ?? 0) }}</strong><el-button text @click="openRecord(record)">{{ t('record.edit') }}</el-button></article></div>
        <p v-else class="sheet-empty">{{ t('calendar.noRecords') }}</p>
      </section>
    </div>

    <div v-if="editorOpen" class="sheet-backdrop" @click.self="closeEditor">
      <section class="ledger-sheet edit-sheet" role="dialog" aria-modal="true" :aria-label="editorRecord?.status === 'draft' ? t('record.newTitle') : t('record.editTitle')">
        <div class="sheet-handle" /><header class="sheet-heading"><div><p class="eyebrow">{{ editorRecord?.status === 'draft' ? t('record.draft') : t('record.edit') }}</p><h2>{{ editorRecord?.status === 'draft' ? t('record.newTitle') : t('record.editTitle') }}</h2></div><button type="button" class="icon-button close-button" :aria-label="t('actions.close')" :title="t('actions.close')" @click="closeEditor"><el-icon aria-hidden="true"><Close /></el-icon></button></header>
        <p class="editor-hint">{{ editorRecord?.status === 'draft' ? t('record.draftHint') : t('record.editHint') }}</p>
        <div class="save-status" :class="saveState" role="status" aria-live="polite"><i /><span>{{ saveState === 'saving' ? t('saving') : saveState === 'error' ? t('saveFailed') : t('saved') }}</span><button v-if="saveState === 'error'" type="button" :disabled="!validDraft(fields)" @click="retryAutoSave">{{ t('retrySave') }}</button></div>
        <form class="ledger-form" @submit.prevent="completeEditor">
          <label class="form-field"><span>{{ t('record.date') }}</span><input v-model="fields.date" required type="date"></label>
          <label class="form-field"><span>{{ t('record.time') }}</span><input v-model="fields.time" type="time"></label>
          <label class="form-field"><span>{{ t('record.type') }}</span><select v-model="fields.type"><option value="income">{{ t('filters.income') }}</option><option value="expense">{{ t('filters.expense') }}</option></select></label>
          <label class="form-field amount-field"><span>{{ t('record.amount') }}</span><div class="amount-input"><span>¥</span><input v-model="fields.amount" inputmode="decimal" autocomplete="off" :placeholder="t('record.amountPlaceholder')"></div><small v-if="fields.amount && !/^\d{1,12}(?:\.\d{1,2})?$/.test(fields.amount)">{{ t('record.validationAmount') }}</small></label>
          <label class="form-field"><span>{{ t('record.category') }}</span><input v-model="fields.category" list="ledger-category-options" maxlength="64" :placeholder="t('record.categoryPlaceholder')"><datalist id="ledger-category-options"><option v-for="category in categorySuggestions" :key="category" :value="category" /><option v-for="category in categories" :key="`saved-${category}`" :value="category" /></datalist></label>
          <label class="form-field"><span>{{ t('record.method') }}</span><select v-model="fields.payment_method"><option value="wechat">{{ t('record.methods.wechat') }}</option><option value="alipay">{{ t('record.methods.alipay') }}</option><option value="cash">{{ t('record.methods.cash') }}</option><option value="bank_card">{{ t('record.methods.bank_card') }}</option><option value="other">{{ t('record.methods.other') }}</option><option v-for="method in methods.filter(value => !['wechat','alipay','cash','bank_card','other'].includes(value))" :key="method" :value="method">{{ method }}</option></select></label>
          <label class="form-field"><span>{{ t('record.flag') }}</span><select v-model="fields.flag"><option value="">{{ t('record.ordinary') }}</option><option value="important">{{ t('record.important') }}</option><option value="anomaly">{{ t('record.anomaly') }}</option><option value="pending_check">{{ t('record.pendingCheck') }}</option></select></label>
          <label class="form-field note-field"><span>{{ t('record.note') }}</span><textarea v-model="fields.note" maxlength="2000" rows="4" :placeholder="t('record.notePlaceholder')" /></label>
          <div class="form-actions"><el-button v-if="editorRecord" class="delete-button" :loading="savingDelete" @click="deleteRecord(editorRecord)">{{ t('actions.delete') }}</el-button><span class="form-action-spacer" /><el-button class="soft-button" @click="closeEditor">{{ t('actions.close') }}</el-button><el-button type="primary" class="ledger-primary" :disabled="!canComplete" @click="completeEditor">{{ t('actions.complete') }}</el-button></div>
        </form>
      </section>
    </div>

    <div v-if="importOpen" class="sheet-backdrop import-backdrop" @click.self="importOpen = false">
      <section class="import-dialog" role="dialog" aria-modal="true" :aria-label="t('import.title')"><header class="sheet-heading"><div><p class="eyebrow">{{ t('actions.import') }}</p><h2>{{ t('import.title') }}</h2></div><button class="icon-button close-button" type="button" :aria-label="t('import.cancel')" :title="t('import.cancel')" @click="importOpen = false"><el-icon aria-hidden="true"><Close /></el-icon></button></header><p class="import-formats">{{ t('import.formats') }}</p><div class="import-review"><strong>{{ t('import.validCount', { count: importRecords.length }) }}</strong><p v-if="matchedImportCount">{{ t('import.updateCount', { count: matchedImportCount }) }}</p><p v-if="duplicateImportCount">{{ t('import.duplicateCount', { count: duplicateImportCount }) }}</p><p v-if="importInvalidCount">{{ t('import.invalidCount', { count: importInvalidCount }) }}</p><p v-if="importTooMany" class="import-warning">{{ t('import.tooMany') }}</p></div><div class="import-preview-list"><div v-for="(record, index) in importRecords.slice(0, 5)" :key="`${record.business_id || record.date}-${index}`"><span>{{ dateLabel(record.date, { year: 'numeric', month: 'short', day: 'numeric' }) }} · {{ record.category }}</span><strong>{{ record.type === 'income' ? '+' : '−' }}{{ money(amountToFen(record.amount) ?? 0) }}</strong></div></div><footer class="import-actions"><el-button class="soft-button" @click="importOpen = false">{{ t('import.cancel') }}</el-button><el-button type="primary" class="ledger-primary" :loading="importBusy" :disabled="importTooMany" @click="confirmImport">{{ t('import.confirm') }}</el-button></footer></section>
    </div>
  </div>
</template>

<style scoped>
/* 账本与其他页面共用语义主题；收入、支出和错误继续保留各自的状态色。 */
.ledger-page{color:var(--ink);background:var(--page);min-height:100vh}.ledger-main{width:min(1440px,calc(100% - 72px));margin:0 auto;padding:52px 0 28px}.ledger-hero,.hero-actions,.calendar-heading,.month-heading,.calendar-heading-actions,.filter-title,.analysis-heading,.card-heading,.sheet-heading,.form-actions,.import-actions{display:flex;align-items:center;justify-content:space-between}.ledger-hero{gap:24px;margin-bottom:28px}.hero-copy .eyebrow,.eyebrow{margin:0 0 7px;color:var(--muted);font-size:11px;font-weight:800;letter-spacing:.15em;text-transform:uppercase}.hero-copy h1{margin:0;font-size:clamp(30px,4vw,44px);letter-spacing:-.04em}.hero-copy p:last-child{margin:9px 0 0;color:var(--muted);font-size:15px}.hero-actions{gap:9px}.ledger-primary{border:0!important;background:var(--accent)!important;color:var(--on-accent)!important;min-height:42px!important;border-radius:var(--radius-control)!important;padding:0 17px!important;font-weight:700!important;box-shadow:0 8px 18px color-mix(in srgb,var(--accent) 18%,transparent)}.ledger-primary:hover:not(:disabled){background:var(--accent-hover)!important}.soft-button{min-height:40px!important;border-color:var(--line)!important;background:var(--surface)!important;color:var(--ink-soft)!important;border-radius:var(--radius-control)!important;font-weight:650!important}.soft-button:hover:not(:disabled){border-color:var(--accent)!important;color:var(--accent-hover)!important}.dropdown-caret{margin-left:8px;color:var(--muted)}.visually-hidden{position:absolute;left:0;top:0;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}
.ledger-kpis{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:13px;margin-bottom:15px}.kpi-card{position:relative;min-height:116px;padding:19px 21px;border:1px solid var(--line);border-radius:var(--radius-card);background:var(--surface);box-shadow:var(--soft-shadow)}.kpi-card>span{display:block;color:var(--muted);font-size:12px;font-weight:650}.kpi-card strong{display:block;margin-top:10px;font-size:clamp(20px,2vw,27px);letter-spacing:-.04em;white-space:nowrap}.kpi-card small{display:block;margin-top:7px;color:var(--muted);font-size:11px}.kpi-trend{position:absolute;top:19px;right:20px;width:28px;height:28px;border-radius:9px;display:grid;place-items:center;font-style:normal;font-size:14px}.income-kpi i{background:var(--success-soft);color:var(--success)}.expense-kpi i{background:var(--danger-soft);color:var(--danger)}.net-kpi i{display:none}.count-kpi i{display:none}.negative{color:var(--danger)!important}.draft-banner{display:flex;align-items:center;gap:13px;margin:14px 0;padding:14px 18px;border:1px solid var(--line);border-radius:var(--radius-card);background:var(--surface-soft)}.draft-banner-icon{width:34px;height:34px;border-radius:var(--radius-control);background:var(--accent-soft);color:var(--accent);display:grid;place-items:center}.draft-banner>div:nth-child(2){display:grid;gap:3px}.draft-banner>div:nth-child(2) strong{font-size:13px}.draft-banner>div:nth-child(2) span{font-size:11px;color:var(--muted)}.draft-banner-items{display:flex;gap:7px;margin-left:auto;overflow:auto}.draft-banner-items button{border:1px solid var(--line);background:var(--surface);color:var(--ink-soft);border-radius:var(--radius-control);padding:7px 10px;white-space:nowrap;cursor:pointer;font-size:12px}.draft-banner-items button span{color:var(--warning);margin-left:5px}
.filter-panel,.calendar-section,.analysis-card{border:1px solid var(--line);border-radius:var(--radius-card);background:var(--surface);box-shadow:var(--soft-shadow)}.filter-panel{margin:14px 0;padding:16px 18px}.filter-title{margin-bottom:12px}.filter-title>div{display:flex;align-items:center;gap:12px}.section-kicker{font-size:13px;font-weight:750}.filter-title small,.month-record-count{color:var(--muted);font-size:12px}.text-action{border:0;background:none;color:var(--accent);font-size:12px;font-weight:700;cursor:pointer}.filter-controls{display:grid;grid-template-columns:minmax(200px,1.6fr) repeat(4,minmax(120px,1fr)) minmax(128px,.8fr);gap:9px}.filter-controls :deep(.el-input__wrapper),.filter-controls :deep(.el-select__wrapper){min-height:39px;border-radius:var(--radius-control);box-shadow:0 0 0 1px var(--line) inset!important;background:var(--surface)}.filter-controls :deep(.el-input__inner){font-size:12px}.ledger-error{display:flex;justify-content:space-between;align-items:center;padding:11px 15px;border:1px solid color-mix(in srgb,var(--danger) 30%,var(--line));border-radius:12px;color:var(--danger);background:var(--danger-soft);margin:14px 0}.ledger-loading{display:flex;align-items:center;gap:10px;padding:18px;color:var(--muted)}
.calendar-section{margin-top:15px;padding:21px}.calendar-heading{margin-bottom:17px}.month-heading{justify-content:flex-start;gap:12px}.month-heading h2{font-size:24px;letter-spacing:-.035em;margin:0}.icon-button{width:36px;height:36px;border:1px solid var(--line);border-radius:var(--radius-control);color:var(--ink-soft);background:var(--surface);font-size:23px;line-height:1;cursor:pointer}.icon-button:hover{border-color:var(--accent);color:var(--accent)}.today-button{border:0;background:var(--accent-soft);color:var(--accent-hover);border-radius:var(--radius-control);height:32px;padding:0 12px;font-size:12px;font-weight:700;cursor:pointer}.calendar-heading-actions{gap:13px}.copy-button{font-size:12px!important}.weekday-row,.calendar-grid{display:grid;grid-template-columns:repeat(7,minmax(0,1fr))}.weekday-row{margin-bottom:6px;color:var(--muted);text-align:center;font-size:11px;font-weight:700}.calendar-grid{border-top:1px solid var(--line);border-left:1px solid var(--line);border-radius:12px;overflow:hidden}.calendar-cell{position:relative;min-width:0;min-height:102px;padding:11px 10px 10px;border:0;border-right:1px solid var(--line);border-bottom:1px solid var(--line);background:var(--surface);text-align:left;cursor:pointer;transition:background .16s ease}.calendar-cell:hover{background:var(--surface-soft)}.calendar-cell.is-selected{background:var(--accent-soft);box-shadow:inset 0 0 0 2px var(--accent);z-index:1}.calendar-cell.outside-month{background:var(--surface);color:var(--muted)}.calendar-cell.is-today .cell-date{background:var(--accent);color:var(--on-accent)}.cell-date{width:25px;height:25px;border-radius:9px;display:grid;place-items:center;font-size:12px;font-weight:700}.cell-count{position:absolute;right:10px;top:13px;color:var(--muted);font-size:10px}.cell-income,.cell-expense{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:11px;font-weight:700;margin-top:9px}.cell-income{color:var(--success)}.cell-expense{margin-top:4px;color:var(--danger)}.cell-bar{position:absolute;left:10px;right:10px;bottom:9px;height:3px;background:var(--surface-muted);border-radius:4px;overflow:hidden}.cell-bar i{display:block;height:100%;background:var(--accent);border-radius:4px}.cell-open{position:absolute;right:9px;bottom:17px;color:var(--muted);font-size:11px;opacity:0;transition:opacity .15s}.calendar-cell:hover .cell-open{opacity:1}.calendar-empty{display:none}.mobile-agenda{display:none}
.analysis-section{padding:36px 0 0}.analysis-heading{margin-bottom:16px}.analysis-heading .eyebrow{margin-bottom:5px}.analysis-heading h2{margin:0;font-size:23px;letter-spacing:-.03em}.analysis-period{border:1px solid var(--line);border-radius:999px;padding:7px 12px;background:var(--surface);color:var(--ink-soft);font-size:12px}.analysis-grid{display:grid;grid-template-columns:1.45fr 1fr;gap:13px}.analysis-card{padding:19px;min-width:0}.analysis-card h3{margin:0;font-size:14px;letter-spacing:-.015em}.card-heading{min-height:22px;gap:12px}.card-heading>span{color:var(--muted);font-size:11px}.card-heading>div>p{margin:5px 0 0;color:var(--muted);font-size:11px}.trend-chart{display:flex;align-items:flex-end;gap:4px;height:158px;padding:15px 4px 0;overflow:hidden}.trend-column{position:relative;display:flex;flex:1;min-width:3px;height:100%;align-items:flex-end;justify-content:center}.trend-bar{display:block;width:min(15px,80%);min-height:2px;border-radius:5px 5px 2px 2px;background:linear-gradient(180deg,color-mix(in srgb,var(--success) 65%,var(--surface)),var(--success))}.trend-bar.loss{background:linear-gradient(180deg,color-mix(in srgb,var(--danger) 55%,var(--surface)),var(--danger))}.trend-column small{position:absolute;bottom:-18px;color:var(--muted);font-size:9px}.trend-baseline{height:1px;background:var(--line);margin-top:5px}.weekday-chart{height:187px;display:grid;grid-template-columns:repeat(7,1fr);gap:9px;align-items:end;padding:15px 1px 0}.weekday-column{height:100%;display:flex;flex-direction:column;align-items:center;justify-content:flex-end;gap:6px;color:var(--muted);font-size:10px}.weekday-column small{font-size:9px;color:var(--muted)}.weekday-track{height:130px;display:flex;align-items:flex-end;width:100%;justify-content:center}.weekday-track i{display:block;width:14px;max-width:75%;border-radius:8px 8px 3px 3px;background:var(--accent);min-height:3px}.group-card{min-height:237px}.group-list{display:grid;gap:13px;margin-top:18px}.group-row{display:grid;grid-template-columns:minmax(74px,1fr) minmax(60px,1.6fr) auto;align-items:center;gap:9px;font-size:11px}.group-row>span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--ink-soft)}.group-row strong{font-size:11px;white-space:nowrap}.group-row small{grid-column:2/4;margin-top:-6px;color:var(--muted);font-size:9px}.group-track{height:6px;border-radius:8px;background:var(--surface-muted);overflow:hidden}.group-track i{display:block;height:100%;border-radius:8px;background:var(--accent)}.method-track i{background:var(--accent)}.analysis-empty{margin:34px 0 22px;text-align:center;color:var(--muted);font-size:12px}.compare-card,.yearly-card,.heatmap-card,.recent-card{margin-top:13px}.compare-heading{align-items:flex-start}.compare-heading>div:first-child{display:grid;gap:3px}.compare-mode{display:flex;padding:3px;background:var(--surface-muted);border-radius:var(--radius-control)}.compare-mode button{border:0;border-radius:8px;background:transparent;padding:8px 11px;color:var(--ink-soft);font-size:11px;font-weight:650;cursor:pointer}.compare-mode button.active{background:var(--surface);color:var(--accent-hover);box-shadow:0 2px 8px color-mix(in srgb,var(--ink) 5%,transparent)}.compare-selectors{display:flex;align-items:end;gap:11px;margin:19px 0 12px;max-width:620px}.compare-selectors label{display:grid;gap:6px;flex:1;min-width:0;color:var(--muted);font-size:11px}.compare-selectors input{width:100%;min-width:0;box-sizing:border-box;min-height:39px;border:1px solid var(--line);border-radius:var(--radius-control);background:var(--surface);padding:0 10px;color:var(--ink-soft);font:inherit}.swap-button{width:38px;height:38px;border:1px solid var(--line);border-radius:var(--radius-control);background:var(--surface);color:var(--ink-soft);font-size:18px;cursor:pointer}.compare-table{border-top:1px solid var(--line)}.compare-table-head,.compare-row{display:grid;grid-template-columns:minmax(95px,1.35fr) repeat(3,minmax(95px,1fr));align-items:center;gap:9px;padding:11px 4px;border-bottom:1px solid var(--line)}.compare-table-head{color:var(--muted);font-size:10px}.compare-row>span{color:var(--ink-soft);font-size:11px}.compare-row strong,.compare-row em{font-size:11px;font-style:normal;white-space:nowrap}.compare-row em{color:var(--success)}.compare-row em.negative{color:var(--danger)!important}.insight-strip{display:flex;align-items:center;gap:9px;margin-top:15px;padding:13px 14px;border-radius:12px;background:var(--accent-soft)}.insight-spark{color:var(--accent);font-size:18px}.insight-strip p{margin:0;color:var(--ink-soft);font-size:12px;font-weight:700}.insight-strip small{margin-left:auto;color:var(--muted);font-size:10px}.yearly-chart{display:flex;align-items:stretch;gap:7px;height:175px;margin-top:15px;overflow-x:auto;padding:4px 0}.year-month{position:relative;display:flex;flex:1;min-width:62px;flex-direction:column;justify-content:flex-end;align-items:center;gap:6px;border:1px solid transparent;border-radius:var(--radius-control);background:transparent;color:var(--ink-soft);cursor:pointer}.year-month.selected{border-color:var(--accent);background:var(--accent-soft)}.year-month>span{font-size:10px}.year-month i{display:block;width:19px;min-height:3px;max-height:105px;border-radius:6px 6px 2px 2px;background:var(--success)}.year-month i.loss{background:var(--danger)}.year-month small{width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;text-align:center;color:var(--muted);font-size:9px}.heatmap-scroll{overflow-x:auto;padding:16px 0 4px}.heatmap-grid{display:grid;grid-auto-flow:column;grid-template-rows:repeat(7,12px);grid-auto-columns:12px;gap:4px;min-width:max-content}.heat-cell{width:12px;height:12px;border-radius:3px;background:var(--surface-muted)}.heat-cell.level-1{background:color-mix(in srgb,var(--success) 18%,var(--surface))}.heat-cell.level-2{background:color-mix(in srgb,var(--success) 35%,var(--surface))}.heat-cell.level-3{background:color-mix(in srgb,var(--success) 65%,var(--surface))}.heat-cell.level-4{background:var(--success)}.heat-cell.loss.level-1{background:color-mix(in srgb,var(--danger) 18%,var(--surface))}.heat-cell.loss.level-2{background:color-mix(in srgb,var(--danger) 35%,var(--surface))}.heat-cell.loss.level-3{background:color-mix(in srgb,var(--danger) 65%,var(--surface))}.heat-cell.loss.level-4{background:var(--danger)}.recent-list{display:grid;margin-top:9px}.recent-row{display:flex;align-items:center;gap:11px;min-height:58px;padding:8px 2px;border:0;border-bottom:1px solid var(--line);background:none;text-align:left;cursor:pointer}.recent-row:last-child{border-bottom:0}.recent-type{width:31px;height:31px;flex:none;border-radius:var(--radius-control);display:grid;place-items:center;font-size:16px;font-weight:700}.recent-type.income{color:var(--success);background:var(--success-soft)}.recent-type.expense{color:var(--danger);background:var(--danger-soft)}.recent-main{display:grid;gap:5px;min-width:0}.recent-main strong{font-size:12px}.recent-main small{color:var(--muted);font-size:10px}.recent-row>b{margin-left:auto;white-space:nowrap;font-size:12px}.recent-row>b.income{color:var(--success)}.recent-row>b.expense{color:var(--danger)}
.sheet-backdrop{position:fixed;inset:0;z-index:60;display:flex;justify-content:flex-end;background:rgb(0 0 0 / 42%);backdrop-filter:blur(3px)}.ledger-sheet{position:relative;color:var(--ink);display:flex;flex-direction:column;width:min(530px,100%);height:100%;padding:30px 28px 24px;background:var(--surface-overlay);box-shadow:-18px 0 50px color-mix(in srgb,var(--ink) 12%,transparent);overflow:auto}.sheet-handle{display:none}.sheet-heading{align-items:flex-start;gap:14px;margin-bottom:14px}.sheet-heading .eyebrow{margin-bottom:4px}.sheet-heading h2{margin:0;font-size:22px;letter-spacing:-.03em}.close-button{flex:none;font-size:22px}.day-summary{display:grid;grid-template-columns:1fr auto;gap:5px 12px;padding:17px;border-radius:var(--radius-card);background:var(--accent-soft)}.day-summary>span{align-self:end;color:var(--muted);font-size:11px}.day-summary>strong{grid-column:2;grid-row:1/3;font-size:22px;align-self:center}.day-summary>small{color:var(--muted);font-size:11px}.day-sheet-actions{display:flex;gap:8px;margin:14px 0 8px}.sheet-record-list{display:grid}.sheet-record{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:start;gap:10px;padding:15px 0;border-bottom:1px solid var(--line)}.sheet-record-main{min-width:0}.sheet-record-main>strong{font-size:13px}.sheet-record-main>small{display:block;margin-top:5px;color:var(--muted);font-size:10px}.sheet-record-main p{margin:7px 0 0;color:var(--ink-soft);font-size:12px;white-space:pre-wrap;overflow-wrap:anywhere}.sheet-record-amount{white-space:nowrap;font-size:12px}.sheet-record-amount.income{color:var(--success)}.sheet-record-amount.expense{color:var(--danger)}.sheet-record>.el-button{grid-column:2;justify-self:start;margin:0;padding:3px 0;color:var(--accent)}.sheet-empty{margin:auto 0;text-align:center;color:var(--muted);font-size:13px}.editor-hint{margin:0 0 12px;color:var(--muted);font-size:12px;line-height:1.55}.save-status{display:flex;align-items:center;gap:7px;min-height:34px;padding:0 11px;border-radius:var(--radius-control);background:var(--surface-soft);color:var(--ink-soft);font-size:11px}.save-status i{width:7px;height:7px;border-radius:50%;background:var(--success)}.save-status.saving i{background:var(--warning);animation:pulse 1s infinite}.save-status.error{background:var(--danger-soft);color:var(--danger)}.save-status.error i{background:var(--danger)}.save-status button{margin-left:auto;border:0;background:none;color:var(--accent);font:inherit;font-weight:750;cursor:pointer}.ledger-form{display:grid;grid-template-columns:1fr 1fr;gap:14px 12px;margin-top:17px}.form-field{display:grid;align-content:start;gap:7px;min-width:0;color:var(--ink-soft);font-size:11px;font-weight:700}.form-field input,.form-field select,.form-field textarea{box-sizing:border-box;width:100%;min-height:43px;border:1px solid var(--line);border-radius:var(--radius-control);background:var(--surface);padding:0 12px;color:var(--ink);font:inherit;font-size:14px;font-weight:500;line-height:1.4;outline:none}.form-field select{font-size:13px}.form-field input:focus,.form-field select:focus,.form-field textarea:focus{border-color:var(--focus);box-shadow:0 0 0 3px color-mix(in srgb,var(--focus) 16%,transparent)}.form-field textarea{padding:10px 12px;resize:vertical}.form-field small{color:var(--danger);font-size:10px}.amount-field{grid-column:span 2}.amount-input{position:relative}.amount-input>span{position:absolute;left:13px;top:13px;color:var(--ink-soft);font-size:15px}.amount-input input{padding-left:32px;font-size:19px;font-weight:700}.note-field{grid-column:span 2}.form-actions{grid-column:1/-1;gap:8px;margin-top:2px;padding-top:14px;border-top:1px solid var(--line)}.form-action-spacer{flex:1}.delete-button{margin-right:auto;color:var(--danger)!important;border-color:color-mix(in srgb,var(--danger) 25%,var(--line))!important;background:var(--danger-soft)!important;border-radius:var(--radius-control)!important}.import-backdrop{justify-content:center;align-items:center}.import-dialog{width:min(560px,calc(100% - 30px));padding:24px;border-radius:var(--radius-overlay);background:var(--surface-overlay);box-shadow:var(--shadow)}.import-formats{margin:0 0 17px;color:var(--muted);font-size:12px;line-height:1.55}.import-review{padding:15px;border-radius:var(--radius-control);background:var(--surface-soft)}.import-review>strong{font-size:13px}.import-review p{margin:9px 0 0;color:var(--ink-soft);font-size:11px}.import-review .import-warning{color:var(--danger)}.import-preview-list{max-height:200px;overflow:auto;margin:13px 0}.import-preview-list>div{display:flex;justify-content:space-between;gap:12px;padding:10px 2px;border-bottom:1px solid var(--line);color:var(--ink-soft);font-size:11px}.import-preview-list strong{white-space:nowrap;color:var(--accent-hover)}.import-actions{justify-content:flex-end;gap:8px;padding-top:12px;border-top:1px solid var(--line)}
/* 筛选器保持与原生表单相同的键盘焦点提示，选中状态同时通过 ARIA 暴露。 */
.filter-controls :deep(.el-input__wrapper.is-focus),.filter-controls :deep(.el-select__wrapper.is-focused){box-shadow:0 0 0 1px var(--focus) inset,0 0 0 3px color-mix(in srgb,var(--focus) 16%,transparent)!important}
@keyframes pulse{50%{opacity:.35}}.ledger-page :deep(.el-button){font-family:inherit}.ledger-page :deep(.el-button.is-disabled){opacity:.5}.ledger-page :deep(.el-input__wrapper),.ledger-page :deep(.el-select__wrapper){box-shadow:0 0 0 1px var(--line) inset}.ledger-page :deep(.el-dropdown-menu__item){font-size:12px}.workspace-footer{display:flex;justify-content:space-between;padding:28px 3px 6px;color:var(--muted);font-size:10px}
@media(max-width:1050px){.ledger-main{width:min(100% - 44px,900px);padding-top:35px}.filter-controls{grid-template-columns:repeat(3,minmax(0,1fr))}.search-filter{grid-column:span 2}.analysis-grid{grid-template-columns:1fr 1fr}.trend-card{grid-column:span 2}.calendar-cell{min-height:90px;padding:9px 7px}.cell-income,.cell-expense{font-size:10px}.kpi-card{padding:16px}.hero-actions{flex-wrap:wrap;justify-content:flex-end}}
@media(max-width:700px){.ledger-main{width:calc(100% - 28px);padding:25px 0 18px}.ledger-hero{align-items:flex-start;flex-direction:column;margin-bottom:17px;gap:15px}.hero-copy h1{font-size:31px}.hero-copy p:last-child{font-size:13px}.hero-actions{width:100%;display:grid;grid-template-columns:1fr 1fr;gap:8px}.hero-actions>:not(.visually-hidden){width:100%}.hero-actions :deep(.el-button){justify-content:center}.hero-actions .el-dropdown{width:100%}.ledger-kpis{grid-template-columns:1fr 1fr;gap:8px}.kpi-card{min-height:89px;padding:13px 14px;border-radius:var(--radius-card)}.kpi-card>span{font-size:10px}.kpi-card strong{margin-top:8px;font-size:19px}.kpi-card small{font-size:9px}.kpi-trend{top:12px;right:12px;width:24px;height:24px}.draft-banner{align-items:flex-start;flex-wrap:wrap;padding:12px;gap:9px}.draft-banner-items{width:100%;grid-column:1/-1;margin-left:0}.draft-banner>div:nth-child(2) strong{font-size:11px}.filter-panel{padding:13px;border-radius:var(--radius-card)}.filter-title{margin-bottom:10px}.filter-title>div{align-items:flex-start;flex-direction:column;gap:3px}.filter-title small{font-size:10px}.filter-controls{grid-template-columns:1fr 1fr;gap:7px}.search-filter{grid-column:span 2}.filter-controls :deep(.el-input__wrapper),.filter-controls :deep(.el-select__wrapper){min-height:42px}.filter-controls :deep(.el-input__inner),.filter-controls :deep(.el-select__placeholder){font-size:11px}.calendar-section{padding:13px 10px;border-radius:var(--radius-card)}.calendar-heading{align-items:flex-start;flex-direction:column;gap:12px;margin-bottom:13px}.month-heading{gap:7px}.month-heading h2{font-size:20px}.icon-button{width:34px;height:34px}.today-button{height:31px;padding:0 10px}.calendar-heading-actions{width:100%;justify-content:space-between}.month-record-count{font-size:10px}.copy-button{min-height:36px!important;padding:0 10px!important}.weekday-row{font-size:9px;margin-bottom:3px}.calendar-grid{display:none}.calendar-empty{display:grid;justify-items:center;gap:8px;padding:30px 10px 14px;text-align:center}.calendar-empty p{margin:0;color:var(--muted);font-size:12px}.empty-orbit{font-size:28px;color:var(--accent)}.mobile-agenda{display:block;margin-top:12px}.agenda-header{display:flex;align-items:center;justify-content:space-between;padding:5px 3px 8px}.agenda-header h3{margin:0;font-size:13px}.agenda-header>span{color:var(--muted);font-size:10px}.agenda-day{display:grid;grid-template-columns:45px minmax(0,1fr) auto 14px;align-items:center;gap:8px;width:100%;min-height:68px;padding:9px 4px;border:0;border-bottom:1px solid var(--line);background:var(--surface);text-align:left;cursor:pointer}.agenda-date{display:grid;gap:2px;text-align:center}.agenda-date strong{font-size:18px}.agenda-date small{color:var(--muted);font-size:9px}.agenda-lines{display:grid;gap:5px;min-width:0}.agenda-lines>span{display:flex;align-items:center;gap:5px;overflow:hidden;white-space:nowrap;text-overflow:ellipsis;color:var(--ink-soft);font-size:10px}.agenda-lines i{font-size:9px;font-style:normal;font-weight:800}.agenda-lines i.income{color:var(--success)}.agenda-lines i.expense{color:var(--danger)}.agenda-lines small{color:var(--muted);font-size:9px}.agenda-net{white-space:nowrap;color:var(--success);font-size:10px;font-weight:700}.agenda-net.negative{color:var(--danger)!important}.agenda-arrow{font-size:19px;color:var(--muted)}.agenda-empty{padding:12px;color:var(--muted);text-align:center;font-size:11px}.analysis-section{padding-top:26px}.analysis-heading{margin-bottom:13px}.analysis-heading h2{font-size:20px}.analysis-grid{grid-template-columns:1fr;gap:9px}.trend-card{grid-column:auto}.analysis-card{padding:15px;border-radius:var(--radius-card)}.trend-chart{height:133px;gap:2px}.trend-bar{width:min(10px,85%)}.weekday-chart{height:167px;gap:5px}.weekday-track{height:112px}.weekday-track i{width:12px}.group-card{min-height:190px}.group-list{gap:12px}.group-row{grid-template-columns:minmax(68px,.9fr) minmax(50px,1.3fr) auto;gap:7px}.group-row strong{font-size:10px}.group-row small{font-size:8px}.compare-card,.yearly-card,.heatmap-card,.recent-card{margin-top:9px}.compare-heading{align-items:flex-start;flex-direction:column}.compare-mode{align-self:stretch}.compare-mode button{flex:1}.compare-selectors{gap:7px}.compare-selectors label{font-size:10px}.compare-selectors input{font-size:12px}.compare-table-head,.compare-row{grid-template-columns:minmax(65px,1.05fr) repeat(3,minmax(65px,1fr));gap:4px;padding:10px 0}.compare-row>span,.compare-row strong,.compare-row em{font-size:9px}.insight-strip{flex-wrap:wrap;padding:11px}.insight-strip small{width:100%;margin-left:27px}.yearly-chart{height:157px;gap:3px}.year-month{min-width:52px}.year-month small{font-size:8px}.heatmap-grid{grid-template-rows:repeat(7,10px);grid-auto-columns:10px;gap:3px}.heat-cell{width:10px;height:10px}.recent-row{min-height:56px}.recent-main strong{font-size:11px}.recent-main small{font-size:9px}.workspace-footer{padding-top:23px}.sheet-backdrop{align-items:flex-end;justify-content:center}.ledger-sheet{width:100%;height:min(92dvh,850px);padding:12px 18px calc(18px + env(safe-area-inset-bottom));border-radius:var(--radius-overlay) var(--radius-overlay) 0 0;box-shadow:0 -15px 50px color-mix(in srgb,var(--ink) 13%,transparent)}.sheet-handle{display:block;align-self:center;width:39px;height:4px;margin:1px 0 17px;border-radius:4px;background:var(--line-strong)}.sheet-heading h2{font-size:19px}.day-summary{padding:13px}.day-sheet-actions{display:grid;grid-template-columns:1fr 1fr}.day-sheet-actions>*{margin:0!important}.sheet-record{grid-template-columns:auto minmax(0,1fr) auto;gap:8px;padding:13px 0}.sheet-record-main>strong{font-size:12px}.sheet-record-main>small{font-size:9px}.sheet-record-amount{font-size:10px}.sheet-record>.el-button{font-size:11px}.editor-hint{font-size:11px}.save-status{font-size:10px}.ledger-form{gap:12px 9px;margin-top:13px}.form-field{font-size:10px}.form-field input,.form-field select,.form-field textarea{min-height:42px;padding-left:10px;font-size:16px}.form-field select{font-size:13px}.amount-input input{padding-left:31px;font-size:19px}.form-field textarea{font-size:15px}.form-actions{position:sticky;bottom:-18px;background:var(--surface-overlay);padding:12px 0 max(4px,env(safe-area-inset-bottom));margin-top:0}.form-actions :deep(.el-button){min-height:42px!important;margin-left:0!important;padding:0 10px!important;font-size:11px}.form-action-spacer{display:none}.delete-button{margin-right:auto!important}.import-backdrop{align-items:flex-end}.import-dialog{width:100%;padding:20px 18px calc(18px + env(safe-area-inset-bottom));border-radius:var(--radius-overlay) var(--radius-overlay) 0 0}.import-dialog .sheet-heading{margin-bottom:11px}.import-preview-list{max-height:150px}}
@media(max-width:380px){.ledger-main{width:calc(100% - 20px)}.hero-actions{grid-template-columns:1fr 1fr}.hero-actions :deep(.el-button){padding:0 10px!important;font-size:11px}.kpi-card strong{font-size:17px}.cell-income,.cell-expense{font-size:9px}.compare-table-head,.compare-row{grid-template-columns:minmax(58px,.9fr) repeat(3,minmax(57px,1fr))}.compare-row>span,.compare-row strong,.compare-row em{font-size:8px}.ledger-sheet{padding-left:13px;padding-right:13px}}
/* 点击行与普通按钮都提供轻量反馈，不改变尺寸或内容位置。 */
.ledger-page button{transition:background-color .16s ease,border-color .16s ease,color .16s ease,box-shadow .16s ease}
.recent-row:hover,.agenda-day:hover{background:var(--surface-soft)}.text-action:hover{color:var(--accent-hover)}
.ledger-page button:focus-visible,.ledger-page input:focus-visible,.ledger-page select:focus-visible,.ledger-page textarea:focus-visible{outline:3px solid var(--focus);outline-offset:3px}
.ledger-page .cell-net{display:block;margin-top:4px;overflow:hidden;color:var(--ink-soft);font-size:10px;font-weight:700;text-overflow:ellipsis;white-space:nowrap}
@media(max-width:768px){.ledger-main{padding-bottom:calc(92px + env(safe-area-inset-bottom))}.ledger-page .icon-button,.ledger-page .today-button,.ledger-page .filter-controls :deep(.el-input__wrapper),.ledger-page .filter-controls :deep(.el-select__wrapper),.ledger-page .hero-actions :deep(.el-button),.ledger-page .day-sheet-actions :deep(.el-button){min-height:44px!important}.ledger-page .icon-button{width:44px;height:44px}.ledger-page .today-button{height:44px}.ledger-page .filter-controls :deep(.el-input__wrapper),.ledger-page .filter-controls :deep(.el-select__wrapper){height:44px}.ledger-page .ledger-primary,.ledger-page .soft-button{min-height:44px!important}.ledger-page .form-field input,.ledger-page .form-field select{min-height:44px;font-size:16px}.ledger-page .compare-mode button,.ledger-page .compare-selectors input,.ledger-page .text-action,.ledger-page .save-status button,.ledger-page .draft-banner-items button{min-height:44px}.ledger-page .swap-button{width:44px;height:44px;flex:0 0 44px}.ledger-page .form-actions :deep(.el-button){min-height:44px!important}.ledger-page .sheet-record>.el-button{min-height:44px}}
@media(prefers-reduced-motion:reduce){.ledger-page *,.ledger-page *::before,.ledger-page *::after{animation-duration:.01ms!important;animation-iteration-count:1!important;scroll-behavior:auto!important;transition-duration:.01ms!important}}
@media(min-width:769px){.ledger-main{width:min(1440px,calc(100vw - 310px));margin-right:auto;margin-left:calc(226px + max(42px,(100vw - 1666px)/2))}.ledger-main .workspace-footer{width:auto;margin:0}}
@media(max-width:768px){.ledger-main .workspace-footer{width:auto;margin:0}}
@media(min-width:769px) and (max-width:1050px){.calendar-grid{display:none}.mobile-agenda{display:block}.analysis-grid{grid-template-columns:1fr 1fr}.trend-card{grid-column:span 2}.compare-table-head,.compare-row{grid-template-columns:minmax(68px,1fr) repeat(3,minmax(0,1fr));gap:5px}.compare-row strong,.compare-row em{font-size:9px;overflow-wrap:anywhere;white-space:normal}}
.month-picker{min-height:40px;max-width:155px;border:1px solid var(--line);border-radius:8px;background:var(--surface);color:var(--ink);padding:0 8px}
/* 日期内保留逐笔摘要；窄屏横向滚动日历，同时提供原有每日列表。 */
.calendar-cell{vertical-align:top;min-height:195px;padding-bottom:25px}.cell-records{display:grid;gap:5px;margin-top:9px}.cell-record{display:flex;gap:5px;justify-content:space-between;font-size:10px;color:var(--muted)}.cell-record>span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.cell-record b{white-space:nowrap;font-weight:600}.cell-record b.income{color:var(--success)}.cell-record b.expense{color:var(--danger)}.cell-more{display:block;font-size:10px;color:var(--accent);margin-top:5px}.calendar-cell:focus-visible{outline:2px solid var(--accent);outline-offset:-3px}
@media(max-width:1050px){.calendar-section{overflow-x:auto}.calendar-grid{display:grid;min-width:770px}.weekday-row{min-width:770px}.calendar-cell{min-height:195px}.calendar-heading{position:sticky;left:0}.mobile-agenda{position:sticky;left:0}.calendar-empty{display:none}}
</style>
