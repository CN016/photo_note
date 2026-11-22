<script setup lang="ts">
import { loadDetailImage, loadManagedDetailImage, takeDetailRecord } from '../images/detailImages'
import EditorToolSurface from '../components/EditorToolSurface.vue'
import { captureDetailImage, copyVisibleImage, detailImageReady } from '../motion/imageMotion'
import {
  PINCH_FULLSCREEN_THRESHOLD, pinchReturnFrame, pinchTargetScale, pinchZoomFrame, pointDistance, shouldReturnFromPinch,
  type PinchFrame, type PinchPoints, type ZoomRect,
} from '../images/pinchZoom'
import ContentZoom from '../components/ContentZoom.vue'
import { createLongPress } from '../context-menu/longPress'
import { friendlyExtra, formatMetadataValue as formatImageMetadataValue, rawMetadataValue, gpsMapUrl } from './imageMetadataDisplay'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { gsap } from 'gsap'
import { useI18n } from 'vue-i18n'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox, type UploadFile } from 'element-plus'
import { ArrowLeft, ArrowRight, Check, Close, Crop, Delete, FullScreen, Plus, RefreshLeft, RefreshRight, UploadFilled, MoreFilled, Aim, Operation, Document } from '@element-plus/icons-vue'
import { api as baseApi } from '../api/client'
import { ApiError, type InspirationImageMetadata, type InspirationRecord } from '../api/types'
import { useAnchorDisplayStore } from '../stores/anchorDisplay'
import {
  anchorToView, createEditorDocument, cropOnRotatedCanvas, DocumentHistory, parseEditorDocument, viewSelectionToSourceRect,
  viewToSource, type EditorDocument, type ImageAnchor, type NormalizedRect,
} from '../../../../packages/editor-core/src'
import {
  analyzeImageColors, decodeImagePreview, isColorAnalysis, makeWorkingJpeg, releaseCanvas, renderDocumentPreview,
  readInspirationImageMetadata, type ColorAnalysis,
} from '../../../../packages/image-processing/src'

type TransformAction = 'rotate' | 'crop' | 'reset'
type RGBChannel = 'red' | 'green' | 'blue'
interface AnchorDraft { id: string | null; x: number; y: number; note: string }
interface AnchorMoveDraft { anchorId: string; pointerId: number; startX: number; startY: number; x: number; y: number; moved: boolean }

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const anchorDisplayStore = useAnchorDisplayStore()
const itemId = computed(() => typeof route.params.id === 'string' ? route.params.id : '')
const managementMode = computed(() => route.name === 'organization-edit-inspiration' || route.name === 'platform-edit-inspiration')
const managementOrgID = computed(() => route.name === 'platform-edit-inspiration' ? Number(route.query.orgid) : undefined)
/** 管理模式走独立授权接口，后端从原记录取得创建者，普通用户入口始终保持私有语义。 */
const api = {
  ...baseApi,
  inspiration: (id: string) => managementMode.value ? baseApi.managementInspiration(id, managementOrgID.value) : baseApi.inspiration(id),
  download: (key: string) => managementMode.value ? loadManagedDetailImage(itemId.value, key, managementOrgID.value) : loadDetailImage(key, record.value?.file_hash || ''),
  updateInspiration: (id: string, data: Record<string, unknown>, imageMetadata?: import('../api/types').InspirationImageMetadata) => managementMode.value
    ? baseApi.managementSaveInspiration(id, { data, ...(imageMetadata ? { image_metadata: imageMetadata } : {}) }, managementOrgID.value)
    : baseApi.updateInspiration(id, data, imageMetadata),
  updateInspirationTags: (id: string, names: string[]) => managementMode.value ? baseApi.managementSaveInspiration(id, { tag_names: names }, managementOrgID.value) : baseApi.updateInspirationTags(id, names),
}
const isExisting = computed(() => itemId.value.length > 0)
const isEditing = ref(false)
// 属性和工具按需打开，不把拍摄参数、标签与编辑控件堆在图片下方。
const toolPanel = ref<'properties' | 'anchors' | 'adjust' | 'note' | null>(null)
const tagPickerOpen = ref(false)
const detailTagQuery = ref('')
const toolAnchor = shallowRef<HTMLElement>()
const noteText = computed(() => typeof documentData.value.image_note === 'string' ? documentData.value.image_note : '')
const noteDraft = ref('')
const noteEditing = ref(false)
const noteSaving = ref(false)
const notePersisted = ref('')
const noteInput = ref<{ focus: () => void } | null>(null)
/** 图片备注沿用文档扩展JSON和已有保存接口；详情态直接输入，新图和显式编辑态仍进入撤销历史。 */
function updateImageNote(value: string | number) { commit({ ...documentData.value, image_note: String(value).slice(0, 500) }) }
/** 参数浮层不阻挡画布；锚点一键进入编辑和点按添加状态。 */
function openTool(panel: 'anchors' | 'adjust' | 'note', event: MouseEvent) {
  toolAnchor.value = event.currentTarget as HTMLElement
  if (!isEditing.value) beginEditing()
  if (panel === 'anchors') { anchorMode.value = true; cropMode.value = false }
  else anchorMode.value = false
  toolPanel.value = toolPanel.value === panel ? null : panel
}
/** 右上角属性面板与按钮共用同一个锚定玻璃浮层，避免再弹出独立模态框。 */
function openProperties(event: MouseEvent) {
  toolAnchor.value = event.currentTarget as HTMLElement
  toolPanel.value = toolPanel.value === 'properties' ? null : 'properties'
}
const toolTitle = computed(() => t(toolPanel.value === 'properties' ? 'editor.metadataPanelTitle' : toolPanel.value === 'anchors' ? 'editor.toolbarAnchors' : toolPanel.value === 'note' ? 'editor.imageNote' : 'editor.adjust'))
/** 锚点添加需要回到画布接收点击，先关闭工具浮窗，保留当前编辑状态。 */
function toggleAnchorTool() { anchorMode.value = !anchorMode.value; toolPanel.value = null }

/** 可视视口变化时同步键盘占位；小输入条使用固定定位，但不会被软键盘盖住。 */
function updateAnchorEditorViewport(): void {
  cancelAnimationFrame(anchorEditorViewportFrame)
  anchorEditorViewportFrame = requestAnimationFrame(() => {
    const viewport = window.visualViewport
    const keyboardBottom = viewport ? Math.max(0, window.innerHeight - viewport.height - viewport.offsetTop) : 0
    document.documentElement.style.setProperty('--anchor-keyboard-bottom', `${keyboardBottom}px`)
  })
}

function startAnchorEditorViewportTracking(): void {
  if (anchorEditorTracking) return
  anchorEditorTracking = true
  window.visualViewport?.addEventListener('resize', updateAnchorEditorViewport)
  window.visualViewport?.addEventListener('scroll', updateAnchorEditorViewport)
  window.addEventListener('resize', updateAnchorEditorViewport)
  updateAnchorEditorViewport()
}

function stopAnchorEditorViewportTracking(): void {
  if (!anchorEditorTracking) return
  anchorEditorTracking = false
  cancelAnimationFrame(anchorEditorViewportFrame)
  window.visualViewport?.removeEventListener('resize', updateAnchorEditorViewport)
  window.visualViewport?.removeEventListener('scroll', updateAnchorEditorViewport)
  window.removeEventListener('resize', updateAnchorEditorViewport)
  document.documentElement.style.removeProperty('--anchor-keyboard-bottom')
}

const editSnapshot = ref('')
const documentData = ref<EditorDocument>(createEditorDocument())
const sourceCanvas = shallowRef<HTMLCanvasElement | null>(null)
const previewCanvas = ref<HTMLCanvasElement | null>(null)
const backdropCanvas = ref<HTMLCanvasElement | null>(null)
const stageBox = ref<HTMLElement | null>(null)
const zoomLayer = ref<HTMLElement | null>(null)
const zoomShell = ref<HTMLElement | null>(null)
const zoomCanvas = ref<HTMLCanvasElement | null>(null)
const zoomCloseButton = ref<HTMLButtonElement | null>(null)
const zoomActive = ref(false)
const zoomFullscreen = ref(false)
const record = shallowRef<InspirationRecord | null>(null)
const imageFile = shallowRef<File | null>(null)
const imageMetadata = shallowRef<InspirationImageMetadata | null>(null)
const filename = ref('')
const imageReady = ref(false)
const downsampled = ref(false)
const loading = ref(false)
const saving = ref(false)
const busyPhase = ref('')
const error = ref('')
const status = ref('')
const tagNames = ref<string[]>([])
const tagOptions = ref<string[]>([])
const tagOptionsLoading = ref(false)
const tagSaving = ref(false)
const tagSearchError = ref('')
const tagSaveError = ref('')
const tagSaveStatus = ref('')
const colorAnalysis = ref<ColorAnalysis | null>(null)
const colorAnalysisError = ref('')
const colorAnalysisSaving = ref(false)
const anchorMode = ref(false)
const cropMode = ref(false)
const anchorSaving = ref(false)
const transformDialog = ref<TransformAction | null>(null)
const anchorEditorOpen = ref(false)
const anchorDraft = ref<AnchorDraft | null>(null)
const anchorEditorInput = ref<{ focus: () => void } | null>(null)
const movingAnchor = ref<AnchorMoveDraft | null>(null)
const pressedDetailTag = ref('')
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
let colorWritePromise: Promise<void> | null = null
let tagWritePromise: Promise<void> | null = null
let tagSearchTimer: number | undefined
let noteSaveTimer: number | undefined
let tagSearchVersion = 0
let suppressAnchorClick = false
let suppressPreviewClick = false
let noteSavePromise: Promise<boolean> | null = null
let detailWriteChain: Promise<unknown> = Promise.resolve()
let detailLocalRevision = 0
let zoomOrigin: ZoomRect | null = null
let zoomStart: PinchPoints | null = null
let zoomLastFrame: PinchFrame | null = null
let zoomSettledFrame: PinchFrame | null = null
let zoomPinchMode: 'expand' | 'return' | null = null
let zoomClosing = false
let previousBodyOverflow = ''
let anchorEditorTracking = false
let anchorEditorViewportFrame = 0
const detailTagLongPress = createLongPress(() => {
  const name = pressedDetailTag.value
  if (name) void removeDetailTag(name)
})

/** 详情页只展示已采集到的属性；元数据中的空值不会占据面板空间。 */
const imageMetadataSections = computed(() => {
  const metadata = record.value?.image_metadata
  if (!metadata) return []
  const sections = [
    { title: t('editor.metadataImageGroup'), fields: [
      ['image_id', 'editor.metadataImageId'], ['source_file_size', 'editor.metadataSourceFileSize'],
      ['source_last_modified_at', 'editor.metadataSourceLastModified'], ['image_width', 'editor.metadataWidth'],
      ['image_height', 'editor.metadataHeight'], ['horizontal_resolution_dpi', 'editor.metadataHorizontalResolution'],
      ['vertical_resolution_dpi', 'editor.metadataVerticalResolution'], ['bit_depth', 'editor.metadataBitDepth'],
      ['color_space', 'editor.metadataColorSpace'], ['compression', 'editor.metadataCompression'],
    ] },
    { title: t('editor.metadataDescriptionGroup'), fields: [
      ['title', 'editor.metadataTitle'], ['subject', 'editor.metadataSubject'], ['rating', 'editor.metadataRating'],
      ['tags', 'editor.metadataTags'], ['author', 'editor.metadataAuthor'], ['comments', 'editor.metadataComments'],
      ['copyright', 'editor.metadataCopyright'], ['software', 'editor.metadataSoftware'], ['date_acquired', 'editor.metadataDateAcquired'],
    ] },
    { title: t('editor.metadataCameraGroup'), fields: [
      ['camera_make', 'editor.metadataCameraMake'], ['camera_model', 'editor.metadataCameraModel'], ['camera_serial_number', 'editor.metadataCameraSerial'],
      ['date_time_original', 'editor.metadataDateTimeOriginal'], ['date_time_digitized', 'editor.metadataDateTimeDigitized'],
    ] },
    { title: t('editor.metadataLensGroup'), fields: [
      ['lens_make', 'editor.metadataLensMake'], ['lens_model', 'editor.metadataLensModel'], ['lens_serial_number', 'editor.metadataLensSerial'],
      ['flash_make', 'editor.metadataFlashMake'], ['flash_model', 'editor.metadataFlashModel'], ['flash_serial_number', 'editor.metadataFlashSerial'],
      ['aperture_f_number', 'editor.metadataAperture'], ['maximum_aperture_f_number', 'editor.metadataMaximumAperture'], ['exposure_time_seconds', 'editor.metadataExposureTime'],
      ['iso_speed', 'editor.metadataIso'], ['exposure_compensation_ev', 'editor.metadataExposureCompensation'],
      ['exposure_program', 'editor.metadataExposureProgram'], ['flash_fired', 'editor.metadataFlashFired'], ['flash_mode', 'editor.metadataFlashMode'],
      ['flash_energy_bcps', 'editor.metadataFlashEnergy'],
      ['focal_length_mm', 'editor.metadataFocalLength'], ['focal_length_35mm_mm', 'editor.metadataFocalLength35mm'],
      ['subject_distance_m', 'editor.metadataSubjectDistance'], ['metering_mode', 'editor.metadataMeteringMode'],
      ['light_source', 'editor.metadataLightSource'], ['white_balance', 'editor.metadataWhiteBalance'],
      ['contrast', 'editor.metadataContrast'], ['brightness_ev', 'editor.metadataBrightness'], ['saturation', 'editor.metadataSaturation'],
      ['sharpness', 'editor.metadataSharpness'], ['digital_zoom_ratio', 'editor.metadataDigitalZoom'],
      ['compression_bits_per_pixel', 'editor.metadataCompressionBits'], ['exif_version', 'editor.metadataExifVersion'],
    ] },
    { title: t('editor.metadataGpsGroup'), fields: [
      ['gps_latitude', 'editor.metadataGpsLatitude'], ['gps_longitude', 'editor.metadataGpsLongitude'],
      ['gps_altitude_m', 'editor.metadataGpsAltitude'], ['gps_timestamp', 'editor.metadataGpsTimestamp'],
    ] },
  ]
  const additional = friendlyExtra(metadata.extra ?? {}, t, locale.value)
  const mapped = sections.map(section => ({
    ...section,
    rows: section.fields.flatMap(([key, label]) => {
      const value = metadata[key as keyof InspirationImageMetadata]
      if (value === undefined || value === null || value === '' || (Array.isArray(value) && value.length === 0)) return []
      return [{ key, label: t(label), value: formatMetadataValue(key, value) }]
    }),
  })).filter(section => section.rows.length > 0)
  if (additional.length) mapped.push({ title: t('editor.metadataAdditionalGroup'), fields: [], rows: additional })
  return mapped
})
const imageMetadataExtraEntries = computed(() => Object.entries(record.value?.image_metadata?.extra ?? {})
  .sort(([left], [right]) => left.localeCompare(right))
  .map(([key, value]) => ({ key, value: rawMetadataValue(value), complex: typeof value === 'object' || rawMetadataValue(value).length > 100 })))
const imageGpsMapUrl = computed(() => gpsMapUrl(record.value?.image_metadata?.gps_latitude, record.value?.image_metadata?.gps_longitude))
const hasImageMetadata = computed(() => imageMetadataSections.value.length > 0 || imageMetadataExtraEntries.value.length > 0)

function formatMetadataValue(key: string, value: unknown): string {
  return formatImageMetadataValue(key, value, t, locale.value)
}

const canUndo = computed(() => { historyRevision.value; return history?.canUndo ?? false })
const canRedo = computed(() => { historyRevision.value; return history?.canRedo ?? false })
const noteDirty = computed(() => noteDraft.value !== notePersisted.value)
const hasUnsavedChanges = computed(() => {
  if (editSnapshot.value === '') return false
  const documentChanged = JSON.stringify(documentData.value) !== editSnapshot.value
  const draft = anchorDraft.value
  const draftChanged = draft !== null && (draft.id === null
    ? draft.note.trim().length > 0
    : documentData.value.anchors.find(anchor => anchor.id === draft.id)?.note !== draft.note)
  return documentChanged || draftChanged || cropSelection.value !== null || !!transformDialog.value || !!movingAnchor.value?.moved
})
const displayDimensions = computed(() => {
  const source = sourceCanvas.value
  if (!source) return { width: 0, height: 0 }
  const rotatedWidth = documentData.value.rotation === 90 || documentData.value.rotation === 270 ? source.height : source.width
  const rotatedHeight = documentData.value.rotation === 90 || documentData.value.rotation === 270 ? source.width : source.height
  const crop = cropOnRotatedCanvas(documentData.value)
  const width = Math.max(1, Math.round(rotatedWidth * crop.width))
  const height = Math.max(1, Math.round(rotatedHeight * crop.height))
  // 只读详情至少让图片的长边或宽边贴住渲染框；编辑态仍预留锚点触控区。
  const detailInset = isExisting.value && !isEditing.value ? 0 : 48
  const availableWidth = Math.max(1, fitBox.value.width - detailInset)
  const availableHeight = Math.max(1, fitBox.value.height - detailInset)
  const scale = Math.min(1, availableWidth / width || 1, availableHeight / height || 1)
  return { width: Math.max(1, Math.round(width * scale)), height: Math.max(1, Math.round(height * scale)) }
})
const anchorDisplayStyle = computed<Record<string, string>>(() => {
  const scale = anchorDisplayStore.size / 100
  return {
    '--anchor-dot-half': `${12 * scale}px`,
    '--anchor-dot-size': `${24 * scale}px`,
    '--anchor-dot-core-size': `${10 * scale}px`,
    '--anchor-gap': `${6 * scale}px`,
    '--anchor-label-font-size': `${12 * scale}px`,
    '--anchor-label-padding-y': `${6 * scale}px`,
    '--anchor-label-padding-x': `${11 * scale}px`,
    '--anchor-index-size': `${21 * scale}px`,
    '--anchor-index-font-size': `${9 * scale}px`,
    '--anchor-note-font-size': `${11 * scale}px`,
  }
})
const visibleAnchors = computed(() => documentData.value.anchors.flatMap((anchor, index) => {
  const draft = movingAnchor.value?.anchorId === anchor.id ? movingAnchor.value : null
  const displayAnchor = draft ? { ...anchor, x: draft.x, y: draft.y } : anchor
  const position = anchorToView(displayAnchor, documentData.value)
  if (!position.visible) return []
  const width = Math.max(1, displayDimensions.value.width)
  const scale = anchorDisplayStore.size / 100
  const desiredLabelWidth = Math.min(240 * scale, Math.max(80 * scale, Array.from(anchor.note).length * 12 * scale + 24 * scale))
  const leftSpace = position.x * width
  const rightSpace = (1 - position.x) * width
  const markerOffset = 36 * scale
  const leftLabelSpace = Math.max(0, leftSpace - markerOffset)
  const rightLabelSpace = Math.max(0, rightSpace - markerOffset)
  const side = leftLabelSpace >= desiredLabelWidth || leftLabelSpace >= rightLabelSpace ? 'anchor-left' : 'anchor-right'
  const availableLabelWidth = side === 'anchor-left' ? leftLabelSpace : rightLabelSpace
  return [{
    ...displayAnchor,
    index,
    side,
    labelMaxWidth: `${Math.max(52 * scale, Math.min(desiredLabelWidth, availableLabelWidth))}px`,
    left: `${position.x * 100}%`,
    top: `${position.y * 100}%`,
  }]
}))
const draftAnchorPosition = computed(() => {
  const draft = anchorDraft.value
  if (!draft || draft.id) return null
  const position = anchorToView(draft, documentData.value)
  return position.visible ? { left: `${position.x * 100}%`, top: `${position.y * 100}%` } : null
})
const cropDragStyle = computed(() => {
  const selection = cropSelection.value
  if (!selection) return {}
  return { left: `${selection.x * 100}%`, top: `${selection.y * 100}%`, width: `${selection.width * 100}%`, height: `${selection.height * 100}%` }
})
/** 详情操作栏以图片主色做极轻环境染色，浅色页面上也能看出透明磨砂层次。 */
const detailAmbientStyle = computed<Record<string, string>>(() => {
  const ambient = record.value?.dominant_color || colorAnalysis.value?.palette[0]?.hex
  const style: Record<string, string> = {}
  if (ambient) style['--detail-ambient'] = ambient
  return style
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

/** 按当前界面语言显示颜色百分比，原始画像数值仍以数字保存在 JSONB 中。 */
function displayPercent(value: number): string {
  return `${new Intl.NumberFormat(locale.value, { maximumFractionDigits: 2 }).format(value)}%`
}

/** 构造完整编辑数据并合并自动画像，避免编辑保存覆盖尚未写回的颜色画像。 */
function editorDataForSave(): Record<string, unknown> {
  const data: Record<string, unknown> = { ...documentData.value }
  if (colorAnalysis.value) data.color_analysis = colorAnalysis.value
  return data
}

/** 进入编辑时记录轻量 JSON 快照；撤销历史仍只管理用户编辑字段。 */
function beginEditing(): void {
  editSnapshot.value = JSON.stringify(documentData.value)
  history = new DocumentHistory(documentData.value)
  historyRevision.value += 1
  isEditing.value = true
  error.value = ''
  status.value = ''
}

/** 详情默认保持沉浸阅读；只有用户从属性面板明确选择时才进入带保存按钮的完整编辑态。 */
function startFullEditing(): void {
  toolPanel.value = null
  beginEditing()
}

/** 退出编辑前确认丢弃未保存修改，并将轻量文档和撤销历史恢复至进入时快照。 */
async function cancelEditing(): Promise<void> {
  finishRgbChange()
  if (hasUnsavedChanges.value) {
    try {
      await ElMessageBox.confirm(t('editor.discardChangesText'), t('editor.discardChangesTitle'), {
        confirmButtonText: t('editor.discardChangesConfirm'),
        cancelButtonText: t('common.cancel'),
        type: 'warning',
        customClass: 'editor-glass-dialog',
      })
    } catch { return }
  }
  if (editSnapshot.value) documentData.value = JSON.parse(editSnapshot.value) as EditorDocument
  history = new DocumentHistory(documentData.value)
  historyRevision.value += 1
  editSnapshot.value = ''
  isEditing.value = false
  toolPanel.value = null
  anchorMode.value = false
  cropMode.value = false
  cropSelection.value = null
  transformDialog.value = null
  anchorEditorOpen.value = false
  anchorDraft.value = null
  error.value = ''
  status.value = ''
  scheduleRender()
}

/** 详情自动保存统一从顶部反馈，避免底栏移除后错误停留在不可见位置。 */
function showTopNotice(message: string, type: 'success' | 'error'): void {
  ElMessage({
    message,
    type,
    duration: type === 'success' ? 1600 : 2800,
    grouping: true,
    offset: 72,
    customClass: 'editor-glass-message',
  })
}

/** 文档自动保存按提交顺序串行执行，防止备注、锚点和颜色画像互相覆盖。 */
function queueDetailWrite(nextData: Record<string, unknown>): Promise<InspirationRecord> {
  const expectedId = itemId.value
  const payload = JSON.parse(JSON.stringify(nextData)) as Record<string, unknown>
  const operation = detailWriteChain.then(async () => {
    const updated = await api.updateInspiration(expectedId, payload)
    if (record.value?.business_id === updated.business_id) record.value = updated
    return updated
  })
  detailWriteChain = operation.then(() => undefined, () => undefined)
  return operation
}

/** 只有本地文档没有继续变化时才采用服务端响应，避免迟到保存覆盖正在输入的新内容。 */
function mergePersistedDocument(updated: InspirationRecord, expectedRevision: number): void {
  if (detailLocalRevision !== expectedRevision || record.value?.business_id !== updated.business_id) return
  documentData.value = parseEditorDocument(updated.data)
  history = new DocumentHistory(documentData.value)
  historyRevision.value += 1
  scheduleRender()
}

/** 输入暂停后自动提交整图备注；始终以最新草稿为准，保存期间的新输入会继续排队。 */
async function flushImageNote(showSuccess = true): Promise<boolean> {
  if (noteSaveTimer !== undefined) {
    window.clearTimeout(noteSaveTimer)
    noteSaveTimer = undefined
  }
  if (noteSavePromise) return noteSavePromise

  noteSavePromise = (async () => {
    let saved = false
    while (noteDraft.value !== notePersisted.value) {
      const expectedRevision = detailLocalRevision
      const note = noteDraft.value
      noteSaving.value = true
      try {
        const updated = await queueDetailWrite({ ...editorDataForSave(), image_note: note })
        notePersisted.value = note
        saved = true
        mergePersistedDocument(updated, expectedRevision)
      } catch (cause) {
        showTopNotice(localError(cause, t('editor.noteSaveFailed')), 'error')
        return false
      } finally {
        noteSaving.value = false
      }
    }
    if (saved && showSuccess) showTopNotice(t('editor.noteAutosaved'), 'success')
    return true
  })()

  try { return await noteSavePromise }
  finally { noteSavePromise = null }
}

/** 备注停止输入约半秒后自动落库；失焦和离页会通过 flushImageNote 立即补交。 */
function scheduleImageNoteSave(): void {
  if (noteSaveTimer !== undefined) window.clearTimeout(noteSaveTimer)
  noteSaveTimer = window.setTimeout(() => {
    noteSaveTimer = undefined
    void flushImageNote()
  }, 650)
}

/** 点击详情说明空白区进入原地编辑，不打开底栏或额外编辑页。 */
async function startImageNoteEditing(): Promise<void> {
  if (isEditing.value) return
  noteEditing.value = true
  await nextTick()
  noteInput.value?.focus()
}

/** 备注输入先更新本地文档，自动保存期间仍可从顶部通知看到最终成功或失败。 */
function handleImageNoteInput(value: string | number): void {
  const text = String(value).slice(0, 500)
  noteDraft.value = text
  documentData.value = { ...documentData.value, image_note: text }
  detailLocalRevision += 1
  scheduleImageNoteSave()
}

/** 自动画像只合并到已保存数据；独立写入进行时，编辑保存会等待它完成再提交完整文档。 */
async function persistColorAnalysis(profile: ColorAnalysis, baseData: Record<string, unknown>): Promise<void> {
  if (!isExisting.value || !record.value) return
  if (colorWritePromise) { await colorWritePromise; return }

  colorAnalysisSaving.value = true
  colorAnalysisError.value = ''
  const pending = (async () => {
    try {
      await queueDetailWrite({ ...baseData, color_analysis: profile })
      colorAnalysisError.value = ''
    } catch (cause) {
      colorAnalysisError.value = localError(cause, t('editor.colorAnalysisSaveFailed'))
    } finally {
      colorAnalysisSaving.value = false
    }
  })()
  colorWritePromise = pending
  try { await pending }
  finally { if (colorWritePromise === pending) colorWritePromise = null }
}

/** 重新读取缺失画像或重试保存失败的画像；错误只影响画像，不阻断图片详情。 */
async function retryColorAnalysis(): Promise<void> {
  if (!sourceCanvas.value || !record.value || saving.value || colorAnalysisSaving.value) return
  colorAnalysisError.value = ''
  let profile = colorAnalysis.value
  if (!profile) {
    try {
      profile = analyzeImageColors(sourceCanvas.value)
      colorAnalysis.value = profile
    } catch {
      colorAnalysisError.value = t('editor.colorAnalysisFailed')
      return
    }
  }
  await persistColorAnalysis(profile, record.value.data)
}

/** 每次提交建立一条轻量文档历史，不复制 canvas 像素或原图文件。 */
function commit(next: EditorDocument): void {
  history ??= new DocumentHistory(documentData.value)
  documentData.value = history.commit(next)
  if (isExisting.value && !isEditing.value) detailLocalRevision += 1
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
  await loadSelectedFile(file)
}

async function selectUploadedFile(file: UploadFile): Promise<void> {
  if (file.raw) await loadSelectedFile(file.raw)
}

async function loadSelectedFile(file: File): Promise<void> {
  const version = ++decodeVersion
  error.value = ''
  status.value = ''
  colorAnalysis.value = null
  colorAnalysisError.value = ''
  imageMetadata.value = null
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
    imageMetadata.value = await readInspirationImageMetadata(file)
    if (version !== decodeVersion) return
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

/** 打开既有记录时读取压缩后的工作图，避免依赖后台会回收的原图 key。 */
async function loadExisting(): Promise<void> {
  const version = ++decodeVersion
  loading.value = true
  error.value = ''
  colorAnalysis.value = null
  colorAnalysisError.value = ''
  try {
    const loaded = (!managementMode.value && takeDetailRecord(itemId.value)) || await api.inspiration(itemId.value)
    if (version !== decodeVersion) return
    record.value = loaded
    tagNames.value = [...(loaded.tag_names ?? [])]
    tagOptions.value = [...tagNames.value]
    searchTagOptions('')
    imageMetadata.value = loaded.image_metadata ?? null
    filename.value = loaded.file_name || t('gallery.untitled')
    documentData.value = parseEditorDocument(loaded.data)
    noteDraft.value = typeof documentData.value.image_note === 'string' ? documentData.value.image_note : ''
    notePersisted.value = noteDraft.value
    detailLocalRevision = 0
    history = new DocumentHistory(documentData.value)
    const workImage = await api.download(loaded.image_key)
    if (version !== decodeVersion) return
    const decoded = await decodeImagePreview(workImage)
    if (version !== decodeVersion) { releaseCanvas(decoded.canvas); return }
    sourceCanvas.value = decoded.canvas
    downsampled.value = decoded.wasDownsampled
    const storedProfile = isColorAnalysis(loaded.data.color_analysis) ? loaded.data.color_analysis : null
    colorAnalysis.value = storedProfile
    if (!storedProfile) {
      try {
        const profile = analyzeImageColors(decoded.canvas)
        colorAnalysis.value = profile
        void persistColorAnalysis(profile, loaded.data)
      } catch {
        colorAnalysisError.value = t('editor.colorAnalysisFailed')
      }
    }
    imageReady.value = true
    scheduleRender()
    await nextTick()
    observeStage()
  } catch (cause) { if (version === decodeVersion) error.value = localError(cause, t('editor.loadError')) }
  finally { if (version === decodeVersion) loading.value = false }
}

/** 通过带用户范围的分页接口远程检索标签名称，保留当前已选项供多选框稳定显示。 */
function searchTagOptions(query: string): void {
  if (managementMode.value) { tagOptions.value = [...tagNames.value]; return }
  if (tagSearchTimer !== undefined) window.clearTimeout(tagSearchTimer)
  const version = ++tagSearchVersion
  tagSearchError.value = ''
  tagSearchTimer = window.setTimeout(async () => {
    tagOptionsLoading.value = true
    try {
      const result = await api.tags(1, 100, query)
      if (version !== tagSearchVersion) return
      tagOptions.value = [...new Set([...tagNames.value, ...result.items.map(tag => tag.content)])]
    } catch (cause) {
      if (version === tagSearchVersion) {
        tagSearchError.value = localError(cause, t('editor.tagSearchFailed'))
      }
    } finally {
      if (version === tagSearchVersion) {
        tagOptionsLoading.value = false
        tagSearchTimer = undefined
      }
    }
  }, 160)
}

/** 标签变更立即提交完整名称集合；失败时恢复服务端确认过的选择，避免误显示未保存内容。 */
async function saveTagNames(names: string[]): Promise<boolean> {
  const current = record.value
  if (!current || tagSaving.value) return false
  const nextNames = [...new Set(names.map(name => name.trim()).filter(Boolean))]
  const previousNames = [...(current.tag_names ?? [])]
  if (nextNames.length === previousNames.length && nextNames.every((name, index) => name === previousNames[index])) {
    tagNames.value = previousNames
    return true
  }

  tagNames.value = nextNames
  tagOptions.value = [...new Set([...nextNames, ...tagOptions.value])]
  tagSaveError.value = ''
  tagSaveStatus.value = ''
  tagSaving.value = true
  let saved = false
  const pending = (async () => {
    try {
      const updated = await api.updateInspirationTags(current.business_id, nextNames)
      if (record.value?.business_id === current.business_id) {
        const savedNames = updated.tag_names ?? nextNames
        record.value = { ...record.value, tag_ids: updated.tag_ids, tag_names: savedNames, updated_at: updated.updated_at }
        tagNames.value = [...savedNames]
        tagOptions.value = [...new Set([...savedNames, ...tagOptions.value])]
        tagSaveStatus.value = t('editor.tagSaved')
      }
      saved = true
    } catch (cause) {
      if (record.value?.business_id === current.business_id) tagNames.value = [...(record.value.tag_names ?? previousNames)]
      tagSaveError.value = localError(cause, t('editor.tagSaveFailed'))
    } finally {
      tagSaving.value = false
    }
  })()
  tagWritePromise = pending
  void pending.finally(() => {
    if (tagWritePromise === pending) tagWritePromise = null
  })
  await pending
  return saved
}

/** 长按或键盘删除单个标签，成功后用短提示确认，失败沿用现有标签错误反馈。 */
async function removeDetailTag(name: string): Promise<void> {
  pressedDetailTag.value = ''
  detailTagLongPress.cancel()
  if (tagSaving.value) return
  const saved = await saveTagNames((record.value?.tag_names ?? []).filter(tag => tag !== name))
  if (saved) ElMessage({ message: t('editor.tagRemoved', { name }), type: 'success', duration: 1600, grouping: true, customClass: 'editor-glass-message' })
  else if (tagSaveError.value) ElMessage({ message: tagSaveError.value, type: 'error', duration: 2400, grouping: true, customClass: 'editor-glass-message' })
}

/** 标签设置独立于底部操作栏，通过行尾“更多”打开透明磨砂选择框。 */
function toggleTagPicker(): void {
  toolPanel.value = null
  tagPickerOpen.value = !tagPickerOpen.value
}

function closeTagPicker(): void {
  tagPickerOpen.value = false
  detailTagQuery.value = ''
}

/** 点击选项切换标签；选择结果立即写入服务端并同步当前详情。 */
async function toggleDetailTag(name: string): Promise<void> {
  if (tagSaving.value) return
  const selected = tagNames.value.includes(name)
  await saveTagNames(selected ? tagNames.value.filter(tag => tag !== name) : [...tagNames.value, name])
}

/** 输入新名称后回车或点击新增，空值和重复值不会产生额外请求。 */
async function addTypedDetailTag(): Promise<void> {
  const name = detailTagQuery.value.trim()
  if (!name || tagSaving.value || tagNames.value.includes(name)) return
  const saved = await saveTagNames([...tagNames.value, name])
  if (saved) {
    detailTagQuery.value = ''
    searchTagOptions('')
  }
}

function handleTagQueryInput(value: string | number): void {
  detailTagQuery.value = String(value)
  searchTagOptions(detailTagQuery.value)
}

/** 长按期间显示按压进度；移动超过容差即转为滚动并取消删除。 */
function beginDetailTagPress(event: PointerEvent, name: string): void {
  if (tagSaving.value) return
  pressedDetailTag.value = name
  detailTagLongPress.start(event.clientX, event.clientY)
}

function moveDetailTagPress(event: PointerEvent): void {
  detailTagLongPress.move(event.clientX, event.clientY)
}

function endDetailTagPress(): void {
  detailTagLongPress.cancel()
  pressedDetailTag.value = ''
}

const detailTagOptions = computed(() => [...new Set([...tagNames.value, ...tagOptions.value])])

watch(tagPickerOpen, async open => {
  if (!open) return
  tagSaveError.value = ''
  searchTagOptions(detailTagQuery.value)
  await nextTick()
  document.querySelector<HTMLInputElement>('.detail-tag-picker input')?.focus()
})

watch(isEditing, editing => { if (editing) tagPickerOpen.value = false })
watch(anchorEditorOpen, async open => {
  if (!open) {
    stopAnchorEditorViewportTracking()
    return
  }
  startAnchorEditorViewportTracking()
  await nextTick()
  anchorEditorInput.value?.focus()
})

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
      // 背景按详情框比例裁切铺满，只保留低分辨率预览，避免再复制一份原图像素。
      const backdrop = backdropCanvas.value
      if (backdrop) {
        const stageWidth = Math.max(1, fitBox.value.width || target.width)
        const stageHeight = Math.max(1, fitBox.value.height || target.height)
        const stageRatio = stageWidth / stageHeight
        const targetRatio = target.width / target.height
        let sourceX = 0
        let sourceY = 0
        let sourceWidth = target.width
        let sourceHeight = target.height
        if (targetRatio > stageRatio) {
          sourceWidth = target.height * stageRatio
          sourceX = (target.width - sourceWidth) / 2
        } else {
          sourceHeight = target.width / stageRatio
          sourceY = (target.height - sourceHeight) / 2
        }
        backdrop.width = Math.max(1, Math.min(480, Math.round(stageWidth)))
        backdrop.height = Math.max(1, Math.round(backdrop.width / stageRatio))
        backdrop.getContext('2d')?.drawImage(
          target,
          sourceX,
          sourceY,
          sourceWidth,
          sourceHeight,
          0,
          0,
          backdrop.width,
          backdrop.height,
        )
      }
      releaseCanvas(output)
      detailImageReady(target)
    } catch { error.value = t('editor.decodeFailed') }
  })
}

// 进入或退出详情态会挂载/移除低清背景画布；下一帧同步当前编辑结果。
watch(isEditing, () => { void nextTick(() => scheduleRender()) })

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

/** 把图片内点按换算成原图坐标；详情态和新图编辑态共用同一套锚点数据。 */
function addAnchorAtClientPoint(clientX: number, clientY: number): void {
  if (cropMode.value || anchorEditorOpen.value) return
  const bounds = previewCanvas.value?.getBoundingClientRect()
  if (!bounds) return
  if (clientX < bounds.left || clientX > bounds.right || clientY < bounds.top || clientY > bounds.bottom) return
  const point = { x: Math.max(0, Math.min(1, (clientX - bounds.left) / bounds.width)), y: Math.max(0, Math.min(1, (clientY - bounds.top) / bounds.height)) }
  const source = viewToSource(point, documentData.value)
  anchorDraft.value = { id: null, x: source.x, y: source.y, note: '' }
  toolPanel.value = null
  anchorEditorOpen.value = true
}

/** 详情单指点击只负责添加锚点；双指放大由独立查看层承接，避免单击同时触发全屏。 */
function onStageClick(event: MouseEvent): void {
  if (anchorEditorOpen.value) {
    closeAnchorEditor()
    return
  }
  if (suppressPreviewClick) {
    suppressPreviewClick = false
    return
  }
  if (isExisting.value && !isEditing.value) {
    if (zoomActive.value) return
    addAnchorAtClientPoint(event.clientX, event.clientY)
    return
  }
  if (!anchorMode.value || cropMode.value) return
  addAnchorAtClientPoint(event.clientX, event.clientY)
}

/** 将当前详情画布像素同步复制到全屏层；快照只存在于页面内存，不转存或上传。 */
function prepareZoomSnapshot(): ZoomRect | null {
  const canvas = previewCanvas.value
  const shell = zoomShell.value
  const target = zoomCanvas.value
  if (!canvas || !shell || !target || isEditing.value) return null
  const snapshot = copyVisibleImage(canvas)
  const bounds = canvas.getBoundingClientRect()
  if (!snapshot || !bounds.width || !bounds.height) {
    if (snapshot) releaseCanvas(snapshot)
    return null
  }
  target.width = snapshot.width
  target.height = snapshot.height
  target.getContext('2d')?.drawImage(snapshot, 0, 0)
  releaseCanvas(snapshot)
  const origin = { left: bounds.left, top: bounds.top, width: bounds.width, height: bounds.height }
  Object.assign(shell.style, {
    left: `${origin.left}px`,
    top: `${origin.top}px`,
    width: `${origin.width}px`,
    height: `${origin.height}px`,
    transform: 'translate3d(0, 0, 0) scale(1)',
  })
  shell.style.setProperty('--zoom-anchor-inverse', '1')
  return origin
}

/** 全屏锚点保持稳定的屏幕尺寸，只随图片位置移动；反向缩放抵消画布容器的放大。 */
function updateZoomAnchorScale(scale: number): void {
  zoomShell.value?.style.setProperty('--zoom-anchor-inverse', String(1 / Math.max(1, scale)))
}

/** 直接修改合成层变换，让手势移动期间不触发 Vue 重渲染和布局。 */
function applyZoomFrame(frame: PinchFrame): void {
  const shell = zoomShell.value
  const layer = zoomLayer.value
  if (!shell || !layer) return
  shell.style.transform = `translate3d(${frame.x}px, ${frame.y}px, 0) scale(${frame.scale})`
  layer.style.backgroundColor = `rgb(0 0 0 / ${Math.min(0.98, frame.progress * 0.98)})`
  updateZoomAnchorScale(frame.scale)
}

/** 全屏期间锁定页面滚动，关闭时恢复进入查看器前的原始值，避免覆盖其他弹层设置。 */
function setZoomBodyLocked(locked: boolean): void {
  const className = 'inspiration-zoom-open'
  if (locked && !document.body.classList.contains(className)) {
    previousBodyOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    document.body.classList.add(className)
  } else if (!locked && document.body.classList.contains(className)) {
    document.body.style.overflow = previousBodyOverflow
    document.body.classList.remove(className)
    previousBodyOverflow = ''
  }
}

/** 清空全屏快照和合成层状态；临时画布显式归零，避免连续手势积累大图内存。 */
function resetZoomViewer(): void {
  const shell = zoomShell.value
  const layer = zoomLayer.value
  if (shell) gsap.killTweensOf(shell)
  if (layer) gsap.killTweensOf(layer)
  if (shell) {
    shell.style.transform = ''
    shell.style.removeProperty('--zoom-anchor-inverse')
  }
  if (layer) layer.style.backgroundColor = 'transparent'
  const canvas = zoomCanvas.value
  if (canvas) {
    canvas.width = 0
    canvas.height = 0
  }
  zoomActive.value = false
  zoomFullscreen.value = false
  zoomClosing = false
  zoomOrigin = null
  zoomStart = null
  zoomLastFrame = null
  zoomSettledFrame = null
  zoomPinchMode = null
  setZoomBodyLocked(false)
}

/** 放大未达到全屏时沿原轨迹收回；动画结束才释放快照。 */
function restoreZoomViewer(): void {
  const shell = zoomShell.value
  const layer = zoomLayer.value
  if (!shell || !layer) { resetZoomViewer(); return }
  zoomClosing = true
  const duration = window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 0.01 : 0.2
  gsap.killTweensOf([shell, layer])
  gsap.to(shell, {
    x: 0,
    y: 0,
    scale: 1,
    duration,
    ease: 'power2.out',
    onUpdate: () => updateZoomAnchorScale(Number(gsap.getProperty(shell, 'scale'))),
  })
  gsap.to(layer, { backgroundColor: 'rgb(0 0 0 / 0)', duration, ease: 'power2.out', onComplete: resetZoomViewer })
}

/** 手指抬起时已达到全屏阈值就保留大图；否则完整倒放回详情画布原位置。 */
function endPreviewPinch(): void {
  if (!zoomStart || !zoomOrigin) return
  if (zoomPinchMode === 'return') {
    endFullscreenPinch()
    return
  }
  const start = zoomStart
  const frame = zoomLastFrame
  zoomStart = null
  zoomLastFrame = null
  suppressPreviewClick = true
  window.setTimeout(() => { suppressPreviewClick = false }, 400)
  if (!frame || frame.progress < PINCH_FULLSCREEN_THRESHOLD) {
    restoreZoomViewer()
    return
  }
  const viewport = { width: window.innerWidth, height: window.innerHeight }
  const settled = pinchZoomFrame(
    zoomOrigin,
    viewport,
    start,
    { distance: frame.targetScale * start.distance, x: viewport.width / 2, y: viewport.height / 2 },
  )
  const shell = zoomShell.value
  const layer = zoomLayer.value
  const settledFrame = {
    ...settled,
    scale: Math.max(frame.scale, settled.scale),
    progress: 1,
  }
  zoomSettledFrame = settledFrame
  if (shell && layer) {
    const duration = window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 0.01 : 0.12
    gsap.to(shell, {
      x: settledFrame.x,
      y: settledFrame.y,
      scale: settledFrame.scale,
      duration,
      ease: 'power2.out',
      overwrite: true,
      onUpdate: () => updateZoomAnchorScale(Number(gsap.getProperty(shell, 'scale'))),
    })
    gsap.to(layer, { backgroundColor: 'rgb(0 0 0 / 98%)', duration, ease: 'power2.out', overwrite: true })
  }
  zoomFullscreen.value = true
  setZoomBodyLocked(true)
  void nextTick(() => zoomCloseButton.value?.focus())
}

/** 开始双指手势时同步复制真实画布像素，首帧就与用户手指位置对齐。 */
function beginPreviewPinch(event: TouchEvent): void {
  if (isEditing.value || zoomActive.value || anchorEditorOpen.value || event.touches.length !== 2) return
  const origin = prepareZoomSnapshot()
  if (!origin) return
  event.preventDefault()
  const first = { x: event.touches[0]!.clientX, y: event.touches[0]!.clientY }
  const second = { x: event.touches[1]!.clientX, y: event.touches[1]!.clientY }
  zoomOrigin = origin
  zoomStart = {
    distance: pointDistance(first, second),
    x: (first.x + second.x) / 2,
    y: (first.y + second.y) / 2,
  }
  zoomLastFrame = null
  zoomSettledFrame = null
  zoomPinchMode = 'expand'
  zoomClosing = false
  zoomFullscreen.value = false
  zoomActive.value = true
  zoomLayer.value?.classList.add('is-active')
  applyZoomFrame({ scale: 1, x: 0, y: 0, progress: 0, targetScale: 1 })
}

/** 手势每一帧都按当前双指距离更新缩放、位置和背景，不等待阈值再跳转全屏。 */
function movePreviewPinch(event: TouchEvent): void {
  if (!zoomOrigin || !zoomStart || event.touches.length !== 2) return
  event.preventDefault()
  const first = { x: event.touches[0]!.clientX, y: event.touches[0]!.clientY }
  const second = { x: event.touches[1]!.clientX, y: event.touches[1]!.clientY }
  const current = {
    distance: pointDistance(first, second),
    x: (first.x + second.x) / 2,
    y: (first.y + second.y) / 2,
  }
  const frame = pinchZoomFrame(zoomOrigin, { width: window.innerWidth, height: window.innerHeight }, zoomStart, current)
  zoomLastFrame = frame
  applyZoomFrame(frame)
}

/** 全屏后重新接收双指手势，以当前全屏比例为起点连续收拢，接近详情尺寸时直接返回。 */
function beginFullscreenPinch(event: TouchEvent): void {
  if (!zoomFullscreen.value || zoomClosing || !zoomOrigin || !zoomSettledFrame || event.touches.length !== 2) return
  event.preventDefault()
  const first = { x: event.touches[0]!.clientX, y: event.touches[0]!.clientY }
  const second = { x: event.touches[1]!.clientX, y: event.touches[1]!.clientY }
  zoomStart = {
    distance: pointDistance(first, second),
    x: (first.x + second.x) / 2,
    y: (first.y + second.y) / 2,
  }
  zoomLastFrame = null
  zoomPinchMode = 'return'
}

/** 收拢过程持续跟手；达到详情尺寸后移除全屏层，并沿用原进入轨迹返回列表前的位置。 */
function moveFullscreenPinch(event: TouchEvent): void {
  if (zoomPinchMode !== 'return' || !zoomFullscreen.value || !zoomOrigin || !zoomStart || !zoomSettledFrame || event.touches.length !== 2) return
  event.preventDefault()
  const first = { x: event.touches[0]!.clientX, y: event.touches[0]!.clientY }
  const second = { x: event.touches[1]!.clientX, y: event.touches[1]!.clientY }
  const current = {
    distance: pointDistance(first, second),
    x: (first.x + second.x) / 2,
    y: (first.y + second.y) / 2,
  }
  const frame = pinchReturnFrame(
    zoomOrigin,
    { width: window.innerWidth, height: window.innerHeight },
    zoomSettledFrame.scale,
    zoomStart,
    current,
  )
  zoomLastFrame = frame
  applyZoomFrame(frame)
  if (!shouldReturnFromPinch(frame)) return
  zoomStart = null
  zoomLastFrame = null
  zoomPinchMode = null
  suppressPreviewClick = true
  window.setTimeout(() => { suppressPreviewClick = false }, 400)
  closeInspirationViewer()
}

/** 未缩到返回阈值时抬手，平滑回到全屏基准比例，避免停在中间尺寸。 */
function endFullscreenPinch(): void {
  if (zoomPinchMode !== 'return') return
  const frame = zoomLastFrame
  zoomStart = null
  zoomLastFrame = null
  zoomPinchMode = null
  if (frame && shouldReturnFromPinch(frame)) {
    closeInspirationViewer()
    return
  }
  const settled = zoomSettledFrame
  const shell = zoomShell.value
  const layer = zoomLayer.value
  if (!settled || !shell || !layer) return
  const duration = window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 0.01 : 0.12
  gsap.to(shell, {
    x: settled.x,
    y: settled.y,
    scale: settled.scale,
    duration,
    ease: 'power2.out',
    overwrite: true,
    onUpdate: () => updateZoomAnchorScale(Number(gsap.getProperty(shell, 'scale'))),
  })
  gsap.to(layer, { backgroundColor: 'rgb(0 0 0 / 98%)', duration, ease: 'power2.out', overwrite: true })
}

/** 属性面板的键盘/鼠标入口复用同一快照和 GSAP 轨迹，桌面端也能完整查看大图。 */
function openFullscreenFromToolbar(): void {
  toolPanel.value = null
  const origin = prepareZoomSnapshot()
  const shell = zoomShell.value
  const layer = zoomLayer.value
  if (!origin || !shell || !layer) return
  const viewport = { width: window.innerWidth, height: window.innerHeight }
  const targetScale = pinchTargetScale(origin, viewport)
  const target = pinchZoomFrame(
    origin,
    viewport,
    { distance: 1, x: origin.left + origin.width / 2, y: origin.top + origin.height / 2 },
    { distance: targetScale, x: viewport.width / 2, y: viewport.height / 2 },
  )
  zoomOrigin = origin
  zoomSettledFrame = { ...target, progress: 1 }
  zoomPinchMode = null
  zoomActive.value = true
  zoomFullscreen.value = true
  zoomClosing = false
  zoomLayer.value?.classList.add('is-active')
  setZoomBodyLocked(true)
  const duration = window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 0.01 : 0.28
  gsap.killTweensOf([shell, layer])
  gsap.to(shell, {
    x: target.x,
    y: target.y,
    scale: target.scale,
    duration,
    ease: 'power2.out',
    onUpdate: () => updateZoomAnchorScale(Number(gsap.getProperty(shell, 'scale'))),
  })
  gsap.to(layer, { backgroundColor: 'rgb(0 0 0 / 98%)', duration, ease: 'power2.out', onComplete: () => { void nextTick(() => zoomCloseButton.value?.focus()) } })
}

/** 关闭全屏时按同一来源矩形收回，随后释放临时快照和页面滚动锁。 */
function closeInspirationViewer(): void {
  if (!zoomFullscreen.value || zoomClosing) return
  zoomFullscreen.value = false
  restoreZoomViewer()
}

/** 键盘用户聚焦预览画布并按 Enter/Space 时，在画面中心添加备注锚点。 */
function addAnchorAtCenter(): void {
  if (cropMode.value || (isEditing.value && !anchorMode.value) || (!isEditing.value && !isExisting.value)) return
  const source = viewToSource({ x: 0.5, y: 0.5 }, documentData.value)
  anchorDraft.value = { id: null, x: source.x, y: source.y, note: '' }
  toolPanel.value = null
  anchorEditorOpen.value = true
}

/** 把已保存锚点载入底部小输入条，供用户修改或删除。 */
function editAnchor(anchor: ImageAnchor): void {
  toolPanel.value = null
  anchorDraft.value = { ...anchor }
  anchorEditorOpen.value = true
}

/** 关闭锚点小输入条同时清除尚未提交的草稿点位，避免图片上残留临时标记。 */
function closeAnchorEditor(): void {
  anchorEditorOpen.value = false
  anchorDraft.value = null
  error.value = ''
}

/** 在预览画布上拖动锚点时即时更新叠层位置，抬起指针后只提交一条可撤销历史。 */
function beginAnchorMove(event: PointerEvent, anchor: ImageAnchor): void {
  if (anchorMode.value || cropMode.value || anchorEditorOpen.value || event.button !== 0) return
  movingAnchor.value = {
    anchorId: anchor.id,
    pointerId: event.pointerId,
    startX: event.clientX,
    startY: event.clientY,
    x: anchor.x,
    y: anchor.y,
    moved: false,
  }
  const target = event.currentTarget
  if (target instanceof HTMLElement) {
    try { target.setPointerCapture(event.pointerId) } catch { /* 浏览器失焦时交由 pointercancel 清除临时状态。 */ }
  }
}

/** 将捕获到的指针坐标反算为原图坐标；最小拖动阈值避免把普通点按误判为移动。 */
function updateAnchorMove(event: PointerEvent): void {
  const draft = movingAnchor.value
  const canvas = previewCanvas.value
  if (!draft || draft.pointerId !== event.pointerId || !canvas) return
  const moved = draft.moved || Math.hypot(event.clientX - draft.startX, event.clientY - draft.startY) >= 4
  if (!moved) return
  const bounds = canvas.getBoundingClientRect()
  if (!bounds.width || !bounds.height) return
  const point = {
    x: Math.max(0, Math.min(1, (event.clientX - bounds.left) / bounds.width)),
    y: Math.max(0, Math.min(1, (event.clientY - bounds.top) / bounds.height)),
  }
  const source = viewToSource(point, documentData.value)
  movingAnchor.value = { ...draft, x: source.x, y: source.y, moved: true }
}

/** 提交已完成的拖动；不移动时保留原有点击编辑备注行为。 */
function finishAnchorMove(event: PointerEvent): void {
  updateAnchorMove(event)
  const draft = movingAnchor.value
  if (!draft || draft.pointerId !== event.pointerId) return
  movingAnchor.value = null
  if (!draft.moved) return

  suppressAnchorClick = true
  window.setTimeout(() => { suppressAnchorClick = false }, 0)
  const anchors = documentData.value.anchors.map(anchor => anchor.id === draft.anchorId
    ? { ...anchor, x: draft.x, y: draft.y }
    : anchor)
  if (JSON.stringify(anchors) !== JSON.stringify(documentData.value.anchors)) {
    commit({ ...documentData.value, anchors })
    if (isExisting.value && !isEditing.value) void persistDetailAnchor(detailLocalRevision)
  }
}

/** 取消被系统打断的触控手势，不将不完整的锚点位置写入文档。 */
function cancelAnchorMove(event: PointerEvent): void {
  if (movingAnchor.value?.pointerId === event.pointerId) movingAnchor.value = null
}

/** 普通点击打开备注编辑；拖动结束产生的合成 click 只消费一次，避免意外打开对话框。 */
function handleAnchorClick(anchor: ImageAnchor): void {
  if (suppressAnchorClick) { suppressAnchorClick = false; return }
  if (!anchorMode.value) editAnchor(anchor)
}

/** 键盘聚焦的锚点可用方向键微调，Shift 配合方向键按较大步幅移动。 */
function moveAnchorByKeyboard(anchor: ImageAnchor, event: KeyboardEvent): void {
  const step = event.shiftKey ? 0.05 : 0.01
  const delta = event.key === 'ArrowLeft' ? { x: -step, y: 0 }
    : event.key === 'ArrowRight' ? { x: step, y: 0 }
      : event.key === 'ArrowUp' ? { x: 0, y: -step }
        : { x: 0, y: step }
  const current = documentData.value.anchors.find(item => item.id === anchor.id)
  if (!current) return
  const position = anchorToView(current, documentData.value)
  const source = viewToSource({
    x: Math.max(0, Math.min(1, position.x + delta.x)),
    y: Math.max(0, Math.min(1, position.y + delta.y)),
  }, documentData.value)
  if (Math.abs(source.x - current.x) < 0.0001 && Math.abs(source.y - current.y) < 0.0001) return
  const anchors = documentData.value.anchors.map(item => item.id === current.id ? { ...item, ...source } : item)
  commit({ ...documentData.value, anchors })
  if (isExisting.value && !isEditing.value) void persistDetailAnchor(detailLocalRevision)
}

/** 详情态的锚点修改立即写回现有文档接口；失败保留本地标记并从顶部提示用户重试。 */
async function persistDetailAnchor(expectedRevision: number): Promise<boolean> {
  if (!isExisting.value || isEditing.value) return true
  anchorSaving.value = true
  try {
    const updated = await queueDetailWrite(editorDataForSave())
    mergePersistedDocument(updated, expectedRevision)
    showTopNotice(t('editor.anchorSaved'), 'success')
    return true
  } catch (cause) {
    showTopNotice(localError(cause, t('editor.anchorSaveFailed')), 'error')
    return false
  } finally {
    anchorSaving.value = false
  }
}

/** 从锚点列表或备注对话框删除指定锚点，操作写入历史并在详情态自动保存。 */
function deleteAnchorById(id: string): void {
  commit({ ...documentData.value, anchors: documentData.value.anchors.filter(anchor => anchor.id !== id) })
  const expectedRevision = detailLocalRevision
  if (anchorDraft.value?.id === id) {
    anchorEditorOpen.value = false
    anchorDraft.value = null
  }
  void persistDetailAnchor(expectedRevision)
}

/** 新增或更新锚点；详情态保存按钮只等待自动写回，不再依赖已取消的底栏。 */
async function saveAnchor(): Promise<void> {
  if (!anchorDraft.value?.note.trim()) {
    if (anchorDraft.value?.id) deleteAnchorById(anchorDraft.value.id)
    else error.value = t('editor.anchorRequired')
    return
  }
  const draft = { ...anchorDraft.value, note: anchorDraft.value.note.trim() }
  const next = { ...documentData.value, anchors: [...documentData.value.anchors] }
  const existingId = draft.id
  if (existingId) next.anchors = next.anchors.map(anchor => anchor.id === existingId ? { ...draft, id: existingId } : anchor)
  else next.anchors.push({ ...draft, id: makeAnchorId() })
  commit(next)
  const expectedRevision = detailLocalRevision
  anchorEditorOpen.value = false
  anchorDraft.value = null
  error.value = ''
  await persistDetailAnchor(expectedRevision)
}

/** 生成锚点 ID；旧浏览器缺少 randomUUID 时使用本地随机回退。 */
function makeAnchorId(): string {
  return typeof crypto.randomUUID === 'function' ? crypto.randomUUID() : `anchor-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

/** 有锚点时先显示三选一确认；没有锚点则直接开始操作。 */
function prepareTransform(action: TransformAction): void {
  // 旋转和裁切回到画布执行，不能让工具浮窗遮住正在操作的预览。
  toolPanel.value = null
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
  finishRgbChange()
  toolPanel.value = null
  saving.value = true
  try {
    if (isExisting.value) {
      busyPhase.value = t('editor.saving')
      const updated = await queueDetailWrite(editorDataForSave())
      record.value = updated
      documentData.value = parseEditorDocument(updated.data)
      history = new DocumentHistory(documentData.value)
      historyRevision.value += 1
      noteDraft.value = typeof documentData.value.image_note === 'string' ? documentData.value.image_note : ''
      notePersisted.value = noteDraft.value
      editSnapshot.value = ''
      isEditing.value = false
      anchorMode.value = false
      cropMode.value = false
      cropSelection.value = null
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
      const created = await api.createInspiration(uploadWorkKey, file.name, data, imageMetadata.value ?? {
        source_file_size: file.size,
        extra: {},
      })
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
  else { isEditing.value = true; history = new DocumentHistory(documentData.value); historyRevision.value += 1 }
})

/** 通过页内返回或浏览器导航离开时，同样确认是否丢弃已有灵感图的未保存调整。 */
onBeforeRouteLeave(async to => {
  // 多步上传和图片保存尚未完成时不能卸载旧空间；切换协调器会保留旧组织供重试。
  // 读取预览不是写入事务，允许用户在打开动画/解码期间返回；卸载代次会丢弃迟到结果。
  // 保存与空间切换仍等待原范围任务，避免将旧空间写入带到新空间。
  if (saving.value || (loading.value && to.path === '/spaces')) return false
  if (tagWritePromise) await tagWritePromise
  if (colorWritePromise) await colorWritePromise
  if (noteDirty.value) await flushImageNote(false)
  await detailWriteChain
  // 自动保存失败时不能静默离页；用户明确放弃后才允许继续导航。
  if (isExisting.value && !isEditing.value && noteDirty.value) {
    try {
      await ElMessageBox.confirm(t('editor.discardChangesText'), t('editor.discardChangesTitle'), {
        confirmButtonText: t('editor.discardChangesConfirm'),
        cancelButtonText: t('common.cancel'),
        type: 'warning',
        customClass: 'editor-glass-dialog',
      })
    } catch { return false }
  }
  if (to.path === '/spaces') {
    if (!isExisting.value && imageReady.value) return false
    if (isExisting.value && isEditing.value && hasUnsavedChanges.value) {
      await save()
      return !isEditing.value || !hasUnsavedChanges.value
    }
  }
  if (!isExisting.value || !isEditing.value || !hasUnsavedChanges.value) return true
  try {
    await ElMessageBox.confirm(t('editor.discardChangesText'), t('editor.discardChangesTitle'), {
      confirmButtonText: t('editor.discardChangesConfirm'),
      cancelButtonText: t('common.cancel'),
      type: 'warning',
      customClass: 'editor-glass-dialog',
    })
    return true
  } catch { return false }
})

onBeforeUnmount(() => {
  // 先交出真实渲染像素，再释放原画布；退出动画不依赖已经卸载的编辑组件。
  captureDetailImage(previewCanvas.value)
  resetZoomViewer()
  stopAnchorEditorViewportTracking()
  decodeVersion += 1
  tagSearchVersion += 1
  if (tagSearchTimer !== undefined) window.clearTimeout(tagSearchTimer)
  if (noteSaveTimer !== undefined) window.clearTimeout(noteSaveTimer)
  if (frameRequest) cancelAnimationFrame(frameRequest)
  resizeObserver?.disconnect()
  releaseCanvas(sourceCanvas.value)
  if (previewCanvas.value) { previewCanvas.value.width = 0; previewCanvas.value.height = 0 }
})
</script>

<template>
  <div class="workspace-page editor-page" :class="{ 'is-editing': isEditing, 'is-detail': isExisting && !isEditing }" :style="[anchorDisplayStyle, detailAmbientStyle]">
    <ContentZoom><main id="main-content" class="editor-main">
      <header class="image-detail-header">
        <el-tooltip :content="t('common.back')" placement="bottom-start" effect="dark" popper-class="image-detail-tooltip" :show-after="260">
          <RouterLink class="detail-back" to="/inspirations" :aria-label="t('common.back')"><el-icon aria-hidden="true"><ArrowLeft /></el-icon></RouterLink>
        </el-tooltip>
        <h1 class="sr-only">{{ isEditing ? t('editor.editTitle') : t('editor.detailTitle') }}</h1>
        <el-button class="detail-more" circle :title="t('editor.metadataPanelTitle')" :aria-label="t('editor.metadataPanelTitle')" :aria-expanded="toolPanel === 'properties' ? 'true' : 'false'" @click="openProperties"><el-icon aria-hidden="true"><MoreFilled /></el-icon></el-button>
      </header>

      <section v-if="!isExisting && !imageReady" class="upload-welcome">
        <el-upload class="upload-dropzone" action="#" :auto-upload="false" :show-file-list="false" accept="image/*,.heic,.heif" :aria-label="t('editor.fileLabel')" :on-change="selectUploadedFile">
          <el-icon class="upload-icon" aria-hidden="true"><UploadFilled /></el-icon><strong>{{ t('editor.chooseImage') }}</strong><span>{{ t('editor.chooseHint') }}</span>
        </el-upload>
        <p v-if="loading" class="loading-inline" role="status"><span class="loading-dot"></span>{{ t('common.loading') }}</p>
      </section>

      <div v-else-if="imageReady" class="editor-workspace">
        <section class="editor-canvas-column" :aria-label="t('editor.previewLabel')">
          <div
            ref="stageBox"
            class="canvas-stage"
            :data-content-zoom-ignore="cropMode || anchorMode || (isExisting && !isEditing) ? 'true' : undefined"
            :class="{ 'anchor-active': anchorMode, 'crop-active': cropMode, 'preview-ready': isExisting && !isEditing }"
            @click="onStageClick"
            @touchstart="beginPreviewPinch"
            @touchmove="movePreviewPinch"
            @touchend="endPreviewPinch"
            @touchcancel="endPreviewPinch"
          >
            <canvas v-if="!isEditing" ref="backdropCanvas" class="canvas-backdrop" aria-hidden="true"></canvas>
            <div v-if="imageReady" class="editor-canvas-wrap" :style="{ width: `${displayDimensions.width}px`, height: `${displayDimensions.height}px` }">
              <canvas
                ref="previewCanvas"
                class="editor-canvas"
                :role="isExisting && !isEditing ? 'button' : 'img'"
                :tabindex="0"
                :aria-label="anchorMode ? t('editor.anchorModeOn') : isExisting && !isEditing ? t('editor.anchorAtImage') : t('editor.previewLabel')"
                @keydown.enter.prevent="addAnchorAtCenter"
                @keydown.space.prevent="addAnchorAtCenter"
                @pointerdown="beginCropSelection"
                @pointermove="moveCropSelection"
                @pointerup="endCropSelection"
                @pointercancel="endCropSelection"
              ></canvas>
              <div v-if="cropMode && cropSelection" class="crop-selection" :style="cropDragStyle" aria-hidden="true"></div>
              <button
                v-for="anchor in visibleAnchors"
                :key="anchor.id"
                type="button"
                :class="['image-anchor', anchor.side, { 'is-moving': movingAnchor?.anchorId === anchor.id }]"
                :style="{ left: anchor.left, top: anchor.top, '--anchor-label-max-width': anchor.labelMaxWidth }"
                :aria-label="t('editor.anchorLabel', { number: anchor.index + 1, note: anchor.note })"
                :aria-description="t('editor.anchorMoveHelp')"
                @pointerdown.stop="beginAnchorMove($event, anchor)"
                @pointermove.stop="updateAnchorMove($event)"
                @pointerup.stop="finishAnchorMove($event)"
                @pointercancel.stop="cancelAnchorMove($event)"
                @keydown.left.prevent="moveAnchorByKeyboard(anchor, $event)"
                @keydown.right.prevent="moveAnchorByKeyboard(anchor, $event)"
                @keydown.up.prevent="moveAnchorByKeyboard(anchor, $event)"
                @keydown.down.prevent="moveAnchorByKeyboard(anchor, $event)"
                @click.stop="handleAnchorClick(anchor)"
              >
                <span class="image-anchor-label">{{ anchor.note }}</span>
                <span class="image-anchor-dot" aria-hidden="true"><span></span></span>
              </button>
              <span v-if="draftAnchorPosition" class="image-anchor is-draft anchor-right" :style="draftAnchorPosition" aria-hidden="true">
                <span class="image-anchor-dot"><span></span></span>
              </span>
            </div>
            <div v-else class="loading-inline" role="status"><span class="loading-dot"></span>{{ t('common.loading') }}</div>
          </div>
          <div v-if="cropMode" class="crop-action-row"><span>{{ t('editor.cropHint') }}</span><el-button class="compact-button" @click="cancelCrop">{{ t('common.cancel') }}</el-button><el-button class="primary-button compact-button" type="primary" :disabled="!cropSelection" @click="applyCrop">{{ t('editor.cropApply') }}</el-button></div>
          <p v-else-if="anchorMode" class="stage-hint">{{ t('editor.anchorModeOn') }}</p>
          <p v-else-if="isEditing && documentData.anchors.length" class="stage-hint">{{ t('editor.anchorMoveHelp') }}</p>
          <p v-if="downsampled" class="stage-note">{{ t('editor.largeImage') }}</p>
        </section>


      </div>

      <section v-if="isExisting && !isEditing && record" class="detail-caption" :aria-label="t('editor.detailCaption')">
        <div class="detail-note-editor" :class="{ 'is-editing': noteEditing }" :aria-busy="noteSaving">
          <el-input
            v-if="noteEditing"
            ref="noteInput"
            :model-value="noteDraft"
            type="textarea"
            :autosize="{ minRows: 1, maxRows: 8 }"
            maxlength="500"
            resize="none"
            :aria-label="t('editor.imageNote')"
            :placeholder="t('editor.notePlaceholder')"
            @update:model-value="handleImageNoteInput"
            @blur="flushImageNote()"
          />
          <button v-else type="button" class="detail-note-trigger" :aria-label="t('editor.noteEditLabel')" @click="startImageNoteEditing">
            <span :class="{ 'is-placeholder': !noteText }">{{ noteText || t('editor.notePlaceholder') }}</span>
          </button>
        </div>
        <div class="detail-tag-row" role="group" :aria-label="t('editor.tags')">
          <button
            v-for="name in record.tag_names ?? []"
            :key="name"
            type="button"
            class="detail-tag-pill"
            :class="{ 'is-pressing': pressedDetailTag === name }"
            :disabled="tagSaving"
            :aria-label="t('editor.tagDeleteHint', { name })"
            @pointerdown="beginDetailTagPress($event, name)"
            @pointermove="moveDetailTagPress"
            @pointerup="endDetailTagPress"
            @pointerleave="endDetailTagPress"
            @pointercancel="endDetailTagPress"
            @contextmenu.prevent="removeDetailTag(name)"
            @keydown.delete.prevent="removeDetailTag(name)"
            @keydown.backspace.prevent="removeDetailTag(name)"
          >{{ name }}</button>
          <button type="button" class="detail-tag-more" aria-controls="detail-tag-picker" :aria-expanded="tagPickerOpen" @click="toggleTagPicker">
            <el-icon aria-hidden="true"><Plus /></el-icon>
            <span>{{ t('editor.tagMore') }}</span>
          </button>
        </div>
      </section>

      <div v-if="error && !imageReady" class="editor-load-error" role="alert"><p>{{ error }}</p><RouterLink class="secondary-button" to="/inspirations">{{ t('common.back') }}</RouterLink></div>
    </main></ContentZoom>
    <div v-if="imageReady && (!isExisting || isEditing)" class="image-edit-dock" :class="{ 'is-editing': isEditing }" role="region" :aria-label="t('editor.tools')">
      <div class="image-dock-tools" role="toolbar" :aria-label="t('editor.tools')">
        <button type="button" :class="{ active: anchorMode || toolPanel === 'anchors' }" @click="openTool('anchors', $event)"><el-icon aria-hidden="true"><Aim /></el-icon><span>{{ t('editor.toolbarAnchors') }}</span></button>
        <button type="button" :disabled="saving || cropMode" :class="{ active: toolPanel === 'adjust' }" @click="openTool('adjust', $event)"><el-icon aria-hidden="true"><Operation /></el-icon><span>{{ t('editor.adjust') }}</span></button>
        <button type="button" :disabled="saving" :class="{ active: toolPanel === 'note' }" @click="openTool('note', $event)"><el-icon aria-hidden="true"><Document /></el-icon><span>{{ t('editor.imageNote') }}</span></button>
      </div>
      <div v-if="isEditing" class="image-dock-save">
        <el-tooltip :content="t('editor.undo')" placement="top" effect="dark" popper-class="image-detail-tooltip" :show-after="260">
          <el-button circle :disabled="!canUndo || saving" :aria-label="t('editor.undo')" @click="undo"><el-icon aria-hidden="true"><RefreshLeft /></el-icon></el-button>
        </el-tooltip>
        <el-tooltip :content="t('editor.redo')" placement="top" effect="dark" popper-class="image-detail-tooltip" :show-after="260">
          <el-button circle :disabled="!canRedo || saving" :aria-label="t('editor.redo')" @click="redo"><el-icon aria-hidden="true"><RefreshRight /></el-icon></el-button>
        </el-tooltip>
        <el-button type="primary" :disabled="saving || loading || cropMode || anchorEditorOpen || !!transformDialog" :loading="saving" @click="save"><el-icon aria-hidden="true"><Check /></el-icon>{{ isExisting ? t('editor.saveEdit') : t('editor.saveNew') }}</el-button>
      </div>
      <p v-if="error" class="image-dock-status form-error" role="alert">{{ error }}</p>
      <p v-else-if="busyPhase" class="image-dock-status" role="status">{{ busyPhase }}</p>
      <p v-else-if="status" class="image-dock-status success-message" role="status">{{ status }}</p>
    </div>
    <EditorToolSurface v-if="toolPanel" :panel="toolPanel" :title="toolTitle" :anchor="toolAnchor" :ambient="detailAmbientStyle['--detail-ambient']" @close="panel => { if (toolPanel === panel) toolPanel = null }">
        <div class="image-tool-panel">
          <div v-if="toolPanel === 'properties' && isExisting && !isEditing" class="detail-tool-actions">
            <el-button @click="openFullscreenFromToolbar"><el-icon aria-hidden="true"><FullScreen /></el-icon>{{ t('editor.fullscreenImage') }}</el-button>
            <el-button type="primary" @click="startFullEditing"><el-icon aria-hidden="true"><Operation /></el-icon>{{ t('editor.editImage') }}</el-button>
          </div>
          <el-input v-if="toolPanel === 'note'" :model-value="noteText" type="textarea" :rows="3" maxlength="500" show-word-limit :aria-label="t('editor.imageNote')" :placeholder="t('editor.imageNotePlaceholder')" @update:model-value="updateImageNote" />
          <dl v-if="toolPanel === 'properties' && sourceCanvas" class="image-metadata-list basic-image-properties">
            <div><dt>{{ t('editor.metadataWidth') }}</dt><dd>{{ new Intl.NumberFormat(locale).format(sourceCanvas.width) }}</dd></div>
            <div><dt>{{ t('editor.metadataHeight') }}</dt><dd>{{ new Intl.NumberFormat(locale).format(sourceCanvas.height) }}</dd></div>
          </dl>
          <section v-if="toolPanel === 'properties'" class="control-section color-profile-section" :aria-label="t('editor.colorProfile')">
            <div class="section-title-row"><h2>{{ t('editor.colorProfile') }}</h2><span v-if="colorAnalysisSaving" class="profile-saving" role="status">{{ t('editor.colorAnalysisSaving') }}</span></div>
            <template v-if="colorAnalysis">
              <div class="palette-bar" role="img" :aria-label="t('editor.paletteSummary')">
                <span v-for="color in colorAnalysis.palette" :key="color.hex" :style="{ width: `${color.percentage}%`, backgroundColor: color.hex }"></span>
                <span class="other-palette-segment" :style="{ width: `${colorAnalysis.other_percentage}%` }"></span>
              </div>
              <ul class="palette-list">
                <li v-for="color in colorAnalysis.palette" :key="color.hex"><span class="color-swatch" :style="{ backgroundColor: color.hex }" aria-hidden="true"></span><span>{{ color.hex }}</span><strong>{{ displayPercent(color.percentage) }}</strong></li>
                <li><span class="color-swatch other-color-swatch" aria-hidden="true"></span><span>{{ t('editor.otherColors') }}</span><strong>{{ displayPercent(colorAnalysis.other_percentage) }}</strong></li>
              </ul>
              <dl class="color-metrics">
                <div><dt>{{ t('editor.averageBrightness') }}</dt><dd>{{ displayPercent(colorAnalysis.average_brightness) }}</dd></div>
                <div><dt>{{ t('editor.averageSaturation') }}</dt><dd>{{ displayPercent(colorAnalysis.average_saturation) }}</dd></div>
              </dl>
              <p v-if="colorAnalysisError" class="profile-error" role="alert">{{ colorAnalysisError }}</p>
            </template>
            <p v-else-if="colorAnalysisError" class="profile-error" role="alert">{{ colorAnalysisError }}</p>
            <p v-else class="profile-saving" role="status">{{ t('editor.colorAnalysisPending') }}</p>
            <div v-if="colorAnalysisError" class="profile-retry-row">
              <el-button text :disabled="colorAnalysisSaving || saving" @click="retryColorAnalysis">{{ t('editor.colorAnalysisRetry') }}</el-button>
            </div>
          </section>

          <section v-if="toolPanel === 'properties' && hasImageMetadata" class="control-section image-metadata-section" :aria-label="t('editor.metadataPanelTitle')">
            <div class="section-title-row"><h2>{{ t('editor.metadataPanelTitle') }}</h2></div>
            <div class="metadata-scroll" tabindex="0" :aria-label="t('editor.metadataPanelTitle')">
            <div v-for="group in imageMetadataSections" :key="group.title" class="metadata-group">
              <h3>{{ group.title }}</h3>
              <dl class="image-metadata-list">
                <div v-for="row in group.rows" :key="row.key"><dt>{{ row.label }}</dt><dd>{{ row.value }}</dd></div>
              </dl>
            </div>
            <a v-if="imageGpsMapUrl" class="metadata-map-link" :href="imageGpsMapUrl" target="_blank" rel="noopener noreferrer">{{ t('editor.metadataOpenMap') }}</a>
            <details v-if="imageMetadataExtraEntries.length" class="metadata-extra">
              <summary>{{ t('editor.metadataExtra') }} ({{ imageMetadataExtraEntries.length }})</summary>
              <dl class="image-metadata-list">
                <div v-for="entry in imageMetadataExtraEntries" :key="entry.key"><dt>{{ entry.key }}</dt><dd><details v-if="entry.complex"><summary>{{ t('editor.metadataRawValue') }}</summary><pre>{{ entry.value }}</pre></details><template v-else>{{ entry.value }}</template></dd></div>
              </dl>
            </details>
            </div>
          </section>

          <p v-if="toolPanel === 'anchors' && !isEditing && !documentData.anchors.length" class="notes-empty">{{ t('editor.anchorEmpty') }}</p>
          <section v-if="toolPanel === 'anchors'" class="control-section anchor-size-section">
            <div class="section-title-row"><h2>{{ t('editor.anchorDisplaySize') }}</h2><output class="anchor-size-value">{{ anchorDisplayStore.size }}%</output></div>
            <el-slider :model-value="anchorDisplayStore.size" :min="70" :max="150" :step="5" :show-tooltip="false" :aria-label="t('editor.anchorDisplaySize')" @input="anchorDisplayStore.setSize(Number($event))" />

          </section>

          <template v-if="isEditing && (toolPanel === 'adjust' || toolPanel === 'anchors')">
            <section v-if="toolPanel === 'adjust'" class="control-section transform-section">
              <div class="section-title-row"><h2>{{ t('editor.editTitle') }}</h2></div>
              <div class="transform-buttons">
                <el-button class="tool-button" :disabled="cropMode" @click="prepareTransform('rotate')"><el-icon aria-hidden="true"><RefreshRight /></el-icon>{{ t('editor.rotate') }}</el-button>
                <el-button class="tool-button" :disabled="cropMode" @click="prepareTransform('crop')"><el-icon aria-hidden="true"><Crop /></el-icon>{{ t('editor.crop') }}</el-button>
                <el-button class="tool-button" :disabled="cropMode" @click="prepareTransform('reset')"><el-icon aria-hidden="true"><FullScreen /></el-icon>{{ t('editor.resetCrop') }}</el-button>
              </div>
            </section>

            <section v-if="toolPanel === 'adjust'" class="control-section color-section">
              <div class="section-title-row"><h2>{{ t('editor.rgb') }}</h2></div>
              <label class="color-slider red-slider"><span>{{ t('editor.red') }}</span><el-slider :model-value="documentData.rgb.red" :min="-100" :max="100" :show-tooltip="false" :aria-label="t('editor.red')" @input="updateRgb('red', String($event))" @change="finishRgbChange" /><output>{{ documentData.rgb.red }}</output></label>
              <label class="color-slider green-slider"><span>{{ t('editor.green') }}</span><el-slider :model-value="documentData.rgb.green" :min="-100" :max="100" :show-tooltip="false" :aria-label="t('editor.green')" @input="updateRgb('green', String($event))" @change="finishRgbChange" /><output>{{ documentData.rgb.green }}</output></label>
              <label class="color-slider blue-slider"><span>{{ t('editor.blue') }}</span><el-slider :model-value="documentData.rgb.blue" :min="-100" :max="100" :show-tooltip="false" :aria-label="t('editor.blue')" @input="updateRgb('blue', String($event))" @change="finishRgbChange" /><output>{{ documentData.rgb.blue }}</output></label>
            </section>

            <section v-if="toolPanel === 'anchors' && documentData.anchors.length" class="control-section notes-section">
              <div class="section-title-row"><div><h2>{{ t('editor.anchors') }}</h2><span class="notes-count">{{ documentData.anchors.length }}</span></div></div>

              <p v-if="!documentData.anchors.length" class="notes-empty">{{ t('editor.anchorEmpty') }}</p>
              <div v-else class="note-list">
                <div v-for="(anchor, index) in documentData.anchors" :key="anchor.id" class="note-list-item-row">
                  <el-button class="note-list-item" @click="editAnchor(anchor)"><span class="note-index">{{ index + 1 }}</span><span>{{ anchor.note }}</span><el-icon class="note-arrow" aria-hidden="true"><ArrowRight /></el-icon></el-button>
                  <el-tooltip :content="t('editor.anchorDeleteLabel', { number: index + 1 })" placement="top" effect="dark" popper-class="image-detail-tooltip" :show-after="260">
                    <el-button class="note-delete-button" text :aria-label="t('editor.anchorDeleteLabel', { number: index + 1 })" @click="deleteAnchorById(anchor.id)"><el-icon aria-hidden="true"><Close /></el-icon></el-button>
                  </el-tooltip>
                </div>
              </div>
            </section>
          </template>

          <section v-else-if="toolPanel === 'anchors' && documentData.anchors.length" class="control-section detail-notes-section">
            <div class="section-title-row"><div><h2>{{ t('editor.anchors') }}</h2><span class="notes-count">{{ documentData.anchors.length }}</span></div></div>
            <ul class="detail-note-list"><li v-for="(anchor, index) in documentData.anchors" :key="anchor.id"><span class="note-index">{{ index + 1 }}</span><span>{{ anchor.note }}</span></li></ul>
          </section>
          <p v-if="status && !isEditing" class="success-message" role="status">{{ status }}</p>
        </div>
    </EditorToolSurface>

    <el-dialog :model-value="!!transformDialog" class="editor-glass-dialog" :title="t('editor.transformTitle')" width="min(500px, calc(100vw - 32px))" @close="transformDialog = null">
      <p>{{ t('editor.transformText') }}</p>
      <template #footer>
        <div class="dialog-actions">
          <el-button text @click="transformDialog = null">{{ t('common.cancel') }}</el-button>
          <el-button @click="performTransform(transformDialog!, false)">{{ t('editor.keepAnchors') }}</el-button>
          <el-button type="primary" @click="performTransform(transformDialog!, true)">{{ t('editor.clearAnchors') }}</el-button>
        </div>
      </template>
    </el-dialog>

    <Teleport to="body">
      <Transition name="anchor-editor">
        <section
          v-if="anchorEditorOpen && anchorDraft"
          class="anchor-inline-editor"
          role="dialog"
          :aria-label="anchorDraft.id ? t('editor.anchorEdit') : t('editor.anchorMode')"
          @keydown.esc.prevent="closeAnchorEditor"
        >
          <el-input
            ref="anchorEditorInput"
            v-model="anchorDraft.note"
            class="anchor-editor-input"
            maxlength="500"
            :placeholder="t('editor.anchorPlaceholder')"
            :aria-label="t('editor.anchorPlaceholder')"
            enterkeyhint="done"
            @keyup.enter.prevent="saveAnchor"
          />
          <el-button
            class="anchor-editor-confirm"
            type="primary"
            native-type="button"
            :loading="anchorSaving"
            :disabled="!anchorDraft.note.trim() && !anchorDraft.id"
            @click="saveAnchor"
          >
            <el-icon aria-hidden="true"><Delete v-if="anchorDraft.id && !anchorDraft.note.trim()" /><Check v-else /></el-icon>
            <span>{{ anchorDraft.id && !anchorDraft.note.trim() ? t('editor.anchorDelete') : t('editor.anchorComplete') }}</span>
          </el-button>
          <p v-if="error" class="anchor-editor-error" role="alert">{{ error }}</p>
        </section>
      </Transition>
    </Teleport>

    <Teleport to="body">
      <Transition name="detail-tag-picker">
        <div v-if="tagPickerOpen" class="detail-tag-picker-layer" @click.self="closeTagPicker">
          <section
            id="detail-tag-picker"
            class="detail-tag-picker"
            role="dialog"
            aria-modal="true"
            :aria-label="t('editor.tags')"
            :style="detailAmbientStyle"
            @keydown.esc.prevent="closeTagPicker"
          >
            <header class="detail-tag-picker-header">
              <h2>{{ t('editor.tags') }}</h2>
              <button type="button" :aria-label="t('common.close')" @click="closeTagPicker"><el-icon aria-hidden="true"><Close /></el-icon></button>
            </header>
            <div class="detail-tag-picker-input">
              <el-input
                v-model="detailTagQuery"
                :aria-label="t('editor.tagSearchPlaceholder')"
                :placeholder="t('editor.tagSearchPlaceholder')"
                maxlength="50"
                @update:model-value="handleTagQueryInput"
                @keyup.enter.prevent="addTypedDetailTag"
              />
              <el-button type="primary" :disabled="!detailTagQuery.trim() || tagSaving" :loading="tagSaving" @click="addTypedDetailTag">{{ t('editor.tagAdd') }}</el-button>
            </div>
            <p v-if="tagSearchError" class="profile-error" role="alert">{{ tagSearchError }}</p>
            <p v-else-if="tagSaveError" class="profile-error" role="alert">{{ tagSaveError }}</p>
            <p v-else-if="tagSaveStatus" class="tag-save-status" role="status">{{ tagSaveStatus }}</p>
            <div class="detail-tag-picker-list">
              <button
                v-for="name in detailTagOptions"
                :key="name"
                type="button"
                :class="{ selected: tagNames.includes(name) }"
                :aria-pressed="tagNames.includes(name)"
                :disabled="tagSaving"
                @click="toggleDetailTag(name)"
              >
                <el-icon v-if="tagNames.includes(name)" aria-hidden="true"><Check /></el-icon>
                <span>{{ name }}</span>
              </button>
              <p v-if="!detailTagOptions.length && !tagOptionsLoading">{{ t('editor.tagPickerEmpty') }}</p>
            </div>
          </section>
        </div>
      </Transition>
    </Teleport>

    <Teleport to="body">
      <div
        ref="zoomLayer"
        class="inspiration-zoom-layer"
        :class="{ 'is-active': zoomActive, 'is-fullscreen': zoomFullscreen }"
        :style="anchorDisplayStyle"
        :aria-hidden="!zoomActive"
        :aria-modal="zoomFullscreen ? 'true' : 'false'"
        role="dialog"
        @click.self="closeInspirationViewer"
        @keydown.esc.prevent="closeInspirationViewer"
        @touchstart="beginFullscreenPinch"
        @touchmove="moveFullscreenPinch"
        @touchend="endFullscreenPinch"
        @touchcancel="endFullscreenPinch"
      >
        <div ref="zoomShell" class="inspiration-zoom-image">
          <canvas ref="zoomCanvas" role="img" :aria-label="t('editor.fullscreenImage')"></canvas>
          <div class="inspiration-zoom-anchors" aria-hidden="true">
            <span
              v-for="anchor in visibleAnchors"
              :key="anchor.id"
              class="inspiration-zoom-anchor"
              :class="anchor.side"
              :style="{ left: anchor.left, top: anchor.top }"
            >
              <span class="inspiration-zoom-anchor-dot"></span>
              <span class="inspiration-zoom-anchor-label">{{ anchor.note }}</span>
            </span>
          </div>
        </div>
        <button
          v-if="zoomFullscreen"
          ref="zoomCloseButton"
          type="button"
          class="inspiration-zoom-close"
          :aria-label="t('common.close')"
          @click="closeInspirationViewer"
        ><el-icon aria-hidden="true"><Close /></el-icon></button>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
/* 图片占据主要工作区；桌面使用悬浮工具 Dock，手机沿底部安全区铺满。 */
.editor-main { padding-top: 12px; padding-bottom: 156px; }
.editor-page.is-detail .editor-main { padding-bottom: 28px; }
.image-detail-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.detail-back { display: grid; place-items: center; width: 44px; height: 44px; border: 1px solid var(--line); border-radius: 50%; color: var(--ink); background: var(--surface); }
.detail-back:hover { background: var(--accent-soft); color: var(--accent); }
.detail-back:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
.detail-more { width: 44px; height: 44px; font-size: 22px; }
.editor-workspace { display: block; }
.canvas-stage { height: calc(100dvh - 228px); min-height: 280px; max-height: none; background-image: none; border: 0; background-color: var(--surface-soft); isolation: isolate; }
.is-detail .canvas-stage { background-color: #0c0c0e; }
.canvas-backdrop { position: absolute; z-index: -1; inset: -36px; width: calc(100% + 72px); height: calc(100% + 72px); filter: blur(28px) saturate(1.15); opacity: .62; transform: scale(1.035); }
.is-detail .editor-canvas-wrap { box-shadow: none; }
.image-edit-dock { position: fixed; z-index: 45; bottom: 20px; left: calc(50% + 112px); transform: translateX(-50%); display: flex; flex-wrap: wrap; align-items: center; gap: 12px; max-width: calc(100% - 290px); padding: 8px; border: 1px solid color-mix(in srgb, var(--ink) 10%, transparent); border-radius: 24px; background: radial-gradient(150% 180% at 8% 0%, color-mix(in srgb, var(--detail-ambient, var(--accent)) 20%, transparent), transparent 58%), linear-gradient(180deg, rgb(255 255 255 / 24%), rgb(255 255 255 / 5%)), color-mix(in srgb, var(--ink) 7%, transparent); box-shadow: inset 0 1px 0 rgb(255 255 255 / 50%), inset 0 -1px 0 rgb(0 0 0 / 8%), 0 12px 34px rgb(0 0 0 / 15%); backdrop-filter: blur(30px) saturate(190%); -webkit-backdrop-filter: blur(30px) saturate(190%); }
.detail-caption { display: grid; gap: 8px; max-width: 760px; margin: 16px auto 0; padding: 0 8px; color: var(--ink); }
.detail-caption p { margin: 0; color: var(--ink-soft); font-size: 14px; line-height: 1.6; white-space: pre-wrap; overflow-wrap: anywhere; }
.detail-note-editor { min-height: 44px; }
.detail-note-trigger { display: flex; width: 100%; min-height: 44px; align-items: flex-start; padding: 4px 0; border: 0; background: transparent; color: var(--ink); cursor: text; font: inherit; font-size: 14px; line-height: 1.72; text-align: left; white-space: pre-wrap; overflow-wrap: anywhere; touch-action: manipulation; }
.detail-note-trigger span { width: 100%; }
.detail-note-trigger span.is-placeholder { color: var(--muted); }
.detail-note-trigger:hover { color: var(--ink); }
.detail-note-trigger:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; border-radius: 12px; }
.detail-note-editor :deep(.el-textarea__inner) { min-height: 32px !important; padding: 4px 0; border: 0; border-radius: 0; background: transparent; box-shadow: none; color: var(--ink); font: inherit; font-size: 14px; line-height: 1.72; }
.detail-note-editor :deep(.el-textarea__inner:focus) { box-shadow: none; }
.detail-tag-row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.detail-tag-pill, .detail-tag-more { position: relative; display: inline-flex; min-height: 34px; align-items: center; justify-content: center; gap: 4px; overflow: hidden; padding: 6px 12px; border: 1px solid color-mix(in srgb, var(--line) 78%, transparent); border-radius: 999px; background: color-mix(in srgb, var(--surface) 74%, transparent); color: var(--ink-soft); cursor: pointer; font: inherit; font-size: 12px; line-height: 1; white-space: nowrap; touch-action: manipulation; user-select: none; -webkit-touch-callout: none; }
.detail-tag-pill:hover, .detail-tag-more:hover { border-color: color-mix(in srgb, var(--ink) 18%, transparent); color: var(--ink); }
.detail-tag-pill:focus-visible, .detail-tag-more:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.detail-tag-pill.is-pressing { border-color: color-mix(in srgb, var(--accent) 46%, transparent); background: color-mix(in srgb, var(--accent) 16%, var(--surface)); color: var(--ink); transform: scale(.97); }
.detail-tag-pill.is-pressing::after { position: absolute; right: 9px; bottom: 4px; left: 9px; height: 2px; border-radius: 99px; background: var(--accent); content: ''; transform-origin: left; animation: detail-tag-hold 500ms linear forwards; }
.detail-tag-more { border-color: color-mix(in srgb, var(--accent) 30%, var(--line)); background: color-mix(in srgb, var(--accent) 10%, transparent); color: var(--accent-hover); }
.detail-tag-more .el-icon { font-size: 14px; }
@keyframes detail-tag-hold { from { transform: scaleX(0); } to { transform: scaleX(1); } }
.detail-tag-picker-layer { position: fixed; z-index: 2003; inset: 0; display: flex; align-items: flex-end; justify-content: center; padding: 16px 12px calc(94px + env(safe-area-inset-bottom)); background: rgb(0 0 0 / 7%); }
.detail-tag-picker { display: flex; width: min(420px, 100%); max-height: min(54dvh, 460px); flex-direction: column; gap: 14px; overflow: hidden; padding: 16px; border: 1px solid color-mix(in srgb, var(--ink) 12%, transparent); border-radius: 28px; background: radial-gradient(130% 180% at 8% 0%, color-mix(in srgb, var(--detail-ambient, var(--accent)) 24%, transparent), transparent 60%), radial-gradient(120% 170% at 92% 100%, rgb(255 255 255 / 16%), transparent 58%), linear-gradient(180deg, rgb(255 255 255 / 25%), rgb(255 255 255 / 4%)), color-mix(in srgb, var(--ink) 8%, transparent); box-shadow: inset 0 1px 0 rgb(255 255 255 / 62%), inset 0 -1px 0 rgb(0 0 0 / 9%), 0 24px 64px rgb(0 0 0 / 22%); backdrop-filter: blur(34px) saturate(200%); -webkit-backdrop-filter: blur(34px) saturate(200%); }
.detail-tag-picker-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.detail-tag-picker-header h2 { margin: 0; color: var(--ink); font-size: 18px; font-weight: 600; letter-spacing: -.02em; }
.detail-tag-picker-header button { display: grid; width: 40px; height: 40px; place-items: center; border: 1px solid color-mix(in srgb, var(--ink) 10%, transparent); border-radius: 50%; background: color-mix(in srgb, var(--surface) 28%, transparent); color: var(--ink-soft); cursor: pointer; font-size: 17px; }
.detail-tag-picker-input { display: flex; align-items: center; gap: 8px; }
.detail-tag-picker-input :deep(.el-input) { flex: 1; min-width: 0; }
.detail-tag-picker-input :deep(.el-input__wrapper) { border-radius: 15px; background: color-mix(in srgb, var(--surface) 36%, transparent); box-shadow: inset 0 1px 0 rgb(255 255 255 / 52%), 0 0 0 1px color-mix(in srgb, var(--ink) 9%, transparent); }
.detail-tag-picker-input .el-button { margin: 0; min-width: 72px; border-radius: 15px; }
.detail-tag-picker-list { display: flex; min-height: 56px; flex-wrap: wrap; align-content: flex-start; gap: 8px; overflow-y: auto; padding: 2px; overscroll-behavior: contain; }
.detail-tag-picker-list button { display: inline-flex; min-height: 38px; align-items: center; gap: 5px; padding: 8px 14px; border: 1px solid color-mix(in srgb, var(--ink) 10%, transparent); border-radius: 999px; background: color-mix(in srgb, var(--surface) 46%, transparent); color: var(--ink-soft); cursor: pointer; font: inherit; font-size: 13px; }
.detail-tag-picker-list button.selected { border-color: color-mix(in srgb, var(--accent) 34%, transparent); background: color-mix(in srgb, var(--accent) 17%, transparent); color: var(--accent-hover); }
.detail-tag-picker-list button:disabled { opacity: .52; cursor: wait; }
.detail-tag-picker-list button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.detail-tag-picker-list p { margin: 8px 2px; color: var(--muted); font-size: 13px; }
.detail-tag-picker-enter-active, .detail-tag-picker-leave-active { transition: opacity 180ms ease; }
.detail-tag-picker-enter-active .detail-tag-picker, .detail-tag-picker-leave-active .detail-tag-picker { transition: transform 220ms cubic-bezier(0.22, 1, 0.36, 1), opacity 180ms ease; }
.detail-tag-picker-enter-from, .detail-tag-picker-leave-to { opacity: 0; }
.detail-tag-picker-enter-from .detail-tag-picker, .detail-tag-picker-leave-to .detail-tag-picker { opacity: 0; transform: translateY(16px) scale(.98); }
.image-dock-tools { display: flex; gap: 4px; flex: 1; }
.image-dock-tools button { display: flex; flex: 1; min-width: 70px; min-height: 56px; flex-direction: column; justify-content: center; align-items: center; gap: 5px; padding: 6px 12px; border: 0; border-radius: 16px; background: transparent; color: var(--ink-soft); cursor: pointer; white-space: nowrap; font: inherit; font-size: 12px; }
.image-dock-tools button .el-icon { font-size: 22px; }
.image-dock-tools button:hover:not(:disabled), .image-dock-tools button.active { background: color-mix(in srgb, var(--accent) 16%, transparent); color: var(--accent-hover); }
.image-dock-tools button:disabled { opacity: .4; cursor: default; }
.image-dock-tools button:focus-visible { outline: 2px solid var(--accent); }
.image-dock-save { display: flex; align-items: center; justify-content: flex-end; gap: 8px; padding: 4px 8px; }
.image-dock-save .el-button { margin: 0; min-height: 40px; }
.image-dock-status { flex-basis: 100%; margin: 0; padding: 0 12px 6px; font-size: 12px; }
.image-tool-panel .control-section { padding: 16px 0; }
.image-tool-panel .control-section:first-child { padding-top: 0; }
.detail-tool-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; padding-bottom: 16px; border-bottom: 1px solid color-mix(in srgb, var(--ink) 8%, transparent); }
.detail-tool-actions .el-button { margin: 0; min-height: 40px; }
.image-tool-panel :deep(.el-select__tags-text) { color: var(--ink); }
.basic-image-properties { margin: 0; padding-bottom: 16px; }
.image-tool-panel .metadata-scroll { height: auto; max-height: 45dvh; }
@media (max-width: 1023px) {
  .image-edit-dock { left: 50%; max-width: calc(100% - 32px); }
}
@media (max-width: 768px) {
  .editor-page { padding-bottom: 0 !important; }
  .editor-main { position: relative; width: 100%; margin: 0; padding: 0 0 150px !important; }
  .editor-page.is-detail .editor-main { padding-bottom: calc(24px + env(safe-area-inset-bottom)) !important; }
  .image-detail-header { position: absolute; inset: max(12px, env(safe-area-inset-top)) 12px auto; z-index: 4; margin: 0; pointer-events: none; }
  .image-detail-header > * { pointer-events: auto; }
  .detail-back, .detail-more { width: 48px; height: 48px; border: 1px solid rgb(255 255 255 / 28%); background: linear-gradient(145deg, rgb(255 255 255 / 12%), rgb(255 255 255 / 3%)), rgb(16 16 18 / 7%); color: #fff; text-shadow: 0 1px 3px rgb(0 0 0 / 38%); backdrop-filter: blur(26px) saturate(180%); -webkit-backdrop-filter: blur(26px) saturate(180%); box-shadow: inset 0 1px 0 rgb(255 255 255 / 34%), 0 8px 26px rgb(0 0 0 / 11%); transition: transform 160ms ease, background-color 160ms ease; }
  .detail-more { --el-button-bg-color: transparent; --el-button-border-color: transparent; --el-button-hover-bg-color: rgb(255 255 255 / 16%); --el-button-hover-border-color: transparent; --el-button-active-bg-color: rgb(255 255 255 / 24%); --el-button-active-border-color: transparent; }
  .detail-back:active, .detail-more:active { transform: scale(.94); }
  .canvas-stage { width: 100%; height: auto; min-height: 0; aspect-ratio: 2 / 3; border-radius: 0; background-color: var(--surface-soft); touch-action: pan-y; }
  .editor-page.is-detail .canvas-stage { height: auto; min-height: 0; max-height: none; aspect-ratio: 2 / 3; background-color: #09090b; }
  .editor-page.is-detail .canvas-stage::after { position: absolute; z-index: 0; inset: 0; background: linear-gradient(180deg, rgb(0 0 0 / 9%), transparent 18%, transparent 82%, rgb(0 0 0 / 6%)); content: ''; pointer-events: none; }
  .editor-page.is-detail .editor-canvas-wrap { z-index: 1; box-shadow: none; }
  .editor-page.is-detail .canvas-backdrop { inset: -44px; width: calc(100% + 88px); height: calc(100% + 88px); filter: blur(34px) saturate(1.18); opacity: .58; transform: scale(1.04); }
  .preview-ready .editor-canvas { cursor: crosshair; -webkit-touch-callout: none; }
  .editor-page.is-editing .canvas-stage { height: calc(100dvh - 280px); }
  .detail-caption { gap: 12px; margin: 0; padding: 18px 18px 28px; }
  .detail-note-trigger { min-height: 48px; padding: 2px 0 6px; font-size: 16px; letter-spacing: -.012em; line-height: 1.72; }
  .detail-note-editor :deep(.el-textarea__inner) { min-height: 34px !important; padding: 2px 0 6px; font-size: 16px; letter-spacing: -.012em; line-height: 1.72; }
  .detail-tag-row { flex-wrap: nowrap; gap: 8px; overflow-x: auto; padding: 1px 0 4px; scrollbar-width: none; overscroll-behavior-inline: contain; }
  .detail-tag-row::-webkit-scrollbar { display: none; }
  .detail-tag-pill, .detail-tag-more { min-height: 38px; flex: 0 0 auto; padding: 8px 14px; font-size: 13px; }
  .detail-tag-more { position: sticky; right: 0; border-color: color-mix(in srgb, var(--accent) 28%, var(--line)); background: radial-gradient(120% 150% at 20% 0%, color-mix(in srgb, var(--detail-ambient, var(--accent)) 20%, transparent), transparent 64%), color-mix(in srgb, var(--surface) 45%, transparent); box-shadow: inset 0 1px 0 rgb(255 255 255 / 46%), 0 4px 14px rgb(0 0 0 / 7%); backdrop-filter: blur(18px) saturate(180%); -webkit-backdrop-filter: blur(18px) saturate(180%); }
  .image-edit-dock { right: 12px; bottom: calc(10px + env(safe-area-inset-bottom)); left: 12px; transform: none; max-width: none; overflow: hidden; padding: 7px; gap: 4px; border: 1px solid color-mix(in srgb, var(--ink) 12%, transparent); border-radius: 24px; background: radial-gradient(120% 180% at 10% 0%, color-mix(in srgb, var(--detail-ambient, var(--accent)) 24%, transparent), transparent 58%), radial-gradient(120% 170% at 90% 100%, rgb(255 255 255 / 16%), transparent 58%), linear-gradient(180deg, rgb(255 255 255 / 25%), rgb(255 255 255 / 4%)), color-mix(in srgb, var(--ink) 8%, transparent); box-shadow: inset 0 1px 0 rgb(255 255 255 / 62%), inset 0 -1px 0 rgb(0 0 0 / 9%), 0 14px 38px rgb(0 0 0 / 16%); backdrop-filter: blur(32px) saturate(200%); -webkit-backdrop-filter: blur(32px) saturate(200%); }
  .image-edit-dock::before { position: absolute; z-index: 0; inset: 0; border-radius: inherit; background: linear-gradient(118deg, rgb(255 255 255 / 18%), transparent 38%, rgb(255 255 255 / 5%) 76%, transparent); content: ''; pointer-events: none; }
  .image-dock-tools { flex-basis: 100%; }
  .image-dock-tools button { min-width: 0; min-height: 54px; gap: 4px; padding: 5px 6px; border-radius: 18px; font-size: 11px; }
  .image-dock-tools button .el-icon { font-size: 21px; }
  .image-dock-tools button:active:not(:disabled) { transform: scale(.96); }
  .image-dock-save { width: 100%; padding-top: 6px; border-top: 1px solid var(--line); }
  .image-dock-save .el-button:last-child { margin-left: auto; }
}

.image-anchor.is-draft { pointer-events: none; opacity: .76; }
.anchor-inline-editor { position: fixed; z-index: 2500; right: 0; bottom: var(--anchor-keyboard-bottom, 0px); left: 0; display: flex; min-height: 68px; align-items: center; gap: 10px; padding: 9px max(14px, env(safe-area-inset-right)) max(9px, env(safe-area-inset-bottom)) max(14px, env(safe-area-inset-left)); border: 0; background: rgb(12 13 15 / 92%); color: #fff; box-shadow: none; backdrop-filter: blur(18px) saturate(140%); -webkit-backdrop-filter: blur(18px) saturate(140%); }
.anchor-editor-input { min-width: 0; flex: 1; }
.anchor-editor-input :deep(.el-input__wrapper) { min-height: 48px; padding: 0 2px; border: 0; border-radius: 0; background: transparent !important; background-image: none !important; box-shadow: none !important; }
.anchor-editor-input :deep(.el-input__inner) { color: #fff; font-size: 17px; font-weight: 500; }
.anchor-editor-input :deep(.el-input__inner::placeholder) { color: rgb(255 255 255 / 52%); }
.anchor-editor-confirm.el-button { min-width: 88px; min-height: 48px; flex: 0 0 auto; margin: 0; padding: 0 18px; border: 0; border-radius: 999px; background: #f3e600 !important; background-image: none !important; box-shadow: none !important; color: #171717 !important; font-weight: 700; opacity: 1; }
.anchor-editor-confirm.el-button:hover, .anchor-editor-confirm.el-button:focus { border: 0; background: #fff33d !important; color: #171717 !important; }
.anchor-editor-confirm.el-button.is-disabled { background: #f3e600 !important; color: rgb(23 23 23 / 46%) !important; opacity: 1; }
.anchor-editor-confirm:focus-visible { outline: 2px solid #fff; outline-offset: 2px; }
.anchor-editor-confirm .el-icon { margin-right: 4px; }
.anchor-editor-error { position: absolute; left: max(14px, env(safe-area-inset-left)); bottom: calc(100% + 6px); margin: 0; color: #ffb4ac; font-size: 12px; line-height: 1.5; text-shadow: 0 1px 3px #000; }
.anchor-editor-enter-active, .anchor-editor-leave-active { transition: opacity 160ms ease, transform 180ms cubic-bezier(.22, 1, .36, 1); }
.anchor-editor-enter-from, .anchor-editor-leave-to { opacity: 0; transform: translateY(14px); }
.inspiration-zoom-layer { position: fixed; z-index: 2400; inset: 0; overflow: hidden; background: transparent; opacity: 0; visibility: hidden; pointer-events: none; touch-action: none; isolation: isolate; }
.inspiration-zoom-layer.is-active { opacity: 1; visibility: visible; }
.inspiration-zoom-layer.is-fullscreen { pointer-events: auto; }
.inspiration-zoom-image { position: absolute; transform-origin: center; will-change: transform; }
.inspiration-zoom-image canvas { display: block; width: 100%; height: 100%; }
.inspiration-zoom-anchors { position: absolute; z-index: 1; inset: 0; pointer-events: none; }
.inspiration-zoom-anchor { position: absolute; width: 0; height: 0; color: #fff; transform: scale(var(--zoom-anchor-inverse, 1)); transform-origin: 0 0; }
.inspiration-zoom-anchor-dot { position: absolute; top: 0; left: 0; width: var(--anchor-dot-size, 24px); height: var(--anchor-dot-size, 24px); border: 2px solid rgb(255 255 255 / 64%); border-radius: 50%; background: rgb(28 30 31 / 40%); box-shadow: 0 2px 8px rgb(0 0 0 / 18%); transform: translate(-50%, -50%); }
.inspiration-zoom-anchor-dot::after { position: absolute; top: 50%; left: 50%; width: var(--anchor-dot-core-size, 10px); height: var(--anchor-dot-core-size, 10px); border-radius: 50%; background: #f3df00; box-shadow: 0 0 0 1px rgb(0 0 0 / 12%); content: ''; transform: translate(-50%, -50%); }
.inspiration-zoom-anchor-label { position: absolute; top: 0; display: block; max-width: min(240px, 48vw); overflow: hidden; padding: var(--anchor-label-padding-y, 7px) var(--anchor-label-padding-x, 11px); border: 1px solid rgb(255 255 255 / 34%); border-radius: 999px; background: rgb(22 24 25 / 28%); box-shadow: 0 3px 12px rgb(0 0 0 / 16%); color: #fff; font-size: var(--anchor-label-font-size, 12px); font-weight: 500; line-height: 1.35; text-overflow: ellipsis; white-space: nowrap; text-shadow: 0 1px 2px rgb(0 0 0 / 62%); transform: translateY(-50%); }
.inspiration-zoom-anchor.anchor-left .inspiration-zoom-anchor-label { right: calc(var(--anchor-dot-half, 12px) + var(--anchor-gap, 6px)); }
.inspiration-zoom-anchor.anchor-right .inspiration-zoom-anchor-label { left: calc(var(--anchor-dot-half, 12px) + var(--anchor-gap, 6px)); }
.inspiration-zoom-close { position: absolute; z-index: 2; top: max(12px, env(safe-area-inset-top)); right: max(12px, env(safe-area-inset-right)); display: grid; width: 48px; height: 48px; place-items: center; border: 1px solid rgb(255 255 255 / 30%); border-radius: 50%; background: rgb(255 255 255 / 12%); color: #fff; cursor: pointer; font-size: 22px; backdrop-filter: blur(24px) saturate(180%); -webkit-backdrop-filter: blur(24px) saturate(180%); box-shadow: inset 0 1px 0 rgb(255 255 255 / 34%), 0 8px 26px rgb(0 0 0 / 28%); }
.inspiration-zoom-close:active { transform: scale(.94); }
.inspiration-zoom-close:focus-visible { outline: 2px solid #fff; outline-offset: 3px; }
:global(body.inspiration-zoom-open) { overflow: hidden !important; overscroll-behavior: none; }
:global(body.image-detail-route .el-popper.is-dark) { border: 1px solid rgb(255 255 255 / 24%); border-radius: 12px; background: linear-gradient(180deg, rgb(255 255 255 / 18%), rgb(255 255 255 / 4%)), rgb(16 16 18 / 46%); color: #fff; backdrop-filter: blur(22px) saturate(180%); -webkit-backdrop-filter: blur(22px) saturate(180%); box-shadow: inset 0 1px 0 rgb(255 255 255 / 30%), 0 10px 28px rgb(0 0 0 / 18%); }
:global(body.image-detail-route .el-popper.is-dark .el-popper__arrow::before) { border-color: rgb(255 255 255 / 18%); background: rgb(48 48 52 / 64%); }
:global(.editor-glass-dialog.el-dialog), :global(.editor-glass-dialog.el-message-box) { overflow: hidden; border: 1px solid color-mix(in srgb, var(--ink) 12%, transparent); background: radial-gradient(140% 180% at 8% 0%, color-mix(in srgb, var(--detail-ambient, var(--accent)) 18%, transparent), transparent 62%), linear-gradient(180deg, rgb(255 255 255 / 20%), rgb(255 255 255 / 3%)), color-mix(in srgb, var(--ink) 8%, transparent); box-shadow: inset 0 1px 0 rgb(255 255 255 / 58%), 0 24px 70px rgb(0 0 0 / 24%); backdrop-filter: blur(34px) saturate(190%); -webkit-backdrop-filter: blur(34px) saturate(190%); }
:global(.editor-glass-dialog .el-dialog__header), :global(.editor-glass-dialog .el-message-box__header) { border-bottom: 1px solid color-mix(in srgb, var(--ink) 8%, transparent); }
:global(.editor-glass-message.el-message) { border: 1px solid color-mix(in srgb, var(--ink) 12%, transparent); background: linear-gradient(180deg, rgb(255 255 255 / 20%), rgb(255 255 255 / 3%)), color-mix(in srgb, var(--ink) 8%, transparent); backdrop-filter: blur(26px) saturate(180%); -webkit-backdrop-filter: blur(26px) saturate(180%); box-shadow: inset 0 1px 0 rgb(255 255 255 / 48%), 0 14px 38px rgb(0 0 0 / 18%); }
@media (prefers-reduced-motion: reduce) {
  .detail-back, .detail-more, .image-dock-tools button, .detail-tag-picker, .detail-tag-picker-layer, .anchor-editor-enter-active, .anchor-editor-leave-active { transition: none; }
}
</style>

<style scoped>
/* 预览快照在画布上方，保留可见、可点按的返回和属性按钮。 */
.image-detail-header { z-index: 2; }
.image-dock-tools button { min-width: 58px; }
@media (min-width: 769px) { .image-detail-header { position: relative; } }
@media (max-width: 768px) { .image-dock-tools button { min-width: 0; } }
</style>

<style scoped>
.editor-load-error { position: relative; z-index: 2; padding: 12px; border-radius: 12px; background: var(--surface); }
</style>
