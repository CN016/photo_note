<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowDown, Brush, Check, CollectionTag, Filter, Search, User } from '@element-plus/icons-vue'
import type { TagRecord } from '../api/types'
import {
  type GalleryPeopleMode,
  type GallerySearchKind,
  type GallerySearchState,
  type GalleryTagSource,
  gallerySearchHasValue,
  switchGallerySearchKind,
} from '../gallerySearch'

const props = defineProps<{
  modelValue: GallerySearchState
  tags: TagRecord[]
  tagsLoading?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: GallerySearchState] }>()
const { t, locale } = useI18n()
const menuOpen = ref(false)
const wheel = ref<HTMLButtonElement>()

const conditionOptions = [
  { kind: 'text' as const, icon: Search, label: 'gallery.conditionText' },
  { kind: 'color' as const, icon: Brush, label: 'gallery.conditionColor' },
  { kind: 'tags' as const, icon: CollectionTag, label: 'gallery.conditionTags' },
  { kind: 'people' as const, icon: User, label: 'gallery.conditionPeople' },
]
const peopleOptions: Array<{ value: GalleryPeopleMode; label: string }> = [
  { value: 'present', label: 'gallery.peoplePresent' },
  { value: 'absent', label: 'gallery.peopleAbsent' },
  { value: 'exact', label: 'gallery.peopleExact' },
  { value: 'range', label: 'gallery.peopleRange' },
]
const tagSources: Array<{ value: GalleryTagSource; label: string }> = [
  { value: 'all', label: 'gallery.allTagSources' },
  { value: 'manual', label: 'gallery.manualOnly' },
  { value: 'ai', label: 'gallery.aiOnly' },
]
const neutralColors = [
  { value: '#111111', label: 'gallery.library.colorNames.black' },
  { value: '#ffffff', label: 'gallery.library.colorNames.white' },
  { value: '#888888', label: 'gallery.library.colorNames.gray' },
]
const currentOption = computed(() => conditionOptions.find(option => option.kind === props.modelValue.kind) || conditionOptions[0]!)
const tagsByID = computed(() => new Map(props.tags.map(tag => [tag.business_id, tag])))
const selectedTags = computed(() => props.modelValue.tagIds.map(id => tagsByID.value.get(id)).filter((tag): tag is TagRecord => Boolean(tag)))
const visibleTags = computed(() => props.tags.filter(tag => {
  if (props.modelValue.tagSource === 'all') return true
  if (props.modelValue.tagSource === 'manual') return tag.source === 'manual' || tag.source === 'mixed'
  return tag.source === 'ai' || tag.source === 'mixed'
}))
const hasValue = computed(() => gallerySearchHasValue(props.modelValue))

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
  menuOpen.value = false
  if (kind !== props.modelValue.kind) emit('update:modelValue', switchGallerySearchKind(props.modelValue, kind))
}

function summary(): string {
  if (props.modelValue.kind === 'text') return props.modelValue.query.trim() || t('gallery.searchPlaceholder')
  if (props.modelValue.kind === 'color') return props.modelValue.color || t('gallery.chooseColor')
  if (props.modelValue.kind === 'tags') {
    if (!props.modelValue.tagIds.length) return t('gallery.chooseTags')
    return new Intl.ListFormat(locale.value, { style: 'short', type: 'conjunction' }).format(selectedTags.value.map(tag => tag.content))
  }
  return t(`gallery.peopleSummary.${props.modelValue.peopleMode}`)
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

function normalizePeopleRange() {
  const peopleMin = Math.max(0, Math.min(100000, Math.round(props.modelValue.peopleMin || 0)))
  const peopleMax = Math.max(peopleMin, Math.min(100000, Math.round(props.modelValue.peopleMax || peopleMin)))
  patch({ peopleMin, peopleMax })
}

function chooseColor(color: string) {
  patch({ color })
}

/** 色环将指针位置转换为色相与饱和度，圆心为白色、边缘为高饱和色。 */
function chooseWheelColor(event: MouseEvent) {
  // 键盘触发按钮 click 时没有指针坐标，方向键逻辑单独处理，避免误选圆心白色。
  if (event.detail === 0) return
  const element = event.currentTarget as HTMLElement
  const rect = element.getBoundingClientRect()
  const radius = rect.width / 2
  const dx = event.clientX - (rect.left + radius)
  const dy = event.clientY - (rect.top + radius)
  const distance = Math.min(radius, Math.hypot(dx, dy))
  const hue = (Math.atan2(dy, dx) * 180 / Math.PI + 90 + 360) % 360
  chooseColor(hsvToHex(hue, distance / radius, 1))
}

/** 键盘左右方向键调整色相，上下方向键调整饱和度，保证色环不只能依赖指针。 */
function wheelKeydown(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key)) return
  event.preventDefault()
  const hsv = hexToHsv(props.modelValue.color || '#ff3b30')
  if (event.key === 'ArrowLeft') hsv.hue = (hsv.hue + 355) % 360
  if (event.key === 'ArrowRight') hsv.hue = (hsv.hue + 5) % 360
  if (event.key === 'ArrowUp') hsv.saturation = Math.min(1, hsv.saturation + .05)
  if (event.key === 'ArrowDown') hsv.saturation = Math.max(0, hsv.saturation - .05)
  chooseColor(hsvToHex(hsv.hue, hsv.saturation, 1))
}

const wheelMarker = computed(() => {
  if (!/^#[0-9a-f]{6}$/i.test(props.modelValue.color)) return null
  const hsv = hexToHsv(props.modelValue.color)
  if (hsv.value < .15 || hsv.saturation < .03) return null
  const angle = (hsv.hue - 90) * Math.PI / 180
  const radius = hsv.saturation * 50
  return { left: `${50 + Math.cos(angle) * radius}%`, top: `${50 + Math.sin(angle) * radius}%` }
})

function hsvToHex(hue: number, saturation: number, value: number): string {
  const chroma = value * saturation
  const segment = hue / 60
  const secondary = chroma * (1 - Math.abs(segment % 2 - 1))
  const [red, green, blue] = segment < 1 ? [chroma, secondary, 0]
    : segment < 2 ? [secondary, chroma, 0]
      : segment < 3 ? [0, chroma, secondary]
        : segment < 4 ? [0, secondary, chroma]
          : segment < 5 ? [secondary, 0, chroma]
            : [chroma, 0, secondary]
  const offset = value - chroma
  return `#${[red, green, blue].map(channel => Math.round((channel + offset) * 255).toString(16).padStart(2, '0')).join('')}`
}

function hexToHsv(value: string): { hue: number; saturation: number; value: number } {
  const match = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(value)
  if (!match) return { hue: 0, saturation: 1, value: 1 }
  const red = Number.parseInt(match[1]!, 16) / 255
  const green = Number.parseInt(match[2]!, 16) / 255
  const blue = Number.parseInt(match[3]!, 16) / 255
  const maximum = Math.max(red, green, blue)
  const minimum = Math.min(red, green, blue)
  const delta = maximum - minimum
  let hue = 0
  if (delta) {
    if (maximum === red) hue = 60 * (((green - blue) / delta) % 6)
    else if (maximum === green) hue = 60 * ((blue - red) / delta + 2)
    else hue = 60 * ((red - green) / delta + 4)
  }
  return { hue: (hue + 360) % 360, saturation: maximum ? delta / maximum : 0, value: maximum }
}
</script>

<template>
  <section class="gallery-search-panel" :aria-label="t('gallery.searchConditions')">
    <div class="gallery-search-box" :class="{ 'has-condition': modelValue.kind !== 'text' }">
      <el-input
        v-if="modelValue.kind === 'text'"
        :model-value="modelValue.query"
        class="gallery-search"
        :aria-label="t('gallery.searchPlaceholder')"
        :placeholder="t('gallery.searchPlaceholder')"
        clearable
        @update:model-value="setQuery"
      >
        <template #prefix><el-icon aria-hidden="true"><Search /></el-icon></template>
      </el-input>
      <button v-else type="button" class="gallery-condition-summary" :aria-label="summary()" @click="menuOpen = true">
        <el-icon aria-hidden="true"><component :is="currentOption.icon" /></el-icon>
        <span><strong>{{ t(currentOption.label) }}</strong><small>{{ summary() }}</small></span>
      </button>
      <el-popover v-model:visible="menuOpen" trigger="click" placement="bottom-end" :width="248" :teleported="false" popper-class="gallery-condition-popper">
        <template #reference>
          <button type="button" class="gallery-condition-button" :aria-label="t('gallery.openSearchConditions')" :aria-expanded="menuOpen">
            <el-icon aria-hidden="true"><Filter /></el-icon>
            <span v-if="hasValue && modelValue.kind !== 'text'" class="condition-active-dot" aria-hidden="true" />
            <el-icon class="condition-chevron" :class="{ expanded: menuOpen }" aria-hidden="true"><ArrowDown /></el-icon>
          </button>
        </template>
        <div class="condition-menu" role="menu" :aria-label="t('gallery.searchConditions')">
          <button v-for="option in conditionOptions" :key="option.kind" type="button" role="menuitemradio" :aria-checked="modelValue.kind === option.kind" :class="{ active: modelValue.kind === option.kind }" @click="chooseKind(option.kind)">
            <el-icon aria-hidden="true"><component :is="option.icon" /></el-icon>
            <span>{{ t(option.label) }}</span>
            <el-icon v-if="modelValue.kind === option.kind" class="condition-check" aria-hidden="true"><Check /></el-icon>
          </button>
        </div>
      </el-popover>
    </div>

    <div v-if="modelValue.kind === 'color'" class="gallery-condition-body color-condition">
      <button
        ref="wheel"
        type="button"
        class="color-wheel"
        role="slider"
        :aria-label="t('gallery.colorWheel')"
        aria-valuemin="0"
        aria-valuemax="359"
        :aria-valuenow="modelValue.color ? Math.round(hexToHsv(modelValue.color).hue) : 0"
        :aria-valuetext="modelValue.color || t('gallery.chooseColor')"
        @click="chooseWheelColor"
        @keydown="wheelKeydown"
      >
        <span v-if="wheelMarker" class="color-wheel-marker" :style="wheelMarker" />
      </button>
      <div class="color-condition-copy">
        <span class="condition-kicker">{{ t('gallery.colorSimilarity') }}</span>
        <strong class="selected-color">
          <i :style="{ background: modelValue.color || 'transparent' }" aria-hidden="true" />
          {{ modelValue.color || t('gallery.chooseColor') }}
        </strong>
        <div class="neutral-colors" :aria-label="t('gallery.neutralColors')">
          <button v-for="color in neutralColors" :key="color.value" type="button" :class="{ active: modelValue.color.toLowerCase() === color.value }" :aria-label="t(color.label)" :style="{ '--swatch': color.value }" @click="chooseColor(color.value)" />
        </div>
      </div>
    </div>

    <div v-else-if="modelValue.kind === 'tags'" class="gallery-condition-body tag-condition">
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

    <div v-else-if="modelValue.kind === 'people'" class="gallery-condition-body people-condition">
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
  </section>
</template>

<style scoped>
.gallery-search-panel { display: grid; width: 100%; max-width: 720px; gap: 10px; margin-top: 12px; }
.gallery-search-box { display: grid; min-width: 0; grid-template-columns: minmax(0, 1fr) 48px; overflow: hidden; border: 1px solid color-mix(in srgb, var(--line) 88%, transparent); border-radius: 999px; background: color-mix(in srgb, var(--surface-overlay) 82%, transparent); box-shadow: inset 0 1px 0 var(--glass-edge), 0 7px 22px rgb(55 64 51 / 6%); }
.gallery-search-box:focus-within { border-color: color-mix(in srgb, var(--accent) 55%, var(--line)); box-shadow: inset 0 1px 0 var(--glass-edge), 0 0 0 3px color-mix(in srgb, var(--accent) 14%, transparent); }
.gallery-search { min-width: 0; }
.gallery-search :deep(.el-input__wrapper) { min-height: 46px; padding: 0 10px 0 18px; border-radius: 999px 0 0 999px; background: transparent !important; box-shadow: none !important; }
.gallery-search :deep(.el-input__inner) { font-size: 16px; }
.gallery-condition-summary { display: flex; min-width: 0; min-height: 46px; align-items: center; gap: 10px; padding: 0 14px 0 18px; border: 0; background: transparent; color: var(--ink); text-align: left; cursor: pointer; }
.gallery-condition-summary > .el-icon { flex: 0 0 auto; color: var(--accent); font-size: 18px; }
.gallery-condition-summary > span { display: grid; min-width: 0; gap: 1px; }
.gallery-condition-summary strong { font-size: 13px; font-weight: 600; }
.gallery-condition-summary small { overflow: hidden; color: var(--muted); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.gallery-condition-button { position: relative; display: grid; width: 48px; height: 100%; min-height: 46px; place-items: center; border: 0; background: transparent; color: var(--ink-soft); cursor: pointer; }
.gallery-condition-button:hover { background: color-mix(in srgb, var(--accent-soft) 45%, transparent); color: var(--accent); }
.gallery-condition-button:focus-visible, .gallery-condition-summary:focus-visible { outline: 2px solid var(--focus); outline-offset: -3px; }
.gallery-condition-button > .el-icon:first-child { font-size: 18px; }
.condition-chevron { position: absolute; right: 7px; bottom: 6px; font-size: 10px !important; transition: transform .16s ease; }
.condition-chevron.expanded { transform: rotate(180deg); }
.condition-active-dot { position: absolute; top: 9px; right: 9px; width: 7px; height: 7px; border-radius: 50%; background: var(--accent); box-shadow: 0 0 0 2px var(--surface-overlay); }
.condition-menu { display: grid; gap: 4px; }
.condition-menu button { display: grid; min-height: 44px; grid-template-columns: 24px minmax(0, 1fr) 20px; align-items: center; gap: 10px; padding: 0 12px; border: 0; border-radius: 12px; background: transparent; color: var(--ink-soft); font: inherit; font-size: 13px; text-align: left; cursor: pointer; }
.condition-menu button:hover, .condition-menu button:focus-visible { background: var(--surface-soft); color: var(--ink); outline: none; }
.condition-menu button.active { background: var(--accent-soft); color: var(--accent); }
.condition-check { justify-self: end; }
.gallery-condition-body { min-width: 0; padding: 13px 14px; border: 1px solid color-mix(in srgb, var(--line) 68%, transparent); border-radius: 20px; background: color-mix(in srgb, var(--surface) 76%, transparent); }
.color-condition { display: flex; align-items: center; gap: 22px; }
.color-wheel { position: relative; width: 168px; height: 168px; flex: 0 0 auto; border: 0; border-radius: 50%; background: radial-gradient(circle, #fff 0%, rgb(255 255 255 / 0) 72%), conic-gradient(from 0deg, #ff3b30, #ffcc00 60deg, #34c759 120deg, #32ade6 180deg, #5856d6 240deg, #ff2d55 300deg, #ff3b30 360deg); box-shadow: inset 0 0 0 1px rgb(255 255 255 / 75%), 0 10px 28px rgb(30 39 48 / 12%); cursor: crosshair; touch-action: manipulation; }
.color-wheel:focus-visible { outline: 3px solid var(--focus); outline-offset: 4px; }
.color-wheel-marker { position: absolute; width: 18px; height: 18px; border: 3px solid #fff; border-radius: 50%; background: var(--surface); box-shadow: 0 1px 6px rgb(0 0 0 / 28%); pointer-events: none; transform: translate(-50%, -50%); }
.color-condition-copy { display: grid; min-width: 0; gap: 10px; }
.condition-kicker { color: var(--muted); font-size: 12px; line-height: 1.55; }
.selected-color { display: flex; align-items: center; gap: 8px; color: var(--ink); font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 13px; font-weight: 600; }
.selected-color i { width: 20px; height: 20px; border: 1px solid var(--line); border-radius: 50%; background-image: linear-gradient(45deg, var(--surface-muted) 25%, transparent 25%, transparent 75%, var(--surface-muted) 75%), linear-gradient(45deg, var(--surface-muted) 25%, transparent 25%, transparent 75%, var(--surface-muted) 75%); background-position: 0 0, 4px 4px; background-size: 8px 8px; box-shadow: inset 0 0 0 999px var(--swatch, transparent); }
.neutral-colors { display: flex; gap: 8px; }
.neutral-colors button { width: 34px; height: 34px; border: 2px solid var(--surface); border-radius: 50%; background: var(--swatch); box-shadow: 0 0 0 1px var(--line); cursor: pointer; }
.neutral-colors button.active { box-shadow: 0 0 0 2px var(--accent); }
.tag-condition { display: grid; gap: 10px; }
.tag-source-tabs, .tag-match, .people-modes { display: flex; align-items: center; gap: 6px; overflow-x: auto; scrollbar-width: none; }
.tag-source-tabs::-webkit-scrollbar, .tag-match::-webkit-scrollbar, .people-modes::-webkit-scrollbar, .tag-scroll::-webkit-scrollbar { display: none; }
.tag-source-tabs button, .tag-match button, .people-modes button, .condition-clear { min-height: 36px; padding: 0 13px; border: 1px solid transparent; border-radius: 999px; background: var(--surface-soft); color: var(--ink-soft); font: inherit; font-size: 12px; white-space: nowrap; cursor: pointer; }
.tag-source-tabs button.active, .tag-match button.active, .people-modes button.active { border-color: color-mix(in srgb, var(--accent) 32%, var(--line)); background: var(--accent-soft); color: var(--accent); }
.tag-scroll { display: flex; min-width: 0; gap: 8px; overflow-x: auto; padding: 2px 1px 4px; scroll-snap-type: x proximity; scrollbar-width: none; }
.tag-choice { display: inline-flex; min-height: 38px; flex: 0 0 auto; align-items: center; gap: 7px; padding: 0 13px; border: 1px solid var(--line); border-radius: 999px; background: color-mix(in srgb, var(--surface) 82%, transparent); color: var(--ink-soft); font: inherit; font-size: 12px; cursor: pointer; scroll-snap-align: start; }
.tag-choice small { padding: 1px 5px; border-radius: 999px; background: var(--surface-muted); color: var(--muted); font-size: 10px; line-height: 1.5; }
.tag-choice.ai small { background: color-mix(in srgb, var(--accent) 18%, transparent); color: var(--accent); }
.tag-choice.active { border-color: var(--accent); background: var(--accent-soft); color: var(--accent); }
.tag-match-row { display: flex; min-height: 36px; align-items: center; justify-content: space-between; gap: 10px; }
.condition-clear { margin-left: auto; background: transparent; color: var(--muted); }
.condition-clear:hover { color: var(--accent); }
.condition-status { display: flex; min-height: 44px; align-items: center; gap: 8px; color: var(--muted); font-size: 12px; }
.people-condition { display: flex; align-items: center; gap: 16px; }
.people-number { width: 132px; }
.people-number :deep(.el-input__wrapper) { min-height: 38px; border-radius: 12px; }
.people-range { display: flex; align-items: center; gap: 8px; color: var(--muted); }
@media (max-width: 768px) {
  .gallery-search-panel { max-width: none; margin-top: 10px; }
  .gallery-search-box { grid-template-columns: minmax(0, 1fr) 44px; }
  .gallery-search :deep(.el-input__wrapper), .gallery-condition-summary { min-height: 44px; }
  .gallery-search :deep(.el-input__inner) { font-size: 16px; }
  .gallery-condition-button { width: 44px; min-height: 44px; }
  .gallery-condition-body { padding: 12px; border-radius: 18px; }
  .color-condition { gap: 14px; }
  .color-wheel { width: 132px; height: 132px; }
  .condition-kicker { display: none; }
  .people-condition { align-items: flex-start; flex-direction: column; gap: 10px; }
  .tag-condition { gap: 8px; }
}
@media (max-width: 380px) {
  .color-wheel { width: 112px; height: 112px; }
  .color-condition { gap: 10px; }
  .neutral-colors button { width: 30px; height: 30px; }
}
</style>
