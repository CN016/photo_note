<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppHeader from '../components/AppHeader.vue'
import { api } from '../api/client'
import { ApiError, type InspirationRecord } from '../api/types'
import {
  anchorToView, createEditorDocument, cropOnRotatedCanvas, DocumentHistory, parseEditorDocument, viewSelectionToSourceRect,
  viewToSource, type EditorDocument, type ImageAnchor, type NormalizedRect,
} from '../../../../packages/editor-core/src'
import { decodeImagePreview, makeWorkingJpeg, releaseCanvas, renderDocumentPreview } from '../../../../packages/image-processing/src'

type TransformAction = 'rotate' | 'crop' | 'reset'
type RGBChannel = 'red' | 'green' | 'blue'
interface AnchorDraft { id: string | null; x: number; y: number; note: string }

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const itemId = computed(() => typeof route.params.id === 'string' ? route.params.id : '')
const isExisting = computed(() => itemId.value.length > 0)
const documentData = ref<EditorDocument>(createEditorDocument())
const sourceCanvas = shallowRef<HTMLCanvasElement | null>(null)
const previewCanvas = ref<HTMLCanvasElement | null>(null)
const stageBox = ref<HTMLElement | null>(null)
const record = shallowRef<InspirationRecord | null>(null)
const imageFile = shallowRef<File | null>(null)
const filename = ref('')
const imageReady = ref(false)
const downsampled = ref(false)
const loading = ref(false)
const saving = ref(false)
const busyPhase = ref('')
const error = ref('')
const status = ref('')
const anchorMode = ref(false)
const cropMode = ref(false)
const transformDialog = ref<TransformAction | null>(null)
const anchorDialog = ref(false)
const anchorDraft = ref<AnchorDraft | null>(null)
const historyRevision = ref(0)
const cropSelection = ref<NormalizedRect | null>(null)
const fitBox = ref({ width: 0, height: 0 })
let history: DocumentHistory | null = null
let frameRequest = 0
let decodeVersion = 0
let uploadSourceKey = ''
let uploadWorkKey = ''
let rgbBefore: string | null = null
let resizeObserver: ResizeObserver | null = null
let selectionStart: { x: number; y: number } | null = null

const canUndo = computed(() => { historyRevision.value; return history?.canUndo ?? false })
const canRedo = computed(() => { historyRevision.value; return history?.canRedo ?? false })
const displayDimensions = computed(() => {
  const source = sourceCanvas.value
  if (!source) return { width: 0, height: 0 }
  const rotatedWidth = documentData.value.rotation === 90 || documentData.value.rotation === 270 ? source.height : source.width
  const rotatedHeight = documentData.value.rotation === 90 || documentData.value.rotation === 270 ? source.width : source.height
  const crop = cropOnRotatedCanvas(documentData.value)
  const width = Math.max(1, Math.round(rotatedWidth * crop.width))
  const height = Math.max(1, Math.round(rotatedHeight * crop.height))
  const scale = Math.min(1, fitBox.value.width / width || 1, fitBox.value.height / height || 1)
  return { width: Math.max(1, Math.round(width * scale)), height: Math.max(1, Math.round(height * scale)) }
})
const visibleAnchors = computed(() => documentData.value.anchors.flatMap((anchor, index) => {
  const position = anchorToView(anchor, documentData.value)
  return position.visible ? [{ ...anchor, index, left: `${position.x * 100}%`, top: `${position.y * 100}%` }] : []
}))
const cropDragStyle = computed(() => {
  const selection = cropSelection.value
  if (!selection) return {}
  return { left: `${selection.x * 100}%`, top: `${selection.y * 100}%`, width: `${selection.width * 100}%`, height: `${selection.height * 100}%` }
})

/** 将后端错误码映射到页面语言资源，不直接展示未经本地化的服务端文本。 */
function localError(cause: unknown, fallback: string): string {
  if (cause instanceof ApiError) {
    if (cause.code === 'network') return t('common.errors.network')
    const key = `common.errors.${cause.code}`
    const translated = t(key)
    if (translated !== key) return translated
  }
  return fallback
}

/** 每次提交建立一条轻量文档历史，不复制 canvas 像素或原图文件。 */
function commit(next: EditorDocument): void {
  history ??= new DocumentHistory(documentData.value)
  documentData.value = history.commit(next)
  historyRevision.value += 1
  scheduleRender()
}

function undo(): void {
  finishRgbChange()
  if (!history?.canUndo) return
  documentData.value = history.undo()
  historyRevision.value += 1
  scheduleRender()
}

function redo(): void {
  finishRgbChange()
  if (!history?.canRedo) return
  documentData.value = history.redo()
  historyRevision.value += 1
  scheduleRender()
}

/** 颜色滑杆的连续输入只在拖动结束时写入一次历史，预览仍随输入更新。 */
function updateRgb(channel: RGBChannel, value: string): void {
  rgbBefore ??= JSON.stringify(documentData.value)
  documentData.value = { ...documentData.value, rgb: { ...documentData.value.rgb, [channel]: Number(value) } }
  scheduleRender()
}

function finishRgbChange(): void {
  if (rgbBefore === null) return
  const before = rgbBefore
  rgbBefore = null
  if (before !== JSON.stringify(documentData.value)) commit(documentData.value)
}

/** 编辑新图时只解码成有限尺寸预览；浏览器原始 File 保持独立不变。 */
async function selectFile(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  const version = ++decodeVersion
  error.value = ''
  status.value = ''
  imageReady.value = false
  resizeObserver?.disconnect()
  resizeObserver = null
  fitBox.value = { width: 0, height: 0 }
  imageFile.value = file
  filename.value = file.name
  uploadSourceKey = ''
  uploadWorkKey = ''
  releaseCanvas(sourceCanvas.value)
  sourceCanvas.value = null
  documentData.value = createEditorDocument()
  history = new DocumentHistory(documentData.value)
  historyRevision.value += 1
  loading.value = true
  try {
    const decoded = await decodeImagePreview(file)
    if (version !== decodeVersion) { releaseCanvas(decoded.canvas); return }
    sourceCanvas.value = decoded.canvas
    downsampled.value = decoded.wasDownsampled
    imageReady.value = true
    scheduleRender()
    await nextTick()
    observeStage()
  } catch {
    if (version === decodeVersion) error.value = t('editor.decodeFailed')
  } finally { if (version === decodeVersion) loading.value = false }
}

/** 打开既有记录时优先读取原图扩展 key，旧记录则兼容使用 image_key。 */
async function loadExisting(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const loaded = await api.inspiration(itemId.value)
    record.value = loaded
    filename.value = loaded.file_name || t('gallery.untitled')
    documentData.value = parseEditorDocument(loaded.data)
    history = new DocumentHistory(documentData.value)
    const sourceKey = typeof loaded.data.source_image_key === 'string' ? loaded.data.source_image_key : loaded.image_key
    const original = await api.download(sourceKey)
    const decoded = await decodeImagePreview(original)
    sourceCanvas.value = decoded.canvas
    downsampled.value = decoded.wasDownsampled
    imageReady.value = true
    scheduleRender()
    await nextTick()
    observeStage()
  } catch (cause) { error.value = localError(cause, t('editor.loadError')) }
  finally { loading.value = false }
}

/** 画布更新合并到下一帧，避免 RGB 拖动时重复做高分辨率像素循环。 */
function scheduleRender(): void {
  if (frameRequest) cancelAnimationFrame(frameRequest)
  frameRequest = requestAnimationFrame(() => {
    frameRequest = 0
    const source = sourceCanvas.value
    const target = previewCanvas.value
    if (!source || !target || !imageReady.value) return
    try {
      const output = renderDocumentPreview(source, documentData.value)
      target.width = output.width
      target.height = output.height
      const context = target.getContext('2d')
      if (context) context.drawImage(output, 0, 0)
      releaseCanvas(output)
    } catch { error.value = t('editor.decodeFailed') }
  })
}

/** 按用户当前拖动框反算原图坐标；坐标语义不随旋转角度改变。 */
function stagePoint(event: PointerEvent): { x: number; y: number } {
  const bounds = previewCanvas.value?.getBoundingClientRect()
  if (!bounds) return { x: 0, y: 0 }
  return { x: Math.max(0, Math.min(1, (event.clientX - bounds.left) / bounds.width)), y: Math.max(0, Math.min(1, (event.clientY - bounds.top) / bounds.height)) }
}

/** 仅在裁切模式记录起点，并捕获当前指针以支持拖出画布边缘。 */
function beginCropSelection(event: PointerEvent): void {
  if (!cropMode.value || !previewCanvas.value) return
  selectionStart = stagePoint(event)
  cropSelection.value = { x: selectionStart.x, y: selectionStart.y, width: 0, height: 0 }
  previewCanvas.value.setPointerCapture(event.pointerId)
}

/** 随指针移动更新归一化裁切框，不复制或重采样原图像素。 */
function moveCropSelection(event: PointerEvent): void {
  if (!cropMode.value || !selectionStart) return
  const point = stagePoint(event)
  cropSelection.value = { x: Math.min(selectionStart.x, point.x), y: Math.min(selectionStart.y, point.y), width: Math.abs(point.x - selectionStart.x), height: Math.abs(point.y - selectionStart.y) }
}

/** 指针抬起时固定最终裁切框，供用户确认或取消。 */
function endCropSelection(event: PointerEvent): void {
  if (!cropMode.value || !selectionStart) return
  moveCropSelection(event)
  selectionStart = null
}

/** 鼠标或触控点击图片时创建带原图归一化坐标的备注锚点。 */
function onStageClick(event: MouseEvent): void {
  if (!anchorMode.value || cropMode.value) return
  const bounds = previewCanvas.value?.getBoundingClientRect()
  if (!bounds) return
  if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) return
  const point = { x: Math.max(0, Math.min(1, (event.clientX - bounds.left) / bounds.width)), y: Math.max(0, Math.min(1, (event.clientY - bounds.top) / bounds.height)) }
  const source = viewToSource(point, documentData.value)
  anchorDraft.value = { id: null, x: source.x, y: source.y, note: '' }
  anchorDialog.value = true
}

/** 键盘用户聚焦预览画布并按 Enter/Space 时，在画面中心添加备注锚点。 */
function addAnchorAtCenter(): void {
  if (!anchorMode.value || cropMode.value) return
  const source = viewToSource({ x: 0.5, y: 0.5 }, documentData.value)
  anchorDraft.value = { id: null, x: source.x, y: source.y, note: '' }
  anchorDialog.value = true
}

/** 把已保存锚点载入备注对话框，供用户修改或删除。 */
function editAnchor(anchor: ImageAnchor): void {
  anchorDraft.value = { ...anchor }
  anchorDialog.value = true
}

/** 新增、更新、删除备注都写入同一份可撤销的编辑文档历史。 */
function saveAnchor(): void {
  if (!anchorDraft.value?.note.trim()) { error.value = t('editor.anchorRequired'); return }
  const draft = { ...anchorDraft.value, note: anchorDraft.value.note.trim() }
  const next = { ...documentData.value, anchors: [...documentData.value.anchors] }
  const existingId = draft.id
  if (existingId) next.anchors = next.anchors.map(anchor => anchor.id === existingId ? { ...draft, id: existingId } : anchor)
  else next.anchors.push({ ...draft, id: makeAnchorId() })
  commit(next)
  anchorDialog.value = false
  anchorDraft.value = null
  anchorMode.value = false
  error.value = ''
}

/** 删除当前备注锚点并写入编辑历史，用户可通过撤销恢复。 */
function deleteAnchor(): void {
  const id = anchorDraft.value?.id
  if (!id) { anchorDialog.value = false; anchorDraft.value = null; return }
  commit({ ...documentData.value, anchors: documentData.value.anchors.filter(anchor => anchor.id !== id) })
  anchorDialog.value = false
  anchorDraft.value = null
}

/** 生成锚点 ID；旧浏览器缺少 randomUUID 时使用本地随机回退。 */
function makeAnchorId(): string {
  return typeof crypto.randomUUID === 'function' ? crypto.randomUUID() : `anchor-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

/** 有锚点时先显示三选一确认；没有锚点则直接开始操作。 */
function prepareTransform(action: TransformAction): void {
  if (documentData.value.anchors.length) transformDialog.value = action
  else performTransform(action, false)
}

function performTransform(action: TransformAction, clearAnchors: boolean): void {
  const base = { ...documentData.value, anchors: clearAnchors ? [] : [...documentData.value.anchors] }
  transformDialog.value = null
  if (action === 'rotate') {
    commit({ ...base, rotation: ((base.rotation + 90) % 360) as EditorDocument['rotation'] })
    return
  }
  if (action === 'reset') {
    commit({ ...base, crop: { x: 0, y: 0, width: 1, height: 1 } })
    cropMode.value = false
    cropSelection.value = null
    return
  }
  if (clearAnchors) commit(base)
  cropMode.value = true
  anchorMode.value = false
  cropSelection.value = null
}

/** 将屏幕选区映射回方向校正后的原图坐标并作为一条撤销命令保存。 */
function applyCrop(): void {
  const selection = cropSelection.value
  if (!selection || selection.width < 0.04 || selection.height < 0.04) { error.value = t('editor.cropTooSmall'); return }
  const crop = viewSelectionToSourceRect({ x: selection.x, y: selection.y }, { x: selection.x + selection.width, y: selection.y + selection.height }, documentData.value)
  commit({ ...documentData.value, crop })
  cropMode.value = false
  cropSelection.value = null
  error.value = ''
}

/** 取消当前裁切手势并移除临时选区，不改变已保存文档。 */
function cancelCrop(): void { cropMode.value = false; cropSelection.value = null; selectionStart = null }

/** 新图先上传原始字节，再上传独立预览副本，最后创建灵感图记录。 */
async function save(): Promise<void> {
  error.value = ''
  status.value = ''
  if (!isExisting.value && (!imageFile.value || !sourceCanvas.value)) { error.value = t('editor.imageRequired'); return }
  saving.value = true
  try {
    if (isExisting.value) {
      busyPhase.value = t('editor.saving')
      await api.updateInspiration(itemId.value, documentData.value as Record<string, unknown>)
    } else {
      const file = imageFile.value!
      if (!uploadSourceKey) {
        busyPhase.value = t('editor.uploadingOriginal')
        uploadSourceKey = (await api.upload(file, file.name)).storekey
      }
      if (!uploadWorkKey) {
        busyPhase.value = t('editor.uploadingWorking')
        const working = await makeWorkingJpeg(sourceCanvas.value!)
        const baseName = file.name.replace(/\.[^.]+$/, '') || 'inspiration'
        uploadWorkKey = (await api.upload(working, `${baseName}.jpg`)).storekey
      }
      busyPhase.value = t('editor.creatingRecord')
      const data = { ...documentData.value, source_image_key: uploadSourceKey }
      const created = await api.createInspiration(uploadWorkKey, file.name, data)
      await router.replace({ name: 'edit-inspiration', params: { id: created.business_id } })
    }
    status.value = t('editor.saved')
  } catch (cause) {
    error.value = localError(cause, t('editor.saveFailed'))
  } finally { saving.value = false; busyPhase.value = '' }
}

/** 按实际画布容器尺寸更新预览适配范围，并监听布局或屏幕旋转变化。 */
function observeStage(): void {
  if (!stageBox.value) return
  resizeObserver?.disconnect()
  const update = (width: number, height: number) => { fitBox.value = { width, height }; scheduleRender() }
  update(stageBox.value.clientWidth, stageBox.value.clientHeight)
  resizeObserver = new ResizeObserver(entries => {
    const box = entries[0]?.contentRect
    if (box) update(box.width, box.height)
  })
  resizeObserver.observe(stageBox.value)
}

onMounted(() => {
  observeStage()
  if (isExisting.value) void loadExisting()
  else { history = new DocumentHistory(documentData.value); historyRevision.value += 1 }
})

onBeforeUnmount(() => {
  decodeVersion += 1
  if (frameRequest) cancelAnimationFrame(frameRequest)
  resizeObserver?.disconnect()
  releaseCanvas(sourceCanvas.value)
  if (previewCanvas.value) { previewCanvas.value.width = 0; previewCanvas.value.height = 0 }
})
</script>

<template>
  <div class="workspace-page editor-page">
    <AppHeader />
    <main class="editor-main">
      <div class="editor-heading-row">
        <div class="editor-title-group">
          <RouterLink class="back-link" to="/inspirations"><span aria-hidden="true">←</span>{{ t('common.back') }}</RouterLink>
          <div><p class="eyebrow">{{ t('common.product') }}</p><h1>{{ isExisting ? t('editor.editTitle') : t('editor.newTitle') }}</h1></div>
        </div>
        <p class="editor-subtitle">{{ t('editor.subtitle') }}</p>
      </div>

      <section v-if="!isExisting && !imageReady" class="upload-welcome">
        <label class="upload-dropzone">
          <input class="sr-only" type="file" accept="image/*,.heic,.heif" :aria-label="t('editor.fileLabel')" @change="selectFile" />
          <span class="upload-icon" aria-hidden="true">↥</span><strong>{{ t('editor.chooseImage') }}</strong><span>{{ t('editor.chooseHint') }}</span>
        </label>
        <p v-if="loading" class="loading-inline" role="status"><span class="loading-dot"></span>{{ t('common.loading') }}</p>
      </section>

      <div v-else-if="imageReady" class="editor-workspace">
        <section class="editor-canvas-column" :aria-label="t('editor.previewLabel')">
          <div class="canvas-toolbar">
            <span class="filename-chip"><span aria-hidden="true">▧</span>{{ filename }}</span>
            <div class="history-actions">
              <button class="icon-button" type="button" :disabled="!canUndo" :aria-label="t('editor.undo')" :title="t('editor.undo')" @click="undo">↶</button>
              <button class="icon-button" type="button" :disabled="!canRedo" :aria-label="t('editor.redo')" :title="t('editor.redo')" @click="redo">↷</button>
            </div>
          </div>
          <div ref="stageBox" class="canvas-stage" :class="{ 'anchor-active': anchorMode, 'crop-active': cropMode }" @click="onStageClick">
            <div v-if="imageReady" class="editor-canvas-wrap" :style="{ width: `${displayDimensions.width}px`, height: `${displayDimensions.height}px` }">
              <canvas ref="previewCanvas" class="editor-canvas" role="img" tabindex="0" :aria-label="anchorMode ? t('editor.anchorModeOn') : t('editor.previewLabel')" @keydown.enter.prevent="addAnchorAtCenter" @keydown.space.prevent="addAnchorAtCenter" @pointerdown="beginCropSelection" @pointermove="moveCropSelection" @pointerup="endCropSelection" @pointercancel="endCropSelection"></canvas>
              <div v-if="cropMode && cropSelection" class="crop-selection" :style="cropDragStyle" aria-hidden="true"></div>
              <button v-for="anchor in visibleAnchors" :key="anchor.id" class="image-anchor" type="button" :style="{ left: anchor.left, top: anchor.top }" :aria-label="t('editor.anchorLabel', { number: anchor.index + 1, note: anchor.note })" @click.stop="editAnchor(anchor)">{{ anchor.index + 1 }}</button>
            </div>
            <div v-else class="loading-inline" role="status"><span class="loading-dot"></span>{{ t('common.loading') }}</div>
          </div>
          <div v-if="cropMode" class="crop-action-row"><span>{{ t('editor.cropHint') }}</span><button class="secondary-button compact-button" type="button" @click="cancelCrop">{{ t('common.cancel') }}</button><button class="primary-button compact-button" type="button" :disabled="!cropSelection" @click="applyCrop">{{ t('editor.cropApply') }}</button></div>
          <p v-else-if="anchorMode" class="stage-hint">{{ t('editor.anchorModeOn') }}</p>
          <p v-if="downsampled" class="stage-note">{{ t('editor.largeImage') }}</p>
        </section>

        <aside class="editor-controls" :aria-label="t('editor.editTitle')">
          <section class="control-section transform-section">
            <div class="section-title-row"><h2>{{ t('editor.editTitle') }}</h2><span class="section-step">01</span></div>
            <div class="transform-buttons">
              <button class="tool-button" type="button" :disabled="cropMode" @click="prepareTransform('rotate')"><span aria-hidden="true">⟳</span>{{ t('editor.rotate') }}</button>
              <button class="tool-button" type="button" :disabled="cropMode" @click="prepareTransform('crop')"><span aria-hidden="true">⌗</span>{{ t('editor.crop') }}</button>
              <button class="tool-button" type="button" :disabled="cropMode" @click="prepareTransform('reset')"><span aria-hidden="true">⤢</span>{{ t('editor.resetCrop') }}</button>
            </div>
          </section>

          <section class="control-section color-section">
            <div class="section-title-row"><h2>{{ t('editor.rgb') }}</h2><span class="section-step">02</span></div>
            <label class="color-slider red-slider"><span>{{ t('editor.red') }}</span><input type="range" min="-100" max="100" step="1" :value="documentData.rgb.red" :aria-label="t('editor.red')" @input="updateRgb('red', ($event.target as HTMLInputElement).value)" @change="finishRgbChange" /><output>{{ documentData.rgb.red }}</output></label>
            <label class="color-slider green-slider"><span>{{ t('editor.green') }}</span><input type="range" min="-100" max="100" step="1" :value="documentData.rgb.green" :aria-label="t('editor.green')" @input="updateRgb('green', ($event.target as HTMLInputElement).value)" @change="finishRgbChange" /><output>{{ documentData.rgb.green }}</output></label>
            <label class="color-slider blue-slider"><span>{{ t('editor.blue') }}</span><input type="range" min="-100" max="100" step="1" :value="documentData.rgb.blue" :aria-label="t('editor.blue')" @input="updateRgb('blue', ($event.target as HTMLInputElement).value)" @change="finishRgbChange" /><output>{{ documentData.rgb.blue }}</output></label>
          </section>

          <section class="control-section notes-section">
            <div class="section-title-row"><div><h2>{{ t('editor.anchors') }}</h2><span class="notes-count">{{ documentData.anchors.length }}</span></div><span class="section-step">03</span></div>
            <button class="note-mode-button" type="button" :class="{ active: anchorMode }" :disabled="cropMode" @click="anchorMode = !anchorMode"><span aria-hidden="true">＋</span>{{ anchorMode ? t('editor.anchorModeOn') : t('editor.anchorMode') }}</button>
            <p v-if="!documentData.anchors.length" class="notes-empty">{{ t('editor.anchorEmpty') }}</p>
            <div v-else class="note-list">
              <button v-for="(anchor, index) in documentData.anchors" :key="anchor.id" class="note-list-item" type="button" @click="editAnchor(anchor)"><span class="note-index">{{ index + 1 }}</span><span>{{ anchor.note }}</span><span class="note-arrow" aria-hidden="true">↗</span></button>
            </div>
          </section>
          <p class="source-preserved"><span aria-hidden="true">◌</span>{{ t('editor.sourcePreserved') }}</p>
          <p v-if="error" class="form-error editor-error" role="alert">{{ error }}</p>
          <p v-if="status" class="success-message" role="status">{{ status }}</p>
          <button class="primary-button save-button desktop-save" type="button" :disabled="saving || loading || cropMode || anchorDialog || !!transformDialog" @click="save">{{ saving ? t('editor.saving') : !isExisting && uploadSourceKey ? t('editor.retry') : isExisting ? t('editor.saveEdit') : t('editor.saveNew') }}<span aria-hidden="true">↗</span></button>
          <p v-if="busyPhase" class="save-progress" role="status"><span class="loading-dot"></span>{{ busyPhase }}</p>
        </aside>
      </div>

      <div v-if="error && !imageReady" class="editor-load-error" role="alert"><p>{{ error }}</p><RouterLink class="secondary-button" to="/inspirations">{{ t('common.back') }}</RouterLink></div>
      <div v-if="imageReady" class="mobile-save-bar"><span v-if="error" class="mobile-save-error" role="alert">{{ error }}</span><span v-else-if="busyPhase" class="mobile-save-progress" role="status">{{ busyPhase }}</span><button class="primary-button save-button" type="button" :disabled="saving || loading || cropMode || anchorDialog || !!transformDialog" @click="save">{{ saving ? t('editor.saving') : !isExisting && uploadSourceKey ? t('editor.retry') : isExisting ? t('editor.saveEdit') : t('editor.saveNew') }}<span aria-hidden="true">↗</span></button></div>
    </main>

    <div v-if="transformDialog" class="dialog-backdrop" @click.self="transformDialog = null">
      <section class="decision-dialog" role="dialog" aria-modal="true" aria-labelledby="transform-title">
        <span class="dialog-icon" aria-hidden="true">⌖</span><p class="eyebrow">{{ t('editor.anchors') }}</p><h2 id="transform-title">{{ t('editor.transformTitle') }}</h2><p>{{ t('editor.transformText') }}</p>
        <div class="dialog-actions"><button class="text-button" type="button" @click="transformDialog = null">{{ t('common.cancel') }}</button><button class="secondary-button" type="button" @click="performTransform(transformDialog!, false)">{{ t('editor.keepAnchors') }}</button><button class="primary-button" type="button" @click="performTransform(transformDialog!, true)">{{ t('editor.clearAnchors') }}</button></div>
      </section>
    </div>

    <div v-if="anchorDialog && anchorDraft" class="dialog-backdrop" @click.self="anchorDialog = false">
      <section class="note-dialog" role="dialog" aria-modal="true" :aria-label="anchorDraft.id ? t('editor.anchorEdit') : t('editor.anchorMode')">
        <div class="dialog-heading"><span class="dialog-icon" aria-hidden="true">{{ anchorDraft.id ? '✎' : '＋' }}</span><button class="dialog-close" type="button" :aria-label="t('common.close')" @click="anchorDialog = false">×</button></div>
        <label class="field"><span>{{ t('editor.anchorEdit') }}</span><textarea v-model="anchorDraft.note" rows="4" maxlength="500" :placeholder="t('editor.anchorPlaceholder')" autofocus></textarea></label>
        <p v-if="error" class="form-error" role="alert">{{ error }}</p>
        <div class="dialog-actions"><button v-if="anchorDraft.id" class="danger-text-button" type="button" @click="deleteAnchor">{{ t('editor.anchorDelete') }}</button><span class="dialog-spacer"></span><button class="text-button" type="button" @click="anchorDialog = false">{{ t('common.cancel') }}</button><button class="primary-button" type="button" @click="saveAnchor">{{ t('editor.anchorSave') }}</button></div>
      </section>
    </div>
  </div>
</template>
