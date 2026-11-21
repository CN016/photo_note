<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ArrowDown, ArrowUp, Download, Picture, Plus, Delete } from '@element-plus/icons-vue'
import Konva from 'konva'
import { exportFile, FileExportError } from '../platform'
import { analyzeImageColors, decodeImagePreview, releaseCanvas } from '../../../../packages/image-processing/src'

type Layout = 'square' | 'portrait' | 'landscape' | 'focus' | 'color-photo'
type Ratio = 'square' | 'portrait' | 'landscape'
type FitMode = 'cover' | 'contain'
type SavedPhoto = { id: string; name: string; blob: Blob; fit: FitMode }
type ColorPhotoTemplate = { source: SavedPhoto | null; palette: string[]; color: string; colorFirst: boolean }
const { t } = useI18n()
const photos = ref<SavedPhoto[]>([])
const layout = ref<Layout>('square')
const ratio = ref<Ratio>('square')
const gap = ref(16)
const background = ref('#f4f1ed')
const format = ref<'png' | 'jpeg'>('jpeg')
const selectedPhoto = ref<string | null>(null)
const colorPhoto = ref<ColorPhotoTemplate>({ source: null, palette: [], color: '#D9C5A4', colorFirst: false })
const analyzingColor = ref(false)
const exporting = ref(false)
const fileInput = ref<HTMLInputElement>()
const colorPhotoInput = ref<HTMLInputElement>()
const stageHost = ref<HTMLDivElement>()
const canvasHost = ref<HTMLDivElement>()
const layouts: Array<{ key: Layout; count: number }> = [{ key: 'square', count: 2 }, { key: 'portrait', count: 2 }, { key: 'landscape', count: 2 }, { key: 'focus', count: 3 }, { key: 'color-photo', count: 1 }]
const ratioNames: Ratio[] = ['square', 'portrait', 'landscape']
const ratioValue = computed(() => ratio.value === 'portrait' ? 0.76 : ratio.value === 'landscape' ? 1.42 : 1)
const columns = computed(() => layout.value === 'square' ? Math.ceil(Math.sqrt(Math.max(1, photos.value.length))) : layout.value === 'portrait' ? 2 : layout.value === 'landscape' ? Math.min(3, Math.max(1, photos.value.length)) : 2)
let stage: Konva.Stage | null = null
let layer: Konva.Layer | null = null
let observer: ResizeObserver | null = null
let renderGeneration = 0
let saveTimer = 0
const databaseName = 'photo-nini-local-collage'
let database: Promise<IDBDatabase> | null = null

/** IndexedDB 以 Blob 保存原图，不将大照片编码成体积膨胀的 Base64 文本。 */
function openDatabase(): Promise<IDBDatabase> {
  if (database) return database
  database = new Promise((resolve, reject) => {
    const request = indexedDB.open(databaseName, 1)
    request.onupgradeneeded = () => { if (!request.result.objectStoreNames.contains('projects')) request.result.createObjectStore('projects') }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
  return database
}
async function databaseRead(): Promise<unknown> {
  const db = await openDatabase()
  return new Promise((resolve, reject) => { const request = db.transaction('projects').objectStore('projects').get('current'); request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error) })
}
async function databaseWrite(value: unknown): Promise<void> {
  const db = await openDatabase()
  await new Promise<void>((resolve, reject) => { const request = db.transaction('projects', 'readwrite').objectStore('projects').put(value, 'current'); request.onsuccess = () => resolve(); request.onerror = () => reject(request.error) })
}
function loadImage(blob: Blob): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image(), url = URL.createObjectURL(blob)
    image.onload = () => { URL.revokeObjectURL(url); resolve(image) }
    image.onerror = () => { URL.revokeObjectURL(url); reject(new Error('decode')) }
    image.src = url
  })
}
async function addFiles(files: FileList | File[]) {
  const remaining = 12 - photos.value.length
  if (remaining <= 0) { ElMessage.info(t('collage.limit')); return }
  const selected = [...files].filter(file => /^image\/(png|jpeg|webp)$/i.test(file.type)).slice(0, remaining)
  if (selected.length !== Math.min([...files].filter(file => /^image\/(png|jpeg|webp)$/i.test(file.type)).length, remaining)) ElMessage.info(t('collage.limit'))
  const results = await Promise.allSettled(selected.map(async file => { const blob = file.slice(0, file.size, file.type); await loadImage(blob); return { id: crypto.randomUUID(), name: file.name, blob } }))
  photos.value = [...photos.value, ...results.flatMap(result => result.status === 'fulfilled' ? [{ ...result.value, fit: 'cover' as const }] : [])]
  if (results.some(result => result.status === 'rejected') || selected.length !== [...files].length) ElMessage.warning(t('collage.unsupported'))
}
/** 本地解码上传图并复用 image-processing 的有界采样颜色分析，分析画布随后立即释放。 */
async function createColorPhoto(files: FileList | File[]) {
  if (analyzingColor.value) return
  const file = [...files].find(item => /^image\/(png|jpeg|webp)$/i.test(item.type))
  if (!file) { ElMessage.warning(t('collage.unsupported')); return }
  analyzingColor.value = true
  try {
    const blob = file.slice(0, file.size, file.type)
    const decoded = await decodeImagePreview(blob, 1024)
    let palette: string[]
    try { palette = analyzeImageColors(decoded.canvas).palette.map(color => color.hex) }
    finally { releaseCanvas(decoded.canvas) }
    const source = { id: crypto.randomUUID(), name: file.name, blob, fit: 'cover' as const }
    colorPhoto.value = { source, palette, color: palette[0] ?? '#D9C5A4', colorFirst: false }
    photos.value = [source]
    layout.value = 'color-photo'
    selectedPhoto.value = null
  } catch { ElMessage.error(t('collage.colorAnalysisFailed')) }
  finally { analyzingColor.value = false }
}
function move(index: number, offset: number) {
  const next = index + offset
  if (next < 0 || next >= photos.value.length) return
  const copy = [...photos.value]; [copy[index], copy[next]] = [copy[next]!, copy[index]!]; photos.value = copy
}
/** 点选两张照片即可交换，避免拖拽跨格命中依赖不稳定的触屏拖放状态。 */
function selectPhoto(id: string) {
  if (selectedPhoto.value && selectedPhoto.value !== id) {
    const from = photos.value.findIndex(photo => photo.id === selectedPhoto.value)
    const to = photos.value.findIndex(photo => photo.id === id)
    if (from >= 0 && to >= 0) move(from, to - from)
    selectedPhoto.value = null
    return
  }
  selectedPhoto.value = id
}
function setFit(mode: FitMode) {
  photos.value = photos.value.map(photo => photo.id === selectedPhoto.value ? { ...photo, fit: mode } : photo)
}
function removePhoto(id: string) { photos.value = photos.value.filter(photo => photo.id !== id) }
function cropFor(image: HTMLImageElement, width: number, height: number) {
  const imageRatio = image.width / image.height, slotRatio = width / height
  if (imageRatio > slotRatio) { const cropWidth = image.height * slotRatio; return { x: (image.width - cropWidth) / 2, y: 0, width: cropWidth, height: image.height } }
  const cropHeight = image.width / slotRatio; return { x: 0, y: (image.height - cropHeight) / 2, width: image.width, height: cropHeight }
}
function cellBounds(index: number, count: number, width: number, height: number) {
  const spacing = gap.value, cols = columns.value, rows = Math.ceil(count / cols)
  if (layout.value === 'focus') {
    if (index === 0) return { x: 0, y: 0, width: width * 0.62 - spacing / 2, height }
    const rest = index - 1, colsRight = 2, rowsRight = Math.ceil((count - 1) / 2)
    const usable = width * 0.38 - spacing / 2
    return { x: width * 0.62 + (rest % colsRight) * usable / colsRight + spacing / 2, y: Math.floor(rest / colsRight) * height / rowsRight + spacing / 2, width: usable / colsRight - spacing, height: height / rowsRight - spacing }
  }
  return { x: (index % cols) * width / cols + spacing / 2, y: Math.floor(index / cols) * height / rows + spacing / 2, width: width / cols - spacing, height: height / rows - spacing }
}
function colorPhotoBounds(index: 0 | 1, width: number, height: number) {
  const colorIndex = colorPhoto.value.colorFirst ? 0 : 1
  const gapSize = Math.min(gap.value, height / 8)
  return { x: 0, y: index * height / 2 + gapSize / 2, width, height: height / 2 - gapSize, isColor: index === colorIndex }
}
function viewportSize() {
  const bounds = canvasHost.value?.getBoundingClientRect()
  const ratioNow = ratioValue.value
  // 用舞台实际内容宽度计算，不读取高度；避免高DPI或桌面纵向空间把交互画布扩展进右侧工具栏。
  const maxWidth = Math.max(220, Math.min((bounds?.width ?? 720) - 56, 760))
  const templateRatio = layout.value === 'color-photo' ? ratio.value === 'landscape' ? 1.42 : ratio.value === 'portrait' ? 0.76 : 0.9 : ratioNow
  const width = Math.min(maxWidth, 760 * Math.min(1, templateRatio)), height = width / templateRatio
  return { width: Math.round(width), height: Math.round(height) }
}
/** Konva 场景负责圆角裁切和鼠标/触控命中；照片次序与裁切模式由Vue状态保存。 */
async function renderScene() {
  if (!stage || !layer) return
  const generation = ++renderGeneration, size = viewportSize()
  stage.size(size); stage.clear(); layer.destroyChildren()
  layer.add(new Konva.Rect({ x: 0, y: 0, ...size, fill: background.value, listening: false }))
  if (layout.value === 'color-photo') {
    const template = colorPhoto.value
    for (const index of [0, 1] as const) {
      const bounds = colorPhotoBounds(index, size.width, size.height)
      const block = new Konva.Rect({ x: bounds.x, y: bounds.y, width: bounds.width, height: bounds.height, fill: bounds.isColor ? template.color : background.value, listening: true, name: `color-photo-block-${index}` })
      block.on('click tap', () => { colorPhoto.value = { ...colorPhoto.value, colorFirst: !colorPhoto.value.colorFirst } })
      layer.add(block)
    }
    if (template.source) {
      try {
        const image = await loadImage(template.source.blob)
        if (generation !== renderGeneration || !stage || !layer) return
        const bounds = colorPhotoBounds(template.colorFirst ? 1 : 0, size.width, size.height)
        const photo = new Konva.Image({ x: bounds.x, y: bounds.y, width: bounds.width, height: bounds.height, image, crop: cropFor(image, bounds.width, bounds.height), listening: true, name: 'color-photo-image' })
        photo.on('click tap', () => { colorPhoto.value = { ...colorPhoto.value, colorFirst: !colorPhoto.value.colorFirst } })
        layer.add(photo)
      } catch { /* 本地图片失效时保留可编辑色块，不清除用户的颜色选择。 */ }
    }
    layer.draw()
    return
  }
  const current = [...photos.value]
  const loaded = await Promise.all(current.map(async (photo, index) => {
    try { return { index, image: await loadImage(photo.blob), fit: photo.fit } } catch { return null }
  }))
  if (generation !== renderGeneration || !stage || !layer) return
  const targetLayer = layer
  loaded.forEach(item => {
    if (!item) return
    const slot = cellBounds(item.index, current.length, size.width, size.height)
    const imageBounds = item.fit === 'contain'
      ? (() => {
          const scale = Math.min(slot.width / item.image.width, slot.height / item.image.height)
          const width = item.image.width * scale, height = item.image.height * scale
          return { x: slot.x + (slot.width - width) / 2, y: slot.y + (slot.height - height) / 2, width, height }
        })()
      : slot
    if (item.fit === 'contain') targetLayer.add(new Konva.Rect({ ...slot, fill: background.value, cornerRadius: 8, listening: false }))
    const image = new Konva.Image({
      ...imageBounds,
      image: item.image,
      crop: item.fit === 'contain' ? { x: 0, y: 0, width: item.image.width, height: item.image.height } : cropFor(item.image, slot.width, slot.height),
      imageSmoothingEnabled: true,
      cornerRadius: 8,
      listening: true,
      name: `photo-${current[item.index]!.id}`,
    })
    image.on('click tap', () => { selectPhoto(current[item.index]!.id); targetLayer.find('.selected-outline').forEach(node => node.destroy()); if (selectedPhoto.value) targetLayer.add(new Konva.Rect({ x: slot.x, y: slot.y, width: slot.width, height: slot.height, stroke: '#3378d4', strokeWidth: 3, listening: false, name: 'selected-outline' })); targetLayer.batchDraw() })
    targetLayer.add(image)
  })
  targetLayer.draw()
}
async function exportImage() {
  if (!photos.value.length || exporting.value || !stage) return
  exporting.value = true
  try {
    // Konva导出按像素比重绘场景，保留交互预览相同的裁切和布局。图片解码结束后再编码。
    await renderScene()
    const dataUrl = stage.toDataURL({ pixelRatio: 3, mimeType: format.value === 'png' ? 'image/png' : 'image/jpeg', quality: 0.94 })
    const blob = await (await fetch(dataUrl)).blob()
    await exportFile(blob, `lightnote-collage.${format.value}`)
    ElMessage.success(t('collage.saved'))
  } catch (error) { if (!(error instanceof FileExportError)) ElMessage.error(t('collage.saveFailed')) }
  finally { exporting.value = false }
}
async function restore() {
  try {
    const value = await databaseRead() as { photos?: SavedPhoto[]; layout?: Layout; ratio?: Ratio; gap?: number; background?: string; format?: 'png' | 'jpeg'; colorPhoto?: ColorPhotoTemplate } | undefined
    if (!value || !Array.isArray(value.photos) || value.photos.length > 12 || value.photos.some(photo => !(photo.blob instanceof Blob))) return
    photos.value = value.photos; if (value.layout && layouts.some(item => item.key === value.layout)) layout.value = value.layout
    if (value.colorPhoto && Array.isArray(value.colorPhoto.palette) && /^#[0-9a-f]{6}$/i.test(value.colorPhoto.color)) colorPhoto.value = value.colorPhoto
    if (value.ratio && ratioNames.includes(value.ratio)) ratio.value = value.ratio
    if (typeof value.gap === 'number') gap.value = Math.max(0, Math.min(40, value.gap))
    if (value.background && /^#[0-9a-f]{6}$/i.test(value.background)) background.value = value.background
    if (value.format === 'png' || value.format === 'jpeg') format.value = value.format
    if (photos.value.length) ElMessage.info(t('collage.restore'))
  } catch { /* 浏览器本地空间不可用时仍允许当前会话临时编辑。 */ }
}
watch([photos, layout, ratio, gap, background, selectedPhoto], () => {
  void renderScene()
  window.clearTimeout(saveTimer)
  saveTimer = window.setTimeout(() => { void databaseWrite({ photos: photos.value, layout: layout.value, ratio: ratio.value, gap: gap.value, background: background.value, format: format.value, colorPhoto: colorPhoto.value }).catch(() => { /* 持久空间不足时保留本页编辑，不伪称草稿已保存。 */ }) }, 350)
}, { deep: true })
watch(colorPhoto, () => {
  if (layout.value === 'color-photo') void renderScene()
  window.clearTimeout(saveTimer)
  saveTimer = window.setTimeout(() => { void databaseWrite({ photos: photos.value, layout: layout.value, ratio: ratio.value, gap: gap.value, background: background.value, format: format.value, colorPhoto: colorPhoto.value }).catch(() => undefined) }, 350)
}, { deep: true })
watch(format, () => { window.clearTimeout(saveTimer); saveTimer = window.setTimeout(() => { void databaseWrite({ photos: photos.value, layout: layout.value, ratio: ratio.value, gap: gap.value, background: background.value, format: format.value }).catch(() => undefined) }, 350) })
onMounted(async () => {
  await nextTick()
  if (!canvasHost.value || !stageHost.value) return
  stage = new Konva.Stage({ container: canvasHost.value, width: 1, height: 1, pixelRatio: 1 })
  layer = new Konva.Layer(); stage.add(layer)
  observer = new ResizeObserver(() => { void renderScene() }); observer.observe(stageHost.value)
  await restore(); await renderScene()
})
onBeforeUnmount(() => { renderGeneration++; observer?.disconnect(); window.clearTimeout(saveTimer); stage?.destroy(); stage = null; layer = null })
</script>

<template>
  <div class="workspace-page collage-page">
    <main id="main-content" class="collage-main">
      <header class="collage-heading"><div><h1>{{ t('collage.title') }}</h1><p>{{ t('collage.subtitle') }}</p></div><span class="photo-count">{{ t('collage.photoCount', { count: photos.length }) }}</span></header>
      <div class="collage-workspace">
        <div ref="stageHost" class="collage-stage" :aria-label="t('collage.title')" @dragover.prevent @drop.prevent="event => { const files = (event as DragEvent).dataTransfer?.files; if (files) void addFiles(files) }">
          <div ref="canvasHost" class="collage-canvas" aria-hidden="true"></div>
          <div v-if="!photos.length" class="collage-empty"><span class="empty-icon"><el-icon><Picture /></el-icon></span><h2>{{ t('collage.emptyTitle') }}</h2><p>{{ t('collage.emptyBody') }}</p><button class="primary-button" @click="fileInput?.click()"><el-icon><Plus /></el-icon>{{ t('collage.addPhotos') }}</button></div>
        </div>
        <aside class="collage-tools">
          <input ref="fileInput" class="sr-only" type="file" accept="image/png,image/jpeg,image/webp" multiple @change="event => { const input = event.target as HTMLInputElement; if (input.files) void addFiles(input.files); input.value = '' }" />
          <input ref="colorPhotoInput" class="sr-only" type="file" accept="image/png,image/jpeg,image/webp" @change="event => { const input = event.target as HTMLInputElement; if (input.files) void createColorPhoto(input.files); input.value = '' }" />
          <button class="add-button" :disabled="photos.length >= 12" @click="fileInput?.click()"><el-icon><Plus /></el-icon>{{ t('collage.addPhotos') }}</button>
          <section class="tool-group"><h2>{{ t('collage.layout') }}</h2><div class="layout-list"><button v-for="item in layouts" :key="item.key" type="button" :class="['layout-option', { selected: layout === item.key }]" :disabled="item.key === 'color-photo' ? false : photos.length < item.count" :aria-pressed="layout === item.key" :title="t(`collage.layouts.${item.key}`)" @click="item.key === 'color-photo' ? (colorPhoto.source ? layout = item.key : colorPhotoInput?.click()) : layout = item.key"><span :class="['layout-glyph', `glyph-${item.key}`]"><i v-for="cell in (item.key === 'focus' ? 3 : item.key === 'square' ? 4 : 2)" :key="cell" /></span><small>{{ t(`collage.layouts.${item.key}`) }}</small></button></div></section>
          <section v-if="layout !== 'color-photo'" class="tool-group"><h2>{{ t('collage.ratio') }}</h2><div class="segmented"><button v-for="item in ratioNames" :key="item" :aria-pressed="ratio === item" @click="ratio = item">{{ t(`collage.ratios.${item}`) }}</button></div></section>
          <section v-if="layout !== 'color-photo'" class="tool-group"><label class="range-label" for="collage-gap">{{ t('collage.spacing') }}<span>{{ gap }} px</span></label><input id="collage-gap" v-model.number="gap" type="range" min="0" max="40" /></section>
          <section v-if="layout !== 'color-photo'" class="tool-group color-group"><label for="collage-background">{{ t('collage.background') }}</label><input id="collage-background" v-model="background" type="color" /></section>
          <section v-if="layout === 'color-photo'" class="tool-group theme-color-tools"><div class="photo-list-heading"><h2>{{ t('collage.themeColor') }}</h2><button type="button" class="quiet-button" @click="colorPhoto.colorFirst = !colorPhoto.colorFirst">{{ t('collage.swapHalves') }}</button></div><div class="palette-list"><button v-for="color in colorPhoto.palette" :key="color" type="button" :style="{ backgroundColor: color }" :aria-label="t('collage.useThemeColor', { color })" :aria-pressed="colorPhoto.color === color" @click="colorPhoto.color = color"></button><label class="custom-color"><span>{{ t('collage.customColor') }}</span><input v-model="colorPhoto.color" type="color" /></label></div><button type="button" class="secondary-button" @click="colorPhotoInput?.click()"><el-icon><Plus /></el-icon>{{ t('collage.replaceColorPhoto') }}</button><span class="palette-hint">{{ t('collage.topFiveHint') }}</span></section>
          <section v-if="photos.length" class="tool-group"><div class="photo-list-heading"><h2>{{ t('collage.photoCount', { count: photos.length }) }}</h2><button type="button" class="quiet-button" :aria-label="t('collage.clear')" :title="t('collage.clear')" @click="photos = []; selectedPhoto = null"><el-icon><Delete /></el-icon></button></div><ul class="photo-list"><li v-for="(photo, index) in photos" :key="photo.id" :class="{ 'photo-selected': photo.id === selectedPhoto }" @click="selectPhoto(photo.id)"><span class="photo-thumb"><el-icon><Picture /></el-icon></span><span :title="photo.name">{{ photo.name }}</span><button :disabled="index === 0" :aria-label="t('collage.moveLeft')" @click.stop="move(index, -1)"><el-icon><ArrowUp /></el-icon></button><button :disabled="index === photos.length - 1" :aria-label="t('collage.moveRight')" @click.stop="move(index, 1)"><el-icon><ArrowDown /></el-icon></button><button :aria-label="t('collage.remove')" @click.stop="removePhoto(photo.id); if (selectedPhoto === photo.id) selectedPhoto = null"><el-icon><Delete /></el-icon></button></li></ul><div v-if="selectedPhoto" class="crop-tools"><span>{{ t('collage.cropSelected') }}</span><button type="button" :aria-pressed="photos.find(photo => photo.id === selectedPhoto)?.fit === 'cover'" @click="setFit('cover')">{{ t('collage.fill') }}</button><button type="button" :aria-pressed="photos.find(photo => photo.id === selectedPhoto)?.fit === 'contain'" @click="setFit('contain')">{{ t('collage.restoreCrop') }}</button></div></section>
          <section class="export-row"><label class="format-select" :aria-label="t('collage.format')"><select v-model="format"><option value="jpeg">{{ t('collage.jpeg') }}</option><option value="png">{{ t('collage.png') }}</option></select></label><button class="primary-button export-button" :disabled="!photos.length || exporting" @click="exportImage"><el-icon><Download /></el-icon>{{ t(exporting ? 'collage.exporting' : 'collage.export') }}</button></section>
        </aside>
      </div>
    </main>
  </div>
</template>

<style scoped>
.collage-main { width: min(calc(100% - 310px), 1450px); min-height: calc(100svh - 72px); padding: 34px 24px 100px; margin: 0 auto 0 calc(226px + (100vw - 226px - min(100vw - 310px, 1450px)) / 2); color: var(--ink); }
.collage-heading { display:flex; align-items:flex-end; justify-content:space-between; gap:16px; margin-bottom:24px; }
.collage-heading h1 { margin:0; font-size:clamp(26px,3vw,36px); font-weight:500; letter-spacing:-.035em; }
.collage-heading p { margin:8px 0 0; color:var(--muted); font-size:14px; }
.photo-count { color:var(--muted); font-size:14px; white-space:nowrap; }
.collage-workspace { display:grid; grid-template-columns:minmax(0,1fr) 310px; align-items:start; gap:20px; }
.collage-stage { position:relative; display:flex; min-width:0; min-height:min(72vh,760px); align-items:center; justify-content:center; padding:24px; border:1px solid var(--line); border-radius:24px; background:var(--surface-soft); overflow:hidden; }
.collage-canvas { position:absolute; inset:24px; display:grid; place-items:center; pointer-events:none; }
.collage-canvas :deep(.konvajs-content) { position:relative !important; flex:0 1 auto; max-width:100%; max-height:100%; pointer-events:auto; touch-action:none; }
.collage-empty { display:grid; justify-items:center; text-align:center; }
.empty-icon { display:grid; width:60px; height:60px; place-items:center; border-radius:18px; background:var(--surface); color:var(--accent-hover); font-size:25px; }
.collage-empty h2 { margin:20px 0 0; font-size:19px; font-weight:500; }
.collage-empty p { margin:8px 0 20px; color:var(--muted); font-size:14px; }
.collage-tools { position:sticky; top:16px; display:grid; gap:18px; padding:20px; border:1px solid var(--line); border-radius:20px; background:var(--surface); }
.tool-group { display:grid; gap:10px; }
.tool-group h2,.photo-list-heading h2 { margin:0; font-size:13px; font-weight:600; }
.add-button,.primary-button { display:flex; min-height:46px; align-items:center; justify-content:center; gap:9px; border:0; border-radius:12px; background:var(--accent); color:#fff; font:inherit; font-size:14px; cursor:pointer; }
.add-button:disabled,.primary-button:disabled { opacity:.45; cursor:not-allowed; }
.layout-list { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:8px; }
.layout-option { display:grid; min-width:0; justify-items:center; gap:7px; padding:8px 3px; border:1px solid var(--line); border-radius:12px; background:var(--surface); color:var(--ink); cursor:pointer; }
.layout-option.selected { border-color:var(--accent); background:var(--accent-soft); }
.layout-option:disabled { opacity:.4; cursor:not-allowed; }
.layout-option small { font-size:11px; }
.layout-glyph { display:grid; width:30px; height:28px; gap:2px; grid-template-columns:repeat(2,1fr); grid-template-rows:repeat(2,1fr); }
.layout-glyph i { display:block; border-radius:2px; background:var(--muted); opacity:.55; }
.selected .layout-glyph i { background:var(--accent); opacity:1; }
.glyph-portrait i:nth-child(odd) { grid-row:span 2; }
.glyph-landscape { grid-template-columns:repeat(3,1fr); }
.glyph-landscape i { grid-column:span 2; }
.glyph-landscape i:nth-child(2) { grid-column:span 1; grid-row:span 2; }
.glyph-focus i:first-child { grid-row:span 2; }
.glyph-color-photo { grid-template-columns:1fr; grid-template-rows:repeat(2,1fr); }
.glyph-color-photo i:first-child { background:#D8C7A8; }
.segmented { display:grid; grid-template-columns:repeat(3,1fr); padding:3px; border-radius:11px; background:var(--surface-soft); }
.segmented button { min-height:36px; border:0; border-radius:8px; background:transparent; color:var(--muted); font:inherit; font-size:12px; cursor:pointer; }
.segmented button[aria-pressed="true"] { background:var(--surface); color:var(--ink); box-shadow:0 1px 4px #00000014; }
.range-label,.color-group { display:flex; align-items:center; justify-content:space-between; gap:12px; color:var(--ink); font-size:13px; }
.range-label span { color:var(--muted); font-variant-numeric:tabular-nums; }
input[type="range"] { width:100%; accent-color:var(--accent); }
.color-group input { width:42px; height:34px; padding:2px; border:1px solid var(--line); border-radius:8px; background:var(--surface); }
.photo-list-heading { display:flex; align-items:center; justify-content:space-between; }
.quiet-button,.photo-list li button { display:grid; width:34px; height:34px; place-items:center; border:0; border-radius:9px; background:var(--surface-soft); color:var(--muted); cursor:pointer; }
.photo-list { display:grid; max-height:220px; gap:6px; overflow:auto; padding:0; margin:0; list-style:none; }
.photo-list li { display:flex; min-width:0; align-items:center; gap:4px; }
.photo-list li.photo-selected { border-radius:10px; outline:2px solid var(--accent); outline-offset:1px; }
.photo-thumb { display:grid; width:34px; height:34px; flex:0 0 auto; place-items:center; border-radius:7px; background:var(--surface-soft); color:var(--muted); }
.photo-list li span:nth-child(2) { min-width:0; flex:1; overflow:hidden; padding:0 5px; color:var(--muted); font-size:11px; text-overflow:ellipsis; white-space:nowrap; }
.photo-list li button { width:30px; height:30px; flex:0 0 auto; }
.photo-list li button:disabled { opacity:.35; cursor:default; }
.crop-tools { display:flex; flex-wrap:wrap; align-items:center; gap:7px; padding-top:8px; border-top:1px solid var(--line); }
.crop-tools > span { flex-basis:100%; color:var(--muted); font-size:12px; }
.crop-tools button { min-height:38px; padding:0 10px; border:1px solid var(--line); border-radius:9px; background:var(--surface); color:var(--ink); font:inherit; font-size:12px; cursor:pointer; }
.crop-tools button[aria-pressed="true"] { border-color:var(--accent); background:var(--accent-soft); }
.theme-color-tools { padding-top:10px; border-top:1px solid var(--line); }
.theme-color-tools .photo-list-heading { gap:8px; }
.theme-color-tools .quiet-button { width:auto; min-width:0; flex:0 0 auto; padding:0 10px; white-space:nowrap; }
.palette-list { display:flex; flex-wrap:wrap; align-items:center; gap:10px; }
.palette-list > button { width:34px; height:34px; border:2px solid var(--surface); border-radius:50%; box-shadow:0 0 0 1px var(--line); cursor:pointer; }
.palette-list > button[aria-pressed="true"] { box-shadow:0 0 0 2px var(--accent); }
.custom-color { display:flex; min-height:38px; align-items:center; gap:8px; color:var(--muted); font-size:12px; }
.custom-color input { width:38px; height:34px; padding:2px; border:1px solid var(--line); border-radius:8px; background:var(--surface); }
.palette-hint { color:var(--muted); font-size:11px; }
.secondary-button { display:flex; min-height:40px; align-items:center; justify-content:center; gap:8px; border:1px solid var(--line); border-radius:10px; background:var(--surface); color:var(--ink); font:inherit; font-size:13px; cursor:pointer; }
.export-row { display:grid; grid-template-columns:84px minmax(0,1fr); gap:8px; padding-top:4px; }
.format-select select { width:100%; height:46px; border:1px solid var(--line); border-radius:11px; background:var(--surface); color:var(--ink); font:inherit; }
.export-button { white-space:nowrap; }
.sr-only { position:absolute; width:1px; height:1px; overflow:hidden; clip:rect(0,0,0,0); }
@media(max-width:1000px) { .collage-main { width:calc(100% - 226px); margin-left:226px; padding:30px 18px 100px; } .collage-workspace { grid-template-columns:minmax(0,1fr) 280px; gap:14px; } .collage-tools { padding:16px; } }
@media(max-width:768px) { .collage-main { width:100%; margin:0; padding:24px 16px 108px; } .collage-workspace { grid-template-columns:1fr; gap:14px; } .collage-stage { min-height:38vh; padding:18px; border-radius:18px; } .collage-tools { position:static; grid-template-columns:1fr 1fr; align-items:start; padding:16px; gap:16px 14px; } .add-button,.photo-list,.export-row { grid-column:1/-1; } .tool-group h2,.photo-list-heading h2 { font-size:12px; } }
@media(max-width:430px) { .collage-tools { grid-template-columns:1fr; } .collage-tools > * { grid-column:1; } .collage-heading { align-items:center; } .collage-heading p { font-size:12px; } }
</style>
