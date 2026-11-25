<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Aim, CollectionTag, Search, Operation, User } from '@element-plus/icons-vue'
import type { TagRecord } from '../api/types'
import {
  applyGalleryPeoplePreset,
  type GalleryPeopleMode,
  type GalleryPeoplePreset,
  type GallerySearchKind,
  type GallerySearchState,
  type GalleryTagSource,
  galleryPeoplePreset,
  gallerySearchHasValue,
  switchGallerySearchKind,
} from '../gallerySearch'
import { galleryHexToHsv, galleryHsvToHex, galleryWheelColorAtPoint } from '../galleryColor'

const props = defineProps<{
  modelValue: GallerySearchState
  tags: TagRecord[]
  tagsLoading?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: GallerySearchState] }>()
const { t } = useI18n()
const wheel = ref<HTMLButtonElement>()
const brightnessTrack = ref<HTMLButtonElement>()
const wheelSelecting = ref(false)
const brightnessSelecting = ref(false)
const wheelPreviewColor = ref('')
const wheelPreviewPoint = ref({ x: 0, y: 0 })
const wheelBrightness = ref(1)
const rememberedHue = ref(0)
const rememberedSaturation = ref(1)
const peopleAdvanced = ref(false)
const colorExpanded = ref(false)
let wheelPointerID: number | null = null
let brightnessPointerID: number | null = null

const conditionOptions = [
  { kind: 'color' as const, icon: Aim, label: 'gallery.conditionColor' },
  { kind: 'text' as const, icon: Search, label: 'gallery.conditionText' },
  { kind: 'tags' as const, icon: CollectionTag, label: 'gallery.conditionTags' },
  { kind: 'people' as const, icon: User, label: 'gallery.conditionPeople' },
]
const peopleOptions: Array<{ value: GalleryPeopleMode; label: string }> = [
  { value: 'present', label: 'gallery.peoplePresent' },
  { value: 'absent', label: 'gallery.peopleAbsent' },
  { value: 'exact', label: 'gallery.peopleExact' },
  { value: 'range', label: 'gallery.peopleRange' },
]
const peoplePresets: GalleryPeoplePreset[] = ['absent', '1', '2', '3', '4', '5', '6', 'many']
const tagSources: Array<{ value: GalleryTagSource; label: string }> = [
  { value: 'all', label: 'gallery.allTagSources' },
  { value: 'manual', label: 'gallery.manualOnly' },
  { value: 'ai', label: 'gallery.aiOnly' },
]

const visibleTags = computed(() => props.tags.filter(tag => {
  if (props.modelValue.tagSource === 'all') return true
  if (props.modelValue.tagSource === 'manual') return tag.source === 'manual' || tag.source === 'mixed'
  return tag.source === 'ai' || tag.source === 'mixed'
}))
const hasValue = computed(() => gallerySearchHasValue(props.modelValue))
const quickPeoplePreset = computed(() => galleryPeoplePreset(props.modelValue))
const wheelDisplayColor = computed(() => wheelSelecting.value || brightnessSelecting.value ? wheelPreviewColor.value : props.modelValue.color)
const wheelDisplayHsv = computed(() => galleryHexToHsv(wheelDisplayColor.value || '#ff3b30'))
const brightnessHue = computed(() => brightnessSelecting.value ? rememberedHue.value : wheelDisplayHsv.value.hue)
const brightnessSaturation = computed(() => brightnessSelecting.value ? rememberedSaturation.value : wheelDisplayHsv.value.saturation)
const brightnessTopColor = computed(() => galleryHsvToHex(brightnessHue.value, brightnessSaturation.value, 1))
const brightnessPercent = computed(() => Math.round(wheelDisplayHsv.value.value * 100))
const wheelMarker = computed(() => {
  if (!/^#[0-9a-f]{6}$/i.test(wheelDisplayColor.value)) return null
  const hsv = galleryHexToHsv(wheelDisplayColor.value)
  if (hsv.value < .15 || hsv.saturation < .03) return null
  const angle = (hsv.hue - 90) * Math.PI / 180
  const radius = hsv.saturation * 50
  return { left: `${50 + Math.cos(angle) * radius}%`, top: `${50 + Math.sin(angle) * radius}%` }
})

function patch(value: Partial<GallerySearchState>) {
  emit('update:modelValue', { ...props.modelValue, ...value })
}

function setQuery(value: string) {
  patch({ query: value })
}

function setPeopleMin(value: number | undefined) {
  patch({ peopleMin: Number(value || 0) })
}

function setPeopleMax(value: number | undefined) {
  patch({ peopleMax: Number(value || 0) })
}

function chooseKind(kind: GallerySearchKind) {
  if (kind !== props.modelValue.kind) emit('update:modelValue', switchGallerySearchKind(props.modelValue, kind))
}

/** 选择颜色条件时同步切换搜索类型并展开色环，避免只改展开状态却仍显示文本搜索。 */
function selectCondition(kind: GallerySearchKind) {
  if (kind === 'color') {
    if (props.modelValue.kind !== 'color') chooseKind('color')
    colorExpanded.value = true
    return
  }
  colorExpanded.value = false
  chooseKind(kind)
}

function setTagSource(source: GalleryTagSource) {
  const allowed = new Set(props.tags.filter(tag => source === 'all' || (source === 'manual' ? tag.source === 'manual' || tag.source === 'mixed' : tag.source === 'ai' || tag.source === 'mixed')).map(tag => tag.business_id))
  patch({ tagSource: source, tagIds: props.modelValue.tagIds.filter(id => allowed.has(id)) })
}

function toggleTag(id: string) {
  patch({ tagIds: props.modelValue.tagIds.includes(id) ? props.modelValue.tagIds.filter(tagID => tagID !== id) : [...props.modelValue.tagIds, id] })
}

function tagSourceLabel(source?: TagRecord['source']): string {
  if (source === 'ai') return 'AI'
  if (source === 'manual') return t('gallery.manualTag')
  if (source === 'mixed') return t('gallery.mixedTag')
  return ''
}

function setPeopleMode(peopleMode: GalleryPeopleMode) {
  patch({ peopleMode })
}

function choosePeoplePreset(preset: GalleryPeoplePreset) {
  emit('update:modelValue', applyGalleryPeoplePreset(props.modelValue, preset))
}

function peoplePresetLabel(preset: GalleryPeoplePreset): string {
  if (preset === 'absent') return t('gallery.peopleQuickAbsent')
  if (preset === 'many') return t('gallery.peopleQuickMany')
  return t('gallery.peopleQuickCount', { count: Number(preset) })
}

function normalizePeopleRange() {
  const peopleMin = Math.max(0, Math.min(100000, Math.round(props.modelValue.peopleMin || 0)))
  const peopleMax = Math.max(peopleMin, Math.min(100000, Math.round(props.modelValue.peopleMax || peopleMin)))
  patch({ peopleMin, peopleMax })
}

function chooseColor(color: string) {
  patch({ color })
}

function updateWheelPreview(event: PointerEvent): void {
  if (!wheel.value) return
  const result = galleryWheelColorAtPoint(event.clientX, event.clientY, wheel.value.getBoundingClientRect(), wheelBrightness.value)
  wheelPreviewColor.value = result.color
  wheelPreviewPoint.value = { x: result.x, y: result.y }
  rememberedHue.value = result.hue
  rememberedSaturation.value = result.saturation
}

/** 按住色环只移动放大色块，不向图库提交请求；手指或鼠标松开后才写入并触发检索。 */
function beginWheel(event: PointerEvent): void {
  if (!wheel.value || event.button !== 0) return
  event.preventDefault()
  wheelPointerID = event.pointerId
  wheel.value.setPointerCapture(event.pointerId)
  wheelSelecting.value = true
  updateWheelPreview(event)
}

function moveWheel(event: PointerEvent): void {
  if (!wheelSelecting.value || wheelPointerID !== event.pointerId) return
  event.preventDefault()
  updateWheelPreview(event)
}

function finishWheel(event: PointerEvent): void {
  if (!wheelSelecting.value || wheelPointerID !== event.pointerId) return
  event.preventDefault()
  updateWheelPreview(event)
  const color = wheelPreviewColor.value
  wheelSelecting.value = false
  wheelPointerID = null
  if (wheel.value?.hasPointerCapture(event.pointerId)) wheel.value.releasePointerCapture(event.pointerId)
  if (color) chooseColor(color)
}

function cancelWheel(): void {
  wheelSelecting.value = false
  brightnessSelecting.value = false
  wheelPreviewColor.value = ''
  wheelPointerID = null
  brightnessPointerID = null
}

/** 键盘左右方向键调整色相，上下方向键调整饱和度，键盘操作直接提交颜色。 */
function wheelKeydown(event: KeyboardEvent): void {
  if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key)) return
  event.preventDefault()
  let hue = rememberedHue.value, saturation = rememberedSaturation.value
  if (event.key === 'ArrowLeft') hue = (hue + 355) % 360
  if (event.key === 'ArrowRight') hue = (hue + 5) % 360
  if (event.key === 'ArrowUp') saturation = Math.min(1, saturation + .05)
  if (event.key === 'ArrowDown') saturation = Math.max(0, saturation - .05)
  rememberedHue.value = hue
  rememberedSaturation.value = saturation
  chooseColor(galleryHsvToHex(hue, saturation, wheelBrightness.value))
}

/** 明度条覆盖黑色到当前色相最亮色，补齐圆盘无法表达的灰度与暗色。 */
function updateBrightnessPreview(event: PointerEvent): void {
  if (!brightnessTrack.value) return
  const bounds = brightnessTrack.value.getBoundingClientRect()
  const value = Math.max(0, Math.min(1, 1 - (event.clientY - bounds.top) / Math.max(1, bounds.height)))
  wheelBrightness.value = value
  wheelPreviewColor.value = galleryHsvToHex(rememberedHue.value, rememberedSaturation.value, value)
}

function beginBrightness(event: PointerEvent): void {
  if (!brightnessTrack.value || event.button !== 0) return
  event.preventDefault()
  brightnessPointerID = event.pointerId
  brightnessTrack.value.setPointerCapture(event.pointerId)
  brightnessSelecting.value = true
  updateBrightnessPreview(event)
}

function moveBrightness(event: PointerEvent): void {
  if (!brightnessSelecting.value || brightnessPointerID !== event.pointerId) return
  event.preventDefault()
  updateBrightnessPreview(event)
}

function finishBrightness(event: PointerEvent): void {
  if (!brightnessSelecting.value || brightnessPointerID !== event.pointerId) return
  event.preventDefault()
  updateBrightnessPreview(event)
  const color = wheelPreviewColor.value
  brightnessSelecting.value = false
  brightnessPointerID = null
  if (brightnessTrack.value?.hasPointerCapture(event.pointerId)) brightnessTrack.value.releasePointerCapture(event.pointerId)
  if (color) chooseColor(color)
}

function brightnessKeydown(event: KeyboardEvent): void {
  if (!['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(event.key)) return
  event.preventDefault()
  const delta = event.key === 'ArrowUp' || event.key === 'ArrowRight' ? .05 : -.05
  const value = Math.max(0, Math.min(1, wheelBrightness.value + delta))
  wheelBrightness.value = value
  chooseColor(galleryHsvToHex(rememberedHue.value, rememberedSaturation.value, value))
}

watch(() => props.modelValue.kind, kind => {
  if (kind !== 'color') cancelWheel()
  if (kind !== 'people') peopleAdvanced.value = false
})
watch(() => props.modelValue.color, color => {
  const hsv = color ? galleryHexToHsv(color) : { hue: 0, saturation: 1, value: 1 }
  wheelBrightness.value = hsv.value
  if (hsv.saturation > .001 && hsv.value > .001) {
    rememberedHue.value = hsv.hue
    rememberedSaturation.value = hsv.saturation
  }
}, { immediate: true })
</script>

<template>
  <section class="gallery-search-panel" :aria-label="t('gallery.searchConditions')">
    <nav class="gallery-condition-tabs" role="tablist" :aria-label="t('gallery.searchConditions')">
      <button
        v-for="option in conditionOptions"
        :key="option.kind"
        type="button"
        role="tab"
        class="gallery-condition-tab"
        :class="{ active: modelValue.kind === option.kind }"
        :aria-selected="modelValue.kind === option.kind"
        :aria-label="t(option.label)"
        :title="t(option.label)"
        @click="selectCondition(option.kind)"
      >
        <el-icon aria-hidden="true"><component :is="option.icon" /></el-icon>
        <span v-if="modelValue.kind === option.kind && hasValue" class="condition-active-dot" aria-hidden="true" />
      </button>
    </nav>

    <div v-if="modelValue.kind === 'color'" class="color-filter-row">
      <button type="button" class="color-filter-toggle" :aria-expanded="colorExpanded" @click="colorExpanded = !colorExpanded">
        <span>{{ t('gallery.colorFilter') }}</span>
        <span v-if="modelValue.color" class="color-filter-current"><i :style="{ background: modelValue.color }" />{{ modelValue.color }}</span>
        <span v-else class="color-filter-current">{{ t('gallery.noColorSelected') }}</span>
        <el-icon aria-hidden="true"><Operation /></el-icon>
      </button>
      <button v-if="modelValue.color" type="button" class="color-filter-clear" @click="chooseColor('')">{{ t('gallery.clearCondition') }}</button>
    </div>

    <div v-if="modelValue.kind === 'text'" class="gallery-condition-body text-condition">
      <el-input
        :model-value="modelValue.query"
        class="gallery-text-search"
        :aria-label="t('gallery.searchPlaceholder')"
        :placeholder="t('gallery.searchDescriptionPlaceholder')"
        clearable
        @update:model-value="setQuery"
      />
    </div>

    <div v-if="modelValue.kind === 'color' && colorExpanded" class="gallery-condition-body color-condition">
      <button
        ref="wheel"
        type="button"
        class="color-wheel"
        role="slider"
        :aria-label="t('gallery.colorWheel')"
        aria-valuemin="0"
        aria-valuemax="359"
        :aria-valuenow="wheelDisplayColor ? Math.round(galleryHexToHsv(wheelDisplayColor).hue) : 0"
        :aria-valuetext="wheelDisplayColor || t('gallery.chooseColor')"
        @pointerdown="beginWheel"
        @pointermove="moveWheel"
        @pointerup="finishWheel"
        @pointercancel="cancelWheel"
        @lostpointercapture="cancelWheel"
        @keydown="wheelKeydown"
      >
        <span v-if="wheelMarker" class="color-wheel-marker" :style="wheelMarker" />
        <span
          v-if="wheelSelecting"
          class="color-wheel-loupe"
          :style="{ left: `${wheelPreviewPoint.x}px`, top: `${wheelPreviewPoint.y}px`, background: wheelPreviewColor }"
          aria-hidden="true"
        />
      </button>
      <button
        ref="brightnessTrack"
        type="button"
        class="color-brightness"
        role="slider"
        aria-orientation="vertical"
        aria-valuemin="0"
        aria-valuemax="100"
        :aria-label="t('gallery.brightness')"
        :aria-valuenow="brightnessPercent"
        :aria-valuetext="t('gallery.brightnessValue', { value: brightnessPercent })"
        :style="{ '--brightness-top': brightnessTopColor }"
        @pointerdown="beginBrightness"
        @pointermove="moveBrightness"
        @pointerup="finishBrightness"
        @pointercancel="cancelWheel"
        @lostpointercapture="cancelWheel"
        @keydown="brightnessKeydown"
      >
        <span class="color-brightness-track" aria-hidden="true"><span class="color-brightness-thumb" :style="{ bottom: `${brightnessPercent}%` }" /></span>
      </button>
      <div class="color-condition-copy">
        <span class="condition-kicker">{{ t('gallery.colorShortHint') }}</span>
        <strong class="selected-color">
          <i :style="{ background: wheelDisplayColor || 'transparent' }" aria-hidden="true" />
          {{ wheelDisplayColor || t('gallery.noColorSelected') }}
        </strong>
        <small class="brightness-value"><span>{{ t('gallery.brightness') }}</span><span>{{ t('gallery.brightnessValue', { value: brightnessPercent }) }}</span></small>
        <button v-if="modelValue.color" type="button" class="condition-clear" @click="chooseColor('')">{{ t('gallery.clearCondition') }}</button>
      </div>
    </div>

    <div v-if="modelValue.kind === 'tags'" class="gallery-condition-body tag-condition">
      <div class="tag-source-tabs" role="radiogroup" :aria-label="t('gallery.tagSource')">
        <button v-for="source in tagSources" :key="source.value" type="button" role="radio" :aria-checked="modelValue.tagSource === source.value" :class="{ active: modelValue.tagSource === source.value }" @click="setTagSource(source.value)">{{ t(source.label) }}</button>
      </div>
      <div v-if="tagsLoading" class="condition-status" role="status"><span class="loading-dot" />{{ t('common.loading') }}</div>
      <div v-else-if="visibleTags.length" class="tag-scroll" :aria-label="t('gallery.filterTags')">
        <button v-for="tag in visibleTags" :key="tag.business_id" type="button" class="tag-choice" :class="{ active: modelValue.tagIds.includes(tag.business_id), ai: tag.source === 'ai' || tag.source === 'mixed' }" :aria-pressed="modelValue.tagIds.includes(tag.business_id)" @click="toggleTag(tag.business_id)">
          <span>{{ tag.content }}</span>
          <small v-if="tagSourceLabel(tag.source)">{{ tagSourceLabel(tag.source) }}</small>
        </button>
      </div>
      <div v-else class="condition-status">{{ t('gallery.noTags') }}</div>
      <div class="tag-match-row">
        <div v-if="modelValue.tagIds.length" class="tag-match" role="radiogroup" :aria-label="t('gallery.tagMatch')">
          <button type="button" role="radio" :aria-checked="modelValue.tagMatch === 'any'" :class="{ active: modelValue.tagMatch === 'any' }" @click="patch({ tagMatch: 'any' })">{{ t('gallery.matchAny') }}</button>
          <button type="button" role="radio" :aria-checked="modelValue.tagMatch === 'all'" :class="{ active: modelValue.tagMatch === 'all' }" @click="patch({ tagMatch: 'all' })">{{ t('gallery.matchAll') }}</button>
        </div>
        <button v-if="modelValue.tagIds.length" type="button" class="condition-clear" @click="patch({ tagIds: [] })">{{ t('gallery.clearCondition') }}</button>
      </div>
    </div>

    <div v-if="modelValue.kind === 'people'" class="gallery-condition-body people-condition">
      <div class="people-quick-grid" role="radiogroup" :aria-label="t('gallery.peopleMode')">
        <button
          v-for="preset in peoplePresets"
          :key="preset"
          type="button"
          role="radio"
          :aria-checked="!peopleAdvanced && quickPeoplePreset === preset"
          :class="{ active: !peopleAdvanced && quickPeoplePreset === preset, many: preset === 'many' }"
          @click="choosePeoplePreset(preset); peopleAdvanced = false"
        >
          <span>{{ peoplePresetLabel(preset) }}</span>
          <small v-if="preset === 'many'">6+</small>
        </button>
      </div>
      <button type="button" class="people-mode-switch" :aria-expanded="peopleAdvanced" @click="peopleAdvanced = !peopleAdvanced">
        <el-icon aria-hidden="true"><Operation /></el-icon>
        {{ peopleAdvanced ? t('gallery.useQuickPeople') : t('gallery.useAdvancedPeople') }}
      </button>
      <div v-if="peopleAdvanced" class="people-advanced">
        <div class="people-modes" role="radiogroup" :aria-label="t('gallery.peopleMode')">
          <button v-for="option in peopleOptions" :key="option.value" type="button" role="radio" :aria-checked="modelValue.peopleMode === option.value" :class="{ active: modelValue.peopleMode === option.value }" @click="setPeopleMode(option.value)">{{ t(option.label) }}</button>
        </div>
        <el-input-number v-if="modelValue.peopleMode === 'exact'" :model-value="modelValue.peopleMin" class="people-number" :min="0" :max="100000" :precision="0" :aria-label="t('gallery.peopleExactLabel')" @update:model-value="setPeopleMin" />
        <div v-else-if="modelValue.peopleMode === 'range'" class="people-range">
          <el-input-number :model-value="modelValue.peopleMin" class="people-number" :min="0" :max="100000" :precision="0" :aria-label="t('gallery.peopleMin')" @update:model-value="setPeopleMin" @change="normalizePeopleRange" />
          <span aria-hidden="true">—</span>
          <el-input-number :model-value="modelValue.peopleMax" class="people-number" :min="0" :max="100000" :precision="0" :aria-label="t('gallery.peopleMax')" @update:model-value="setPeopleMax" @change="normalizePeopleRange" />
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.gallery-search-panel { display: grid; width: 100%; max-width: 720px; gap: 10px; margin-top: 12px; }
.color-filter-row { display: flex; align-items: center; gap: 10px; }
.color-filter-toggle { display: flex; min-width: 0; flex: 1; min-height: 42px; align-items: center; gap: 9px; padding: 0 12px; border: 1px solid var(--line); border-radius: 14px; background: var(--surface-overlay); color: var(--ink); font: inherit; font-size: 13px; text-align: left; cursor: pointer; }
.color-filter-current { display: inline-flex; align-items: center; gap: 6px; color: var(--muted); }
.color-filter-current i { width: 18px; height: 18px; border: 1px solid var(--line); border-radius: 50%; }
.color-filter-clear { padding: 8px 0; border: 0; background: transparent; color: var(--accent); font: inherit; font-size: 12px; cursor: pointer; }
.color-filter-toggle > .el-icon { margin-left: auto; color: var(--muted); }
.gallery-condition-tabs { display: grid; width: max-content; grid-template-columns: repeat(4, 48px); gap: 6px; padding: 4px; border: 1px solid color-mix(in srgb, var(--line) 82%, transparent); border-radius: 18px; background: color-mix(in srgb, var(--surface-overlay) 78%, transparent); box-shadow: inset 0 1px 0 var(--glass-edge); }
.gallery-condition-tab { position: relative; display: grid; width: 48px; height: 46px; place-items: center; border: 0; border-radius: 14px; background: transparent; color: var(--muted); cursor: pointer; transition: color .16s ease, background-color .16s ease, transform .16s ease; }
.gallery-condition-tab > .el-icon { font-size: 21px; }
.gallery-condition-tab:hover { background: var(--surface-soft); color: var(--ink); }
.gallery-condition-tab:active { transform: scale(.95); }
.gallery-condition-tab:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }
.gallery-condition-tab.active { background: var(--accent-soft); color: var(--accent); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--accent) 20%, transparent); }
.condition-active-dot { position: absolute; top: 8px; right: 8px; width: 7px; height: 7px; border-radius: 50%; background: var(--accent); box-shadow: 0 0 0 2px var(--surface-overlay); }
.gallery-condition-body { min-width: 0; padding: 14px; border: 1px solid color-mix(in srgb, var(--line) 68%, transparent); border-radius: 20px; background: color-mix(in srgb, var(--surface) 78%, transparent); }
.text-condition { display: grid; gap: 7px; }
.gallery-text-search :deep(.el-input__wrapper) { min-height: 48px; padding-inline: 16px; border-radius: 15px; background: var(--surface-overlay); box-shadow: inset 0 0 0 1px var(--line); }
.gallery-text-search :deep(.el-input__inner) { font-size: 16px; }
.text-condition p { margin: 0 2px; color: var(--muted); font-size: 12px; line-height: 1.5; }
.color-condition { position: relative; display: grid; grid-template-columns: 176px 44px minmax(0, 1fr); align-items: center; gap: 14px; }
.color-wheel { position: relative; width: 176px; height: 176px; flex: 0 0 auto; overflow: visible; border: 0; border-radius: 50%; background-color: white; background-image: radial-gradient(circle at center, rgb(255 255 255 / 100%) 0%, rgb(255 255 255 / 92%) 8%, rgb(255 255 255 / 58%) 38%, rgb(255 255 255 / 0%) 78%), conic-gradient(from 0deg, hsl(0 100% 50%), hsl(15 100% 50%) 15deg, hsl(30 100% 50%) 30deg, hsl(45 100% 50%) 45deg, hsl(60 100% 50%) 60deg, hsl(75 100% 50%) 75deg, hsl(90 100% 50%) 90deg, hsl(105 100% 50%) 105deg, hsl(120 100% 50%) 120deg, hsl(135 100% 50%) 135deg, hsl(150 100% 50%) 150deg, hsl(165 100% 50%) 165deg, hsl(180 100% 50%) 180deg, hsl(195 100% 50%) 195deg, hsl(210 100% 50%) 210deg, hsl(225 100% 50%) 225deg, hsl(240 100% 50%) 240deg, hsl(255 100% 50%) 255deg, hsl(270 100% 50%) 270deg, hsl(285 100% 50%) 285deg, hsl(300 100% 50%) 300deg, hsl(315 100% 50%) 315deg, hsl(330 100% 50%) 330deg, hsl(345 100% 50%) 345deg, hsl(360 100% 50%) 360deg); box-shadow: inset 0 0 0 1px rgb(255 255 255 / 80%), 0 12px 32px rgb(30 39 48 / 14%); cursor: crosshair; touch-action: none; user-select: none; }
.color-wheel:focus-visible { outline: 3px solid var(--focus); outline-offset: 4px; }
.color-wheel-marker { position: absolute; z-index: 2; width: 20px; height: 20px; border: 3px solid #fff; border-radius: 50%; background: var(--surface); box-shadow: 0 1px 7px rgb(0 0 0 / 36%); pointer-events: none; transform: translate(-50%, -50%); }
.color-wheel-loupe { position: absolute; z-index: 4; width: 72px; height: 72px; border: 4px solid rgb(255 255 255 / 94%); border-radius: 50%; box-shadow: 0 10px 26px rgb(0 0 0 / 28%), 0 0 0 1px rgb(0 0 0 / 12%); pointer-events: none; transform: translate(-50%, calc(-100% - 20px)); animation: loupe-in .14s ease-out; }
.color-wheel-loupe::after { position: absolute; right: 50%; bottom: -13px; width: 12px; height: 12px; border-right: 4px solid rgb(255 255 255 / 94%); border-bottom: 4px solid rgb(255 255 255 / 94%); background: inherit; content: ''; transform: translateX(50%) rotate(45deg); }
@keyframes loupe-in { from { opacity: 0; transform: translate(-50%, calc(-100% - 12px)) scale(.84); } to { opacity: 1; transform: translate(-50%, calc(-100% - 20px)) scale(1); } }
.color-brightness { display: grid; width: 44px; height: 176px; place-items: center; padding: 0; border: 0; background: transparent; cursor: ns-resize; touch-action: none; }
.color-brightness:focus-visible { outline: 3px solid var(--focus); outline-offset: 2px; border-radius: 999px; }
.color-brightness-track { position: relative; display: block; width: 18px; height: 100%; border: 1px solid rgb(255 255 255 / 72%); border-radius: 999px; background: linear-gradient(to top, #000 0%, var(--brightness-top) 100%); box-shadow: inset 0 0 0 1px rgb(0 0 0 / 18%), 0 8px 22px rgb(30 39 48 / 16%); }
.color-brightness-thumb { position: absolute; left: 50%; width: 30px; height: 12px; border: 3px solid #fff; border-radius: 999px; background: var(--surface-overlay); box-shadow: 0 1px 7px rgb(0 0 0 / 32%); pointer-events: none; transform: translate(-50%, 50%); }
.color-condition-copy { display: grid; min-width: 0; align-content: center; justify-items: start; gap: 11px; }
.condition-kicker { color: var(--muted); font-size: 12px; line-height: 1.55; }
.selected-color { display: flex; align-items: center; gap: 8px; color: var(--ink); font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 13px; font-weight: 600; }
.selected-color i { width: 22px; height: 22px; border: 1px solid var(--line); border-radius: 50%; }
.brightness-value { display: flex; gap: 8px; color: var(--muted); font-size: 11px; line-height: 1.4; }
.tag-condition { display: grid; gap: 10px; }
.tag-source-tabs, .tag-match, .people-modes { display: flex; align-items: center; gap: 6px; overflow-x: auto; scrollbar-width: none; }
.tag-source-tabs::-webkit-scrollbar, .tag-match::-webkit-scrollbar, .people-modes::-webkit-scrollbar, .tag-scroll::-webkit-scrollbar { display: none; }
.tag-source-tabs button, .tag-match button, .people-modes button, .condition-clear { min-height: 40px; padding: 0 13px; border: 1px solid transparent; border-radius: 999px; background: var(--surface-soft); color: var(--ink-soft); font: inherit; font-size: 12px; white-space: nowrap; cursor: pointer; }
.tag-source-tabs button.active, .tag-match button.active, .people-modes button.active { border-color: color-mix(in srgb, var(--accent) 32%, var(--line)); background: var(--accent-soft); color: var(--accent); }
.tag-scroll { display: flex; min-width: 0; gap: 8px; overflow-x: auto; padding: 2px 1px 4px; scroll-snap-type: x proximity; scrollbar-width: none; }
.tag-choice { display: inline-flex; min-height: 40px; flex: 0 0 auto; align-items: center; gap: 7px; padding: 0 13px; border: 1px solid var(--line); border-radius: 999px; background: color-mix(in srgb, var(--surface) 82%, transparent); color: var(--ink-soft); font: inherit; font-size: 12px; cursor: pointer; scroll-snap-align: start; }
.tag-choice small { padding: 1px 5px; border-radius: 999px; background: var(--surface-muted); color: var(--muted); font-size: 10px; line-height: 1.5; }
.tag-choice.ai small { background: color-mix(in srgb, var(--accent) 18%, transparent); color: var(--accent); }
.tag-choice.active { border-color: var(--accent); background: var(--accent-soft); color: var(--accent); }
.tag-match-row { display: flex; min-height: 40px; align-items: center; justify-content: space-between; gap: 10px; }
.condition-clear { margin-left: 0; background: transparent; color: var(--muted); }
.condition-clear:hover { color: var(--accent); }
.condition-status { display: flex; min-height: 44px; align-items: center; gap: 8px; color: var(--muted); font-size: 12px; }
.people-condition { display: grid; gap: 10px; }
.people-quick-grid { display: grid; grid-template-columns: repeat(8, minmax(48px, 1fr)); gap: 7px; }
.people-quick-grid button { display: inline-flex; min-height: 46px; align-items: center; justify-content: center; gap: 4px; padding: 0 8px; border: 1px solid transparent; border-radius: 14px; background: var(--surface-soft); color: var(--ink-soft); font: inherit; font-size: 12px; white-space: nowrap; cursor: pointer; }
.people-quick-grid button:hover { border-color: color-mix(in srgb, var(--accent) 22%, var(--line)); color: var(--ink); }
.people-quick-grid button.active { border-color: color-mix(in srgb, var(--accent) 36%, var(--line)); background: var(--accent-soft); color: var(--accent); font-weight: 600; }
.people-quick-grid button small { padding: 1px 4px; border-radius: 999px; background: color-mix(in srgb, currentColor 12%, transparent); font-size: 9px; }
.people-mode-switch { display: inline-flex; width: max-content; min-height: 40px; align-items: center; gap: 6px; padding: 0 10px; border: 0; border-radius: 12px; background: transparent; color: var(--muted); font: inherit; font-size: 12px; cursor: pointer; }
.people-mode-switch:hover, .people-mode-switch:focus-visible { background: var(--surface-soft); color: var(--accent); outline: none; }
.people-advanced { display: grid; gap: 10px; padding-top: 10px; border-top: 1px solid var(--line); }
.people-number { width: 132px; }
.people-number :deep(.el-input__wrapper) { min-height: 40px; border-radius: 12px; }
.people-range { display: flex; align-items: center; gap: 8px; color: var(--muted); }
.visually-hidden { position: absolute !important; width: 1px !important; height: 1px !important; overflow: hidden !important; clip: rect(0, 0, 0, 0) !important; white-space: nowrap !important; }
@media (max-width: 768px) {
  .gallery-search-panel { max-width: none; margin-top: 10px; }
  .gallery-condition-tabs { grid-template-columns: repeat(4, 46px); gap: 4px; padding: 3px; }
  .gallery-condition-tab { width: 46px; height: 44px; }
  .gallery-condition-body { padding: 12px; border-radius: 18px; }
  .color-condition { grid-template-columns: 146px 44px minmax(0, 1fr); gap: 10px; }
  .color-wheel { width: 146px; height: 146px; }
  .color-brightness { height: 146px; }
  .color-wheel-loupe { width: 64px; height: 64px; }
  .condition-kicker { line-height: 1.45; }
  .people-quick-grid { grid-template-columns: repeat(4, minmax(0, 1fr)); }
  .people-quick-grid button { min-height: 44px; }
}
@media (max-width: 380px) {
  .color-condition { grid-template-columns: 136px 44px minmax(0, 1fr); gap: 8px; }
  .color-wheel { width: 136px; height: 136px; }
  .color-brightness { height: 136px; }
}
@media (prefers-reduced-motion: reduce) {
  .gallery-condition-tab, .color-wheel-loupe { transition: none; animation: none; }
}
</style>
