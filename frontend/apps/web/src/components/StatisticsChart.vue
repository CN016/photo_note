<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import * as echarts from 'echarts/core'
import { BarChart, LineChart, PieChart, TreemapChart, RadarChart, ScatterChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, RadarComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { EChartsCoreOption } from 'echarts/core'
import type { InspirationStatisticBucket } from '../api/types'

echarts.use([BarChart, LineChart, PieChart, TreemapChart, RadarChart, ScatterChart, GridComponent, TooltipComponent, RadarComponent, CanvasRenderer])

/** 只渲染传入的聚合值；交互文字由父页面提供可访问的同序列表。 */
const props = defineProps<{ items: InspirationStatisticBucket[]; labels: string[]; mode: string; trend?: boolean }>()
const { locale } = useI18n()
const element = ref<HTMLElement | null>(null)
let chart: echarts.ECharts | null = null
let observer: ResizeObserver | null = null
let themeObserver: MutationObserver | null = null

/** 从当前主题变量读取颜色，使深浅模式切换时图表与页面一致。 */
function palette() {
  const styles = getComputedStyle(document.documentElement)
  const accent = styles.getPropertyValue('--accent').trim() || '#315fe9'
  const ink = styles.getPropertyValue('--ink').trim() || '#172544'
  const muted = styles.getPropertyValue('--muted').trim() || '#6b7890'
  return { accent, ink, muted, colors: [accent, '#65a9e8', '#7c79d5', '#48b5a0', '#e7a85a', '#dc7790'] }
}

/** ECharts 的 HTML tooltip 会渲染图片元数据名称，必须先转义。 */
function escapeHTML(value: string): string { return value.replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char] || char) }

/** 为各图形生成统一的绝对计数提示，比例仅在属性分组内计算。 */
function options(): EChartsCoreOption {
  const theme = palette()
  const counts = props.items.map(item => item.count)
  const total = counts.reduce((sum, value) => sum + value, 0)
  const data = props.items.map((item, index) => ({ name: props.labels[index] || item.value, value: item.count, itemStyle: { color: theme.colors[index % theme.colors.length] } }))
  const number = new Intl.NumberFormat(locale.value)
  const percent = new Intl.NumberFormat(locale.value, { style: 'percent', maximumFractionDigits: 1 })
  const tooltip = { trigger: 'item', formatter: (entry: { name?: string; value?: number; data?: { value?: number } }) => {
    const value = Number(entry.data?.value ?? entry.value ?? 0)
    return `${escapeHTML(entry.name || '')}: ${number.format(value)}${props.trend ? '' : ` · ${percent.format(total ? value / total : 0)}`}`
  } }
  const base = { animation: false, color: theme.colors, textStyle: { color: theme.ink }, tooltip }
  if (props.trend) {
    const horizontal = props.mode === 'bar'
    const names = props.labels
    const axis = { type: 'category', data: names, axisLabel: { color: theme.muted, hideOverlap: true }, axisLine: { lineStyle: { color: theme.muted } }, axisTick: { show: false } }
    const valueAxis = { type: 'value', minInterval: 1, axisLabel: { color: theme.muted }, splitLine: { lineStyle: { opacity: .12 } } }
    const common = { data: counts, itemStyle: { color: theme.accent }, symbolSize: 8 }
    const series = props.mode === 'line' || props.mode === 'area' || props.mode === 'step'
      ? { type: 'line', ...common, smooth: props.mode === 'step' ? false : .25, step: props.mode === 'step' ? 'middle' : undefined, areaStyle: props.mode === 'area' ? { opacity: .2 } : undefined }
      : props.mode === 'lollipop'
        ? [{ type: 'bar', data: counts, barWidth: 3, itemStyle: { color: theme.accent } }, { type: 'line', data: counts, lineStyle: { opacity: 0 }, symbolSize: 11, itemStyle: { color: theme.accent } }]
        : props.mode === 'scatter'
          ? { type: 'scatter', ...common, symbolSize: 12 }
        : { type: 'bar', ...common, barMaxWidth: 24, barMinHeight: 2 }
    return { ...base, grid: { left: 38, right: 18, top: 18, bottom: horizontal ? 14 : 36, containLabel: true }, xAxis: horizontal ? valueAxis : axis, yAxis: horizontal ? { ...axis, data: [...names].reverse() } : valueAxis, series: horizontal ? { type: 'bar', data: [...counts].reverse(), itemStyle: { color: theme.accent }, barMaxWidth: 18 } : series }
  }
  if (props.mode === 'treemap') return { ...base, series: [{ type: 'treemap', data, roam: false, breadcrumb: { show: false }, label: { show: true, overflow: 'truncate', color: '#fff' }, itemStyle: { borderColor: '#fff', borderWidth: 2 } }] }
  if (props.mode === 'radar') return { ...base, tooltip: { trigger: 'item', formatter: () => props.labels.map((name, index) => `${escapeHTML(name)}: ${number.format(counts[index])}`).join('<br>') }, radar: { indicator: props.labels.map(name => ({ name, max: Math.max(1, ...counts) })), axisName: { color: theme.muted, fontSize: 10 }, splitNumber: 3 }, series: [{ type: 'radar', data: [{ value: counts, name: props.labels.join(' / ') }], areaStyle: { opacity: .23 }, symbolSize: 6 }] }
  if (props.mode === 'bar' || props.mode === 'column' || props.mode === 'scatter') {
    const horizontal = props.mode === 'bar'
    const axis = { type: 'category', data: horizontal ? [...props.labels].reverse() : props.labels, axisLabel: { color: theme.muted, hideOverlap: true, width: 92, overflow: 'truncate' }, axisTick: { show: false } }
    const valueAxis = { type: 'value', minInterval: 1, axisLabel: { color: theme.muted }, splitLine: { lineStyle: { opacity: .12 } } }
    return { ...base, grid: { left: 14, right: 18, top: 16, bottom: horizontal ? 12 : 36, containLabel: true }, xAxis: horizontal ? valueAxis : axis, yAxis: horizontal ? axis : valueAxis, series: [{ type: props.mode === 'scatter' ? 'scatter' : 'bar', data: horizontal ? [...data].reverse() : data, barMaxWidth: 30, barMinHeight: 2, symbolSize: 13 }] }
  }
  return { ...base, series: [{ type: 'pie', radius: props.mode === 'donut' ? ['48%', '72%'] : ['0%', '72%'], center: ['50%', '50%'], roseType: props.mode === 'rose' ? 'radius' : undefined, avoidLabelOverlap: true, label: { show: false }, data }] }
}

/** 复用图实例并在数据、语言、模式或主题变化时更新配置。 */
function render() {
  if (!element.value) return
  if (!chart) chart = echarts.init(element.value)
  chart.setOption(options(), true)
  chart.resize()
}
watch(() => [props.items, props.labels, props.mode, locale.value], render, { deep: true })
onMounted(() => {
  render()
  if (element.value) { observer = new ResizeObserver(() => chart?.resize()); observer.observe(element.value) }
  themeObserver = new MutationObserver(() => render())
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'class'] })
})
onBeforeUnmount(() => { observer?.disconnect(); themeObserver?.disconnect(); chart?.dispose(); chart = null })
</script>

<template><div ref="element" class="statistics-echart" aria-hidden="true"></div></template>
