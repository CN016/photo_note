<script setup lang="ts">
import ContentZoom from '../components/ContentZoom.vue'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Aim, Camera, DataAnalysis, Picture, Sunny, Timer, TrendCharts } from '@element-plus/icons-vue'
import AppHeader from '../components/AppHeader.vue'
import StatisticsChart from '../components/StatisticsChart.vue'
import { api } from '../api/client'
import { ApiError, type InspirationStatisticBucket, type InspirationStatistics } from '../api/types'
import { useAuthStore } from '../stores/auth'
import { ElMessage } from 'element-plus'

const { t, locale } = useI18n()
const statistics = ref<InspirationStatistics | null>(null)
const auth = useAuthStore()
const chartModes = ref<Record<string, string>>({})
const savingModes = ref<Record<string, boolean>>({})
const metricModes = ['donut', 'pie', 'rose', 'bar', 'column', 'treemap', 'radar', 'scatter']
const trendModes = ['column', 'line', 'area', 'bar', 'lollipop', 'step', 'scatter']
const loading = ref(false)
const error = ref('')
let requestVersion = 0

/** 只接受已知模式，旧版本或损坏偏好自动回落到各卡片默认展示。 */
function modeFor(key: string): string {
  const choices = key === 'collection_months' ? trendModes : metricModes
  const selected = chartModes.value[key]
  return choices.includes(selected) ? selected : key === 'collection_months' ? 'column' : 'donut'
}

/** 单卡片切换即时生效，失败时回退并提示；同一卡片保存期间不可重复提交。 */
async function changeMode(key: string, mode: string) {
  if (savingModes.value[key] || mode === modeFor(key)) return
  const previous = modeFor(key)
  chartModes.value = { ...chartModes.value, [key]: mode }
  savingModes.value = { ...savingModes.value, [key]: true }
  try {
    await api.updateStatisticsChartMode(key, mode)
    if (auth.user) auth.user.extra = { ...(auth.user.extra || {}), statistics_chart_modes: { ...(auth.user.extra?.statistics_chart_modes || {}), [key]: mode } }
  } catch {
    chartModes.value = { ...chartModes.value, [key]: previous }
    ElMessage.error(t('statistics.saveModeError'))
  } finally {
    savingModes.value = { ...savingModes.value, [key]: false }
  }
}

/** 使用当前用户资料中的持久化偏好，不依赖本地浏览器存储。 */
async function loadModes() {
  try {
    const user = auth.user || await api.currentUser()
    chartModes.value = { ...(user.extra?.statistics_chart_modes || {}) }
  } catch {
    chartModes.value = {}
  }
}

function chartLabels(key: string, items: InspirationStatisticBucket[]): string[] {
  return items.map(item => bucketLabel(key, item.value))
}

function chartPercent(count: number, items: InspirationStatisticBucket[]): string {
  const total = items.reduce((sum, item) => sum + item.count, 0)
  return new Intl.NumberFormat(locale.value, { style: 'percent', maximumFractionDigits: 1 }).format(total ? count / total : 0)
}

/** 统一固定区间的展示顺序，确保不同拍摄参数的图表能直观比较。 */
const bucketOrders: Record<string, string[]> = {
  focal_lengths: ['ultra_wide', 'wide', 'standard', 'telephoto', 'super_telephoto'],
  apertures: ['wide_open', 'large', 'medium', 'small', 'very_small'],
  iso_speeds: ['under_100', '100_400', '400_1600', '1600_6400', '6400_plus'],
  exposure_times: ['1_500_or_faster', '1_500_to_1_60', '1_60_to_1s', 'over_1s'],
  flash_usage: ['fired', 'not_fired'],
  aspect_ratios: ['landscape', 'portrait', 'square'],
  megapixels: ['under_2', '2_12', '12_24', '24_plus'],
}

/** 将接口聚合字段映射为页面分组、标题和图标，不在模板内复制维度定义。 */
const metricSections = computed(() => {
  const current = statistics.value
  if (!current) return []
  return [
    { key: 'camera_models', title: t('statistics.cameraModels'), icon: Camera, items: current.camera_models, top: true },
    { key: 'lens_models', title: t('statistics.lensModels'), icon: Aim, items: current.lens_models, top: true },
    { key: 'focal_lengths', title: t('statistics.focalLengths'), icon: Aim, items: current.focal_lengths, top: false },
    { key: 'apertures', title: t('statistics.apertures'), icon: Sunny, items: current.apertures, top: false },
    { key: 'iso_speeds', title: t('statistics.isoSpeeds'), icon: DataAnalysis, items: current.iso_speeds, top: false },
    { key: 'exposure_times', title: t('statistics.exposureTimes'), icon: Timer, items: current.exposure_times, top: false },
    { key: 'flash_usage', title: t('statistics.flashUsage'), icon: Sunny, items: current.flash_usage, top: false },
    { key: 'aspect_ratios', title: t('statistics.aspectRatios'), icon: Picture, items: current.aspect_ratios, top: false },
    { key: 'megapixels', title: t('statistics.megapixels'), icon: Picture, items: current.megapixels, top: false },
    { key: 'color_spaces', title: t('statistics.colorSpaces'), icon: DataAnalysis, items: current.color_spaces, top: true },
  ]
})

/** 仅按已保存的拍摄参数计算覆盖率，历史无元数据图片仍留在总数分母中。 */
const coverage = computed(() => {
  const total = statistics.value?.total_images ?? 0
  if (!total) return 0
  return Math.min(1, (statistics.value?.images_with_shooting_metadata ?? 0) / total)
})

const coverageLabel = computed(() => new Intl.NumberFormat(locale.value, { style: 'percent', maximumFractionDigits: 1 }).format(coverage.value))

/** 统计接口只返回聚合计数；网络错误和业务错误分别使用通用及本页提示。 */
function errorText(cause: unknown): string {
  if (cause instanceof ApiError && cause.code === 'network') return t('common.errors.network')
  return t('statistics.loadError')
}

/** 使用单次鉴权聚合请求读取统计；较晚返回的旧请求不会覆盖新状态。 */
async function loadStatistics() {
  const version = ++requestVersion
  loading.value = true
  error.value = ''
  try {
    const result = await api.inspirationStatistics()
    if (version === requestVersion) statistics.value = result
  } catch (cause) {
    if (version === requestVersion) error.value = errorText(cause)
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

/** 固定范围分布按拍摄顺序显示，并在无样本时仍列出零值，便于跨组比较。 */
function orderedBuckets(key: string, items: InspirationStatisticBucket[]): InspirationStatisticBucket[] {
  const order = bucketOrders[key]
  if (!order) return items
  const byValue = new Map(items.map(item => [item.value, item]))
  return order.map(value => byValue.get(value) ?? { value, count: 0 })
}

/** 把接口稳定分组键转换为当前语言，并仅对月份和设备名称使用接口值。 */
function bucketLabel(group: string, value: string): string {
  if (group === 'camera_models' || group === 'lens_models' || group === 'color_spaces') return value
  if (group === 'collection_months') {
    const [year, month] = value.split('-').map(Number)
    return new Intl.DateTimeFormat(locale.value, { month: 'short' }).format(new Date(year, month - 1, 1))
  }
  const translationGroup: Record<string, string> = {
    focal_lengths: 'focalLength', apertures: 'aperture', iso_speeds: 'iso',
    exposure_times: 'exposure', flash_usage: 'flash', aspect_ratios: 'aspect', megapixels: 'megapixels',
  }
  return t(`statistics.buckets.${translationGroup[group]}.${value}`)
}

/** 按当前界面语言格式化图片计数。 */
function formatCount(value: number): string {
  return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 0 }).format(value)
}

/** 生成包含完整年月和数量的月份悬停提示。 */
function monthTitle(item: InspirationStatisticBucket): string {
  const [year, month] = item.value.split('-').map(Number)
  const monthName = new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: 'long' }).format(new Date(year, month - 1, 1))
  return t('statistics.monthTooltip', { month: monthName, count: formatCount(item.count) })
}

onMounted(() => { void loadStatistics(); void loadModes() })
onBeforeUnmount(() => { requestVersion += 1 })
</script>

<template>
  <div class="workspace-page statistics-page">
    <AppHeader />
    <ContentZoom><main class="statistics-main">
      <section class="gallery-intro statistics-intro">
        <div><p class="eyebrow">{{ t('common.product') }}</p><h1>{{ t('statistics.title') }}</h1><p class="gallery-subtitle">{{ t('statistics.subtitle') }}</p></div>
      </section>

      <nav class="gallery-subnav" :aria-label="t('statistics.sections')">
        <RouterLink class="gallery-subnav-link" to="/inspirations">{{ t('statistics.listSection') }}</RouterLink>
        <RouterLink class="gallery-subnav-link is-current" to="/inspirations/statistics" aria-current="page">{{ t('statistics.statisticsSection') }}</RouterLink>
      </nav>

      <div v-if="error" class="inline-error" role="alert"><span>{{ error }}</span><el-button text :disabled="loading" @click="loadStatistics">{{ t('statistics.retry') }}</el-button></div>
      <div v-if="loading && !statistics" class="gallery-loading" role="status"><span class="loading-dot"></span>{{ t('statistics.loading') }}</div>

      <section v-else-if="statistics && statistics.total_images === 0" class="gallery-empty statistics-empty">
        <div class="empty-art" aria-hidden="true"><el-icon><TrendCharts /></el-icon></div>
        <p class="eyebrow">{{ t('statistics.title') }}</p><h2>{{ t('statistics.noImagesTitle') }}</h2><p class="statistics-empty-copy">{{ t('statistics.noImagesDescription') }}</p>
        <RouterLink custom to="/inspirations/new" v-slot="{ navigate }"><el-button class="primary-button" type="primary" @click="navigate">{{ t('statistics.addImage') }}</el-button></RouterLink>
      </section>

      <section v-else-if="statistics" class="statistics-content">
        <div class="statistics-summary" :aria-label="t('statistics.title')">
          <article class="statistics-summary-card">
            <span class="statistics-summary-icon"><el-icon aria-hidden="true"><Picture /></el-icon></span>
            <div><strong>{{ formatCount(statistics.total_images) }}</strong><span>{{ t('statistics.totalImages') }}</span></div>
          </article>
          <article class="statistics-summary-card">
            <span class="statistics-summary-icon"><el-icon aria-hidden="true"><Camera /></el-icon></span>
            <div><strong>{{ formatCount(statistics.images_with_shooting_metadata) }}</strong><span>{{ t('statistics.imagesWithSettings') }}</span></div>
          </article>
          <article class="statistics-summary-card statistics-coverage-card">
            <div class="statistics-coverage-heading"><span>{{ t('statistics.settingsCoverage') }}</span><strong>{{ coverageLabel }}</strong></div>
            <div class="statistics-coverage-track" role="meter" :aria-label="t('statistics.settingsCoverage')" :aria-valuetext="coverageLabel" :aria-valuenow="Math.round(coverage * 100)" aria-valuemin="0" aria-valuemax="100"><span :style="{ width: `${coverage * 100}%` }"></span></div>
            <small>{{ t('statistics.coverageDescription') }}</small>
          </article>
        </div>

        <section class="statistics-card statistics-collection-card" :aria-label="t('statistics.collectionTitle')">
          <header class="statistics-card-header"><span class="statistics-card-icon"><el-icon aria-hidden="true"><TrendCharts /></el-icon></span><div><h2>{{ t('statistics.collectionTitle') }}</h2><p>{{ t('statistics.collectionDescription') }}</p></div>
            <el-select class="statistics-mode-select" :aria-label="t('statistics.chartModeFor', { name: t('statistics.collectionTitle') })" :model-value="modeFor('collection_months')" :disabled="savingModes.collection_months" @change="(value: string) => changeMode('collection_months', value)"><el-option v-for="mode in trendModes" :key="mode" :label="t(`statistics.chartModes.${mode}`)" :value="mode" /></el-select>
          </header>
          <StatisticsChart :items="statistics.collection_months" :labels="chartLabels('collection_months', statistics.collection_months)" :mode="modeFor('collection_months')" trend />
          <ol class="statistics-chart-legend statistics-trend-legend"><li v-for="month in statistics.collection_months" :key="month.value"><span>{{ monthTitle(month) }}</span></li></ol>
        </section>

        <div class="statistics-grid">
          <article v-for="group in metricSections" :key="group.key" class="statistics-card statistics-metric-card">
            <header class="statistics-card-header">
              <span class="statistics-card-icon"><el-icon aria-hidden="true"><component :is="group.icon" /></el-icon></span>
              <div class="statistics-card-heading"><h2>{{ group.title }}</h2><small v-if="group.top">{{ t('statistics.topFiveShare') }}</small></div>
              <el-select class="statistics-mode-select" :aria-label="t('statistics.chartModeFor', { name: group.title })" :model-value="modeFor(group.key)" :disabled="savingModes[group.key] || !group.items.length" @change="(value: string) => changeMode(group.key, value)"><el-option v-for="mode in metricModes" :key="mode" :label="t(`statistics.chartModes.${mode}`)" :value="mode" /></el-select>
            </header>
            <p v-if="group.key === 'focal_lengths'" class="statistics-card-note">{{ t('statistics.focalDescription') }}</p>
            <StatisticsChart v-if="group.items.length" :items="orderedBuckets(group.key, group.items)" :labels="chartLabels(group.key, orderedBuckets(group.key, group.items))" :mode="modeFor(group.key)" />
            <ol v-if="group.items.length" class="statistics-chart-legend">
              <li v-for="(item, index) in orderedBuckets(group.key, group.items)" :key="item.value">
                <span class="statistics-chart-swatch" :style="{ '--chart-index': index }"></span><span class="statistics-value">{{ bucketLabel(group.key, item.value) }}</span><span class="statistics-count">{{ t('statistics.photoCount', { count: formatCount(item.count) }) }} · {{ chartPercent(item.count, orderedBuckets(group.key, group.items)) }}</span>
              </li>
            </ol>
            <p v-else class="statistics-no-data">{{ t('statistics.noData') }}</p>
          </article>
        </div>
      </section>
    </main></ContentZoom>
    <footer class="workspace-footer"><span>{{ t('common.brand') }}</span><span>{{ t('common.footer') }}</span></footer>
  </div>
</template>
