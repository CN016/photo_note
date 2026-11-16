<script setup lang="ts">
import ContentZoom from '../components/ContentZoom.vue'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Grid, Tickets, Histogram, Refresh, ArrowRight } from '@element-plus/icons-vue'
import { api } from '../api/client'
import { useAuthStore } from '../stores/auth'
import { chinaDateKey } from '../schedule/scheduleUtils'

const { t, locale } = useI18n()
const auth = useAuthStore()
const month = ref(chinaDateKey().slice(0, 7))
const mode = ref<'cards' | 'table' | 'bars'>('cards')
const modes = [{ key: 'cards' as const, icon: Grid }, { key: 'table' as const, icon: Tickets }, { key: 'bars' as const, icon: Histogram }]
type Metric = { key: string; to: string; period: 'all' | 'month' | 'active' | 'unread'; value: number | null; failed: boolean }
const metrics = ref<Metric[]>([])
const finance = ref<{ income: number; expense: number } | null>(null)
const loading = ref(false)
let version = 0
const scope = () => `${auth.sessionID}:${auth.orgid}:${auth.sessionVersion}:${auth.user?.business_id ?? ''}`
const number = (value: number) => new Intl.NumberFormat(locale.value).format(value)
const money = (cents: number) => new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'CNY' }).format(cents / 100)
const maximum = computed(() => Math.max(1, ...metrics.value.map(item => item.value ?? 0)))
const dateRange = computed(() => {
  // 月份输入仅接受有效年月；按业务北京时间日期键读取当月档期与账本，不使用UTC日期截断。
  const value = /^\d{4}-(0[1-9]|1[0-2])$/.test(month.value) ? month.value : chinaDateKey().slice(0, 7)
  const [year, monthNumber] = value.split('-').map(Number)
  return { from: `${value}-01`, to: `${value}-${new Date(year!, monthNumber!, 0).getDate()}` }
})
/** 各模块独立读取，失败显示不可用而非伪造零；账号/空间/月份变化后丢弃旧批次结果。 */
async function loadSummary() {
  const current = ++version, identity = scope(), { from, to } = dateRange.value
  loading.value = true; finance.value = null
  const definitions: Array<Omit<Metric, 'value' | 'failed'> & { load: () => Promise<number> }> = [
    { key: 'inspirations', to: '/inspirations', period: 'all', load: async () => (await api.inspirations(1, 1)).total },
    { key: 'projects', to: '/projects', period: 'all', load: async () => (await api.projects(1, 1)).total },
    { key: 'portfolios', to: '/portfolios', period: 'all', load: async () => (await api.portfolios()).items.length },
    { key: 'delivery', to: '/delivery', period: 'all', load: async () => (await api.deliveryList()).items.length },
    { key: 'plans', to: '/plans', period: 'active', load: async () => (await api.plans()).items.filter(item => item.status === 'active').length },
    { key: 'schedule', to: '/schedule', period: 'month', load: async () => (await api.scheduleItems(from, to)).items.length },
    { key: 'messages', to: '/messages', period: 'unread', load: async () => (await api.unreadNotificationCount()).unread_count },
    { key: 'ledger', to: '/ledger', period: 'month', load: async () => {
      const items = (await api.ledgerRecords(from, to)).items.filter(item => item.status === 'complete')
      const totals = { income: 0, expense: 0 }
      for (const item of items) totals[item.type] += Math.round(Number(item.amount) * 100)
      if (current === version && identity === scope()) finance.value = totals
      return items.length
    } },
  ]
  metrics.value = definitions.map(({ key, to, period }) => ({ key, to, period, value: null, failed: false }))
  await Promise.allSettled(definitions.map(async (definition, index) => {
    try {
      const value = await definition.load()
      if (current === version && identity === scope()) metrics.value[index] = { ...metrics.value[index]!, value }
    } catch {
      if (current === version && identity === scope()) metrics.value[index] = { ...metrics.value[index]!, failed: true }
    }
  }))
  if (current === version && identity === scope()) loading.value = false
}
watch([scope, month], () => { void loadSummary() }, { immediate: true })
onBeforeUnmount(() => { version += 1 })
</script>

<template>
  <div class="workspace-page workbench-page">
    <ContentZoom><main id="main-content" class="workbench-main">
      <header class="workbench-heading"><h1>{{ t('workbench.title') }}</h1></header>
      <section class="overview-toolbar" :aria-label="t('workbench.views')">
        <label class="overview-month">{{ t('workbench.month') }}<input v-model="month" type="month" :aria-label="t('workbench.month')" /></label>
        <div class="overview-modes" role="group" :aria-label="t('workbench.views')"><button v-for="view in modes" :key="view.key" type="button" :aria-pressed="mode === view.key" :aria-label="t(`workbench.${view.key}`)" :title="t(`workbench.${view.key}`)" @click="mode = view.key"><el-icon><component :is="view.icon" /></el-icon></button></div>
        <button type="button" class="overview-refresh" :disabled="loading" :aria-label="t('workbench.refresh')" @click="loadSummary"><el-icon :class="{ 'is-loading': loading }"><Refresh /></el-icon></button>
      </section>
      <section v-if="finance" class="overview-finance" :aria-label="t('workbench.finance')"><div v-for="key in (['income', 'expense', 'balance'] as const)" :key="key"><span>{{ t(`workbench.${key}`) }}</span><strong>{{ money(key === 'balance' ? finance.income - finance.expense : finance[key]) }}</strong></div></section>
      <section :aria-label="t('workbench.statistics')" :aria-busy="loading">
        <table v-if="mode === 'table'" class="overview-table"><thead><tr><th>{{ t('workbench.module') }}</th><th>{{ t('workbench.scope') }}</th><th>{{ t('workbench.count') }}</th></tr></thead><tbody><tr v-for="item in metrics" :key="item.key"><td><RouterLink :to="item.to">{{ t(`common.${item.key}`) }}</RouterLink></td><td>{{ t(`workbench.periods.${item.period}`) }}</td><td><span v-if="item.failed">{{ t('workbench.unavailable') }}</span><span v-else-if="item.value === null">{{ t('common.loading') }}</span><strong v-else>{{ number(item.value) }}</strong></td></tr></tbody></table>
        <div v-else :class="['overview-metrics', `overview-${mode}`]">
          <RouterLink v-for="item in metrics" :key="item.key" class="overview-metric" :to="item.to">
            <div class="overview-metric-heading"><span>{{ t(`common.${item.key}`) }}</span><el-icon aria-hidden="true"><ArrowRight /></el-icon></div>
            <strong v-if="item.value !== null">{{ number(item.value) }}</strong><span v-else class="overview-status">{{ t(item.failed ? 'workbench.unavailable' : 'common.loading') }}</span>
            <small>{{ t(`workbench.periods.${item.period}`) }}</small><span v-if="mode === 'bars'" class="overview-bar" aria-hidden="true"><i :style="{ width: `${(item.value ?? 0) / maximum * 100}%` }" /></span>
          </RouterLink>
        </div>
      </section>
    </main></ContentZoom>
  </div>
</template>

<style scoped>
.workbench-heading { margin-bottom: 24px; }
.overview-toolbar { display: flex; align-items: center; gap: 12px; margin-bottom: 24px; }
.overview-month { display: flex; min-width: 0; align-items: center; gap: 10px; color: var(--muted); font-size: 14px; }
.overview-month input { min-width: 0; min-height: 44px; padding: 8px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); color: var(--ink); font: inherit; color-scheme: light dark; }
.overview-modes { display: flex; margin-left: auto; padding: 3px; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); }
.overview-modes button, .overview-refresh { display: grid; width: 44px; height: 44px; place-items: center; border: 0; border-radius: 9px; background: transparent; color: var(--muted); font-size: 19px; cursor: pointer; }
.overview-modes button[aria-pressed="true"] { background: var(--accent-soft); color: var(--accent-hover); }
.overview-refresh { border: 1px solid var(--line); background: var(--surface); }
.overview-finance { display: grid; grid-template-columns: repeat(3,minmax(0,1fr)); gap: 24px; padding: 24px 0 28px; margin-bottom: 24px; border-top: 1px solid var(--line); border-bottom: 1px solid var(--line); }
.overview-finance div { display: grid; gap: 10px; }
.overview-finance span, .overview-metric small { color: var(--muted); font-size: 13px; }
.overview-finance strong { color: var(--ink); font-size: clamp(18px,2.3vw,30px); overflow-wrap: anywhere; font-variant-numeric: tabular-nums; }
.overview-metrics { display: grid; gap: 16px; }
.overview-cards { grid-template-columns: repeat(4,minmax(0,1fr)); }
.overview-metric { display: flex; min-width: 0; min-height: 176px; flex-direction: column; gap: 12px; padding: 22px; border: 1px solid var(--line); border-radius: var(--radius-card); background: var(--surface); text-decoration: none; color: var(--ink); }
.overview-metric:hover { border-color: var(--accent); }
.overview-metric-heading { display: flex; align-items: center; justify-content: space-between; gap: 8px; font-size: 15px; }
.overview-metric-heading .el-icon { color: var(--muted); }
.overview-metric > strong { font-size: 36px; line-height: 1.2; font-variant-numeric: tabular-nums; }
.overview-status { color: var(--muted); font-size: 14px; }
.overview-metric small { margin-top: auto; }
.overview-bars { grid-template-columns: repeat(2,minmax(0,1fr)); }
.overview-bars .overview-metric { min-height: 150px; }
.overview-bar { display: block; height: 7px; overflow: hidden; border-radius: 4px; background: var(--surface-soft); }
.overview-bar i { display: block; height: 100%; background: var(--accent); border-radius: inherit; }
.overview-table { width: 100%; border-collapse: collapse; color: var(--ink); font-size: 14px; }
.overview-table th, .overview-table td { padding: 18px 12px; text-align: left; border-bottom: 1px solid var(--line); }
.overview-table th { color: var(--muted); font-weight: 500; }
.overview-table td:last-child, .overview-table th:last-child { text-align: right; }
.overview-table a { color: inherit; text-decoration: none; }
@media(max-width:1100px) { .overview-cards { grid-template-columns: repeat(2,minmax(0,1fr)); } }
@media(max-width:600px) {
  .overview-toolbar { flex-wrap: wrap; gap: 8px; }
  .overview-month { flex-basis: 100%; }
  .overview-modes { margin-left: 0; }
  .overview-finance { gap: 12px; }
  .overview-finance span { font-size: 12px; }
  .overview-finance strong { font-size: 17px; }
  .overview-metric { padding: 16px; min-height: 160px; }
  .overview-bars { grid-template-columns: 1fr; }
}
</style>
