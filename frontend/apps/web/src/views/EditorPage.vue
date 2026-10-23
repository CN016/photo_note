<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { ElMessageBox, type UploadFile } from 'element-plus'
import { ArrowLeft, ArrowRight, Check, Close, Crop, FullScreen, Lock, Plus, Picture, RefreshLeft, RefreshRight, UploadFilled } from '@element-plus/icons-vue'
import AppHeader from '../components/AppHeader.vue'
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
  download: (key: string) => managementMode.value ? baseApi.managementImage(itemId.value, key, managementOrgID.value) : baseApi.download(key),
  updateInspiration: (id: string, data: Record<string, unknown>, imageMetadata?: import('../api/types').InspirationImageMetadata) => managementMode.value
    ? baseApi.managementSaveInspiration(id, { data, ...(imageMetadata ? { image_metadata: imageMetadata } : {}) }, managementOrgID.value)
    : baseApi.updateInspiration(id, data, imageMetadata),
  updateInspirationTags: (id: string, names: string[]) => managementMode.value ? baseApi.managementSaveInspiration(id, { tag_names: names }, managementOrgID.value) : baseApi.updateInspirationTags(id, names),
}
const isExisting = computed(() => itemId.value.length > 0)
const isEditing = ref(false)
const editSnapshot = ref('')
const documentData = ref<EditorDocument>(createEditorDocument())
const sourceCanvas = shallowRef<HTMLCanvasElement | null>(null)
const previewCanvas = ref<HTMLCanvasElement | null>(null)
const stageBox = ref<HTMLElement | null>(null)
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
const transformDialog = ref<TransformAction | null>(null)
const anchorDialog = ref(false)
const anchorDraft = ref<AnchorDraft | null>(null)
const movingAnchor = ref<AnchorMoveDraft | null>(null)
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
let tagSearchVersion = 0
let suppressAnchorClick = false

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
  return sections.map(section => ({
    ...section,
    rows: section.fields.flatMap(([key, label]) => {
      const value = metadata[key as keyof InspirationImageMetadata]
      if (value === undefined || value === null || value === '' || (Array.isArray(value) && value.length === 0)) return []
      return [{ key, label: t(label), value: formatMetadataValue(key, value) }]
    }),
  })).filter(section => section.rows.length > 0)
})
const imageMetadataExtraEntries = computed(() => Object.entries(record.value?.image_metadata?.extra ?? {})
  .sort(([left], [right]) => left.localeCompare(right))
  .map(([key, value]) => ({ key, value: formatMetadataValue(key, value) })))
const hasImageMetadata = computed(() => imageMetadataSections.value.length > 0 || imageMetadataExtraEntries.value.length > 0)

function formatMetadataValue(key: string, value: unknown): string {
  if (Array.isArray(value)) return value.map(item => formatMetadataValue(key, item)).join(', ')
  if (value && typeof value === 'object') return JSON.stringify(value)
  if (typeof value === 'boolean') return t(value ? 'editor.metadataYes' : 'editor.metadataNo')
  if (key === 'source_last_modified_at' && typeof value === 'string') {
    const date = new Date(value)
    if (Number.isFinite(date.getTime())) return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
  }
  return String(value)
}

const canUndo = computed(() => { historyRevision.value; return history?.canUndo ?? false })
const canRedo = computed(() => { historyRevision.value; return history?.canRedo ?? false })
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
  // 预留 24px 环绕留白，让贴近图像边缘的锚点仍能完整显示圆点与触控区域。
  const availableWidth = Math.max(1, fitBox.value.width - 48)
  const availableHeight = Math.max(1, fitBox.value.height - 48)
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

/** 退出编辑前确认丢弃未保存修改，并将轻量文档和撤销历史恢复至进入时快照。 */
async function cancelEditing(): Promise<void> {
  finishRgbChange()
  if (hasUnsavedChanges.value) {
    try {
      await ElMessageBox.confirm(t('editor.discardChangesText'), t('editor.discardChangesTitle'), {
        confirmButtonText: t('editor.discardChangesConfirm'),
        cancelButtonText: t('common.cancel'),
        type: 'warning',
      })
    } catch { return }
  }
  if (editSnapshot.value) documentData.value = JSON.parse(editSnapshot.value) as EditorDocument
  history = new DocumentHistory(documentData.value)
  historyRevision.value += 1
  editSnapshot.value = ''
  isEditing.value = false
  anchorMode.value = false
  cropMode.value = false
  cropSelection.value = null
  transformDialog.value = null
  anchorDialog.value = false
  anchorDraft.value = null
  error.value = ''
  status.value = ''
  scheduleRender()
}

/** 自动画像只合并到已保存数据；独立写入进行时，编辑保存会等待它完成再提交完整文档。 */
async function persistColorAnalysis(profile: ColorAnalysis, baseData: Record<string, unknown>): Promise<void> {
  if (!isExisting.value || !record.value) return
  if (colorWritePromise) { await colorWritePromise; return }

  colorAnalysisSaving.value = true
  colorAnalysisError.value = ''
  const pending = (async () => {
    try {
      const updated = await api.updateInspiration(itemId.value, { ...baseData, color_analysis: profile })
      record.value = updated
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
  loading.value = true
  error.value = ''
  colorAnalysis.value = null
  colorAnalysisError.value = ''
  try {
    const loaded = await api.inspiration(itemId.value)
    record.value = loaded
    tagNames.value = [...(loaded.tag_names ?? [])]
    tagOptions.value = [...tagNames.value]
    searchTagOptions('')
    imageMetadata.value = loaded.image_metadata ?? null
    filename.value = loaded.file_name || t('gallery.untitled')
    documentData.value = parseEditorDocument(loaded.data)
    history = new DocumentHistory(documentData.value)
    const workImage = await api.download(loaded.image_key)
    const decoded = await decodeImagePreview(workImage)
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
  } catch (cause) { error.value = localError(cause, t('editor.loadError')) }
  finally { loading.value = false }
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
async function saveTagNames(names: string[]): Promise<void> {
  const current = record.value
  if (!current || tagSaving.value) return
  const nextNames = [...new Set(names.map(name => name.trim()).filter(Boolean))]
  const previousNames = [...(current.tag_names ?? [])]
  if (nextNames.length === previousNames.length && nextNames.every((name, index) => name === previousNames[index])) {
    tagNames.value = previousNames
    return
  }

  tagNames.value = nextNames
  tagOptions.value = [...new Set([...nextNames, ...tagOptions.value])]
  tagSaveError.value = ''
  tagSaveStatus.value = ''
  tagSaving.value = true
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

/** 在预览画布上拖动锚点时即时更新叠层位置，抬起指针后只提交一条可撤销历史。 */
function beginAnchorMove(event: PointerEvent, anchor: ImageAnchor): void {
  if (!isEditing.value || anchorMode.value || cropMode.value || event.button !== 0) return
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
  if (JSON.stringify(anchors) !== JSON.stringify(documentData.value.anchors)) commit({ ...documentData.value, anchors })
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
}

/** 从锚点列表或备注对话框删除指定锚点，操作写入历史以支持撤销恢复。 */
function deleteAnchorById(id: string): void {
  commit({ ...documentData.value, anchors: documentData.value.anchors.filter(anchor => anchor.id !== id) })
  if (anchorDraft.value?.id === id) {
    anchorDialog.value = false
    anchorDraft.value = null
  }
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
  deleteAnchorById(id)
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
  finishRgbChange()
  saving.value = true
  try {
    if (isExisting.value) {
      if (colorWritePromise) await colorWritePromise
      busyPhase.value = t('editor.saving')
      const updated = await api.updateInspiration(itemId.value, editorDataForSave())
      record.value = updated
      documentData.value = parseEditorDocument(updated.data)
      history = new DocumentHistory(documentData.value)
      historyRevision.value += 1
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
  if (saving.value || loading.value) return false
  if (tagWritePromise) await tagWritePromise
  if (colorWritePromise) await colorWritePromise
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
    })
    return true
  } catch { return false }
})

onBeforeUnmount(() => {
  decodeVersion += 1
  tagSearchVersion += 1
  if (tagSearchTimer !== undefined) window.clearTimeout(tagSearchTimer)
  if (frameRequest) cancelAnimationFrame(frameRequest)
  resizeObserver?.disconnect()
  releaseCanvas(sourceCanvas.value)
  if (previewCanvas.value) { previewCanvas.value.width = 0; previewCanvas.value.height = 0 }
})
</script>

<template>
  <div class="workspace-page editor-page">
    <AppHeader />
    <main class="editor-main" :style="anchorDisplayStyle">
      <div class="editor-heading-row">
        <div class="editor-title-group">
          <RouterLink class="back-link" to="/inspirations"><el-icon aria-hidden="true"><ArrowLeft /></el-icon>{{ t('common.back') }}</RouterLink>
          <div><p class="eyebrow">{{ t('common.product') }}</p><h1>{{ isExisting ? (isEditing ? t('editor.editTitle') : t('editor.detailTitle')) : t('editor.newTitle') }}</h1></div>
        </div>
        <div class="editor-heading-actions">
          <p class="editor-subtitle">{{ t('editor.subtitle') }}</p>
          <el-button v-if="isExisting && !isEditing" class="primary-button detail-edit-button" type="primary" :disabled="loading || !imageReady" @click="beginEditing">{{ t('editor.edit') }}</el-button>
          <el-button v-else-if="isExisting && isEditing" class="cancel-edit-button heading-cancel-button" @click="cancelEditing">{{ t('editor.cancelEdit') }}</el-button>
        </div>
      </div>

      <section v-if="!isExisting && !imageReady" class="upload-welcome">
        <el-upload class="upload-dropzone" action="#" :auto-upload="false" :show-file-list="false" accept="image/*,.heic,.heif" :aria-label="t('editor.fileLabel')" :on-change="selectUploadedFile">
          <el-icon class="upload-icon" aria-hidden="true"><UploadFilled /></el-icon><strong>{{ t('editor.chooseImage') }}</strong><span>{{ t('editor.chooseHint') }}</span>
        </el-upload>
        <p v-if="loading" class="loading-inline" role="status"><span class="loading-dot"></span>{{ t('common.loading') }}</p>
      </section>

      <div v-else-if="imageReady" class="editor-workspace">
        <section class="editor-canvas-column" :aria-label="t('editor.previewLabel')">
          <div class="canvas-toolbar">
            <span class="filename-chip"><el-icon aria-hidden="true"><Picture /></el-icon>{{ filename }}</span>
            <div v-if="isEditing" class="history-actions">
              <el-button class="icon-button" :disabled="!canUndo" :aria-label="t('editor.undo')" :title="t('editor.undo')" @click="undo"><el-icon aria-hidden="true"><RefreshLeft /></el-icon></el-button>
              <el-button class="icon-button" :disabled="!canRedo" :aria-label="t('editor.redo')" :title="t('editor.redo')" @click="redo"><el-icon aria-hidden="true"><RefreshRight /></el-icon></el-button>
            </div>
          </div>
          <div ref="stageBox" class="canvas-stage" :class="{ 'anchor-active': anchorMode, 'crop-active': cropMode }" @click="onStageClick">
            <div v-if="imageReady" class="editor-canvas-wrap" :style="{ width: `${displayDimensions.width}px`, height: `${displayDimensions.height}px` }">
              <canvas ref="previewCanvas" class="editor-canvas" role="img" :tabindex="isEditing ? 0 : -1" :aria-label="anchorMode ? t('editor.anchorModeOn') : t('editor.previewLabel')" @keydown.enter.prevent="addAnchorAtCenter" @keydown.space.prevent="addAnchorAtCenter" @pointerdown="beginCropSelection" @pointermove="moveCropSelection" @pointerup="endCropSelection" @pointercancel="endCropSelection"></canvas>
              <div v-if="cropMode && cropSelection" class="crop-selection" :style="cropDragStyle" aria-hidden="true"></div>
              <component
                :is="isEditing ? 'button' : 'span'"
                v-for="anchor in visibleAnchors"
                :key="anchor.id"
                :class="['image-anchor', anchor.side, { 'is-moving': movingAnchor?.anchorId === anchor.id }]"
                :type="isEditing ? 'button' : undefined"
                :tabindex="isEditing ? 0 : undefined"
                :role="isEditing ? undefined : 'img'"
                :style="{ left: anchor.left, top: anchor.top, '--anchor-label-max-width': anchor.labelMaxWidth }"
                :aria-label="t('editor.anchorLabel', { number: anchor.index + 1, note: anchor.note })"
                :aria-description="isEditing ? t('editor.anchorMoveHelp') : undefined"
                @pointerdown.stop="isEditing && beginAnchorMove($event, anchor)"
                @pointermove.stop="isEditing && updateAnchorMove($event)"
                @pointerup.stop="isEditing && finishAnchorMove($event)"
                @pointercancel.stop="isEditing && cancelAnchorMove($event)"
                @keydown.left.prevent="isEditing && moveAnchorByKeyboard(anchor, $event)"
                @keydown.right.prevent="isEditing && moveAnchorByKeyboard(anchor, $event)"
                @keydown.up.prevent="isEditing && moveAnchorByKeyboard(anchor, $event)"
                @keydown.down.prevent="isEditing && moveAnchorByKeyboard(anchor, $event)"
                @click.stop="isEditing && handleAnchorClick(anchor)"
              >
                <span class="image-anchor-label">{{ anchor.note }}</span>
                <span class="image-anchor-dot" aria-hidden="true"><span></span></span>
              </component>
            </div>
            <div v-else class="loading-inline" role="status"><span class="loading-dot"></span>{{ t('common.loading') }}</div>
          </div>
          <div v-if="cropMode" class="crop-action-row"><span>{{ t('editor.cropHint') }}</span><el-button class="compact-button" @click="cancelCrop">{{ t('common.cancel') }}</el-button><el-button class="primary-button compact-button" type="primary" :disabled="!cropSelection" @click="applyCrop">{{ t('editor.cropApply') }}</el-button></div>
          <p v-else-if="anchorMode" class="stage-hint">{{ t('editor.anchorModeOn') }}</p>
          <p v-else-if="isEditing && documentData.anchors.length" class="stage-hint">{{ t('editor.anchorMoveHelp') }}</p>
          <p v-if="downsampled" class="stage-note">{{ t('editor.largeImage') }}</p>
        </section>

        <aside class="editor-controls" :class="{ 'detail-panel': !isEditing }" :aria-label="isEditing ? t('editor.editTitle') : t('editor.detailTitle')">
          <section class="control-section color-profile-section" :aria-label="t('editor.colorProfile')">
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

          <section v-if="isExisting" class="control-section inspiration-tags-section" :aria-label="t('editor.tags')">
            <div class="section-title-row">
              <h2>{{ t('editor.tags') }}</h2>
              <span v-if="tagSaving" class="profile-saving" role="status">{{ t('editor.tagSaving') }}</span>
            </div>
            <el-select
              v-model="tagNames"
              class="inspiration-tag-select"
              multiple
              filterable
              remote
              allow-create
              clearable
              default-first-option
              collapse-tags
              collapse-tags-tooltip
              :max-collapse-tags="2"
              :remote-method="searchTagOptions"
              :loading="tagOptionsLoading"
              :disabled="tagSaving || loading"
              :placeholder="t('editor.tagSearchPlaceholder')"
              @change="saveTagNames"
            >
              <el-option v-for="name in tagOptions" :key="name" :label="name" :value="name" />
            </el-select>
            <p v-if="tagSearchError" class="profile-error" role="alert">{{ tagSearchError }}</p>
            <p v-else-if="tagSaveError" class="profile-error" role="alert">{{ tagSaveError }}</p>
            <p v-else-if="tagSaveStatus" class="tag-save-status" role="status">{{ tagSaveStatus }}</p>
          </section>

          <section v-if="!isEditing && hasImageMetadata" class="control-section image-metadata-section" :aria-label="t('editor.metadataPanelTitle')">
            <div class="section-title-row"><h2>{{ t('editor.metadataPanelTitle') }}</h2></div>
            <div v-for="group in imageMetadataSections" :key="group.title" class="metadata-group">
              <h3>{{ group.title }}</h3>
              <dl class="image-metadata-list">
                <div v-for="row in group.rows" :key="row.key"><dt>{{ row.label }}</dt><dd>{{ row.value }}</dd></div>
              </dl>
            </div>
            <details v-if="imageMetadataExtraEntries.length" class="metadata-extra">
              <summary>{{ t('editor.metadataExtra') }} ({{ imageMetadataExtraEntries.length }})</summary>
              <dl class="image-metadata-list">
                <div v-for="entry in imageMetadataExtraEntries" :key="entry.key"><dt>{{ entry.key }}</dt><dd>{{ entry.value }}</dd></div>
              </dl>
            </details>
          </section>

          <section class="control-section anchor-size-section">
            <div class="section-title-row"><h2>{{ t('editor.anchorDisplaySize') }}</h2><output class="anchor-size-value">{{ anchorDisplayStore.size }}%</output></div>
            <el-slider :model-value="anchorDisplayStore.size" :min="70" :max="150" :step="5" :aria-label="t('editor.anchorDisplaySize')" @input="anchorDisplayStore.setSize(Number($event))" />
            <p class="anchor-size-help">{{ t('editor.anchorDisplaySizeHelp') }}</p>
          </section>

          <template v-if="isEditing">
            <section class="control-section transform-section">
              <div class="section-title-row"><h2>{{ t('editor.editTitle') }}</h2><span class="section-step">01</span></div>
              <div class="transform-buttons">
                <el-button class="tool-button" :disabled="cropMode" @click="prepareTransform('rotate')"><el-icon aria-hidden="true"><RefreshRight /></el-icon>{{ t('editor.rotate') }}</el-button>
                <el-button class="tool-button" :disabled="cropMode" @click="prepareTransform('crop')"><el-icon aria-hidden="true"><Crop /></el-icon>{{ t('editor.crop') }}</el-button>
                <el-button class="tool-button" :disabled="cropMode" @click="prepareTransform('reset')"><el-icon aria-hidden="true"><FullScreen /></el-icon>{{ t('editor.resetCrop') }}</el-button>
              </div>
            </section>

            <section class="control-section color-section">
              <div class="section-title-row"><h2>{{ t('editor.rgb') }}</h2><span class="section-step">02</span></div>
              <label class="color-slider red-slider"><span>{{ t('editor.red') }}</span><el-slider :model-value="documentData.rgb.red" :min="-100" :max="100" :aria-label="t('editor.red')" @input="updateRgb('red', String($event))" @change="finishRgbChange" /><output>{{ documentData.rgb.red }}</output></label>
              <label class="color-slider green-slider"><span>{{ t('editor.green') }}</span><el-slider :model-value="documentData.rgb.green" :min="-100" :max="100" :aria-label="t('editor.green')" @input="updateRgb('green', String($event))" @change="finishRgbChange" /><output>{{ documentData.rgb.green }}</output></label>
              <label class="color-slider blue-slider"><span>{{ t('editor.blue') }}</span><el-slider :model-value="documentData.rgb.blue" :min="-100" :max="100" :aria-label="t('editor.blue')" @input="updateRgb('blue', String($event))" @change="finishRgbChange" /><output>{{ documentData.rgb.blue }}</output></label>
            </section>

            <section class="control-section notes-section">
              <div class="section-title-row"><div><h2>{{ t('editor.anchors') }}</h2><span class="notes-count">{{ documentData.anchors.length }}</span></div><span class="section-step">03</span></div>
              <el-button class="note-mode-button" :class="{ active: anchorMode }" :disabled="cropMode" @click="anchorMode = !anchorMode"><el-icon aria-hidden="true"><Plus /></el-icon>{{ anchorMode ? t('editor.anchorModeOn') : t('editor.anchorMode') }}</el-button>
              <p v-if="!documentData.anchors.length" class="notes-empty">{{ t('editor.anchorEmpty') }}</p>
              <div v-else class="note-list">
                <div v-for="(anchor, index) in documentData.anchors" :key="anchor.id" class="note-list-item-row">
                  <el-button class="note-list-item" @click="editAnchor(anchor)"><span class="note-index">{{ index + 1 }}</span><span>{{ anchor.note }}</span><el-icon class="note-arrow" aria-hidden="true"><ArrowRight /></el-icon></el-button>
                  <el-button class="note-delete-button" text :aria-label="t('editor.anchorDeleteLabel', { number: index + 1 })" :title="t('editor.anchorDeleteLabel', { number: index + 1 })" @click="deleteAnchorById(anchor.id)"><el-icon aria-hidden="true"><Close /></el-icon></el-button>
                </div>
              </div>
            </section>
            <p class="source-preserved"><el-icon aria-hidden="true"><Lock /></el-icon>{{ t('editor.sourcePreserved') }}</p>
            <p v-if="error" class="form-error editor-error" role="alert">{{ error }}</p>
            <p v-if="status" class="success-message" role="status">{{ status }}</p>
            <el-button class="primary-button save-button desktop-save" type="primary" :disabled="saving || loading || cropMode || anchorDialog || !!transformDialog" :loading="saving" @click="save">{{ saving ? t('editor.saving') : !isExisting && uploadSourceKey ? t('editor.retry') : t('editor.saveEdit') }}<el-icon aria-hidden="true"><Check /></el-icon></el-button>
            <p v-if="busyPhase" class="save-progress" role="status"><span class="loading-dot"></span>{{ busyPhase }}</p>
          </template>

          <section v-else-if="documentData.anchors.length" class="control-section detail-notes-section">
            <div class="section-title-row"><div><h2>{{ t('editor.anchors') }}</h2><span class="notes-count">{{ documentData.anchors.length }}</span></div></div>
            <ul class="detail-note-list"><li v-for="(anchor, index) in documentData.anchors" :key="anchor.id"><span class="note-index">{{ index + 1 }}</span><span>{{ anchor.note }}</span></li></ul>
          </section>
          <p v-if="status && !isEditing" class="success-message" role="status">{{ status }}</p>
        </aside>
      </div>

      <div v-if="error && !imageReady" class="editor-load-error" role="alert"><p>{{ error }}</p><RouterLink class="secondary-button" to="/inspirations">{{ t('common.back') }}</RouterLink></div>
      <div v-if="imageReady && isEditing" class="mobile-save-bar"><span v-if="error" class="mobile-save-error" role="alert">{{ error }}</span><span v-else-if="busyPhase" class="mobile-save-progress" role="status">{{ busyPhase }}</span><el-button v-if="isExisting" class="cancel-edit-button mobile-cancel-button" :disabled="saving" @click="cancelEditing">{{ t('editor.cancelEdit') }}</el-button><el-button class="primary-button save-button" type="primary" :disabled="saving || loading || cropMode || anchorDialog || !!transformDialog" :loading="saving" @click="save">{{ saving ? t('editor.saving') : !isExisting && uploadSourceKey ? t('editor.retry') : isExisting ? t('editor.saveEdit') : t('editor.saveNew') }}<el-icon aria-hidden="true"><Check /></el-icon></el-button></div>
    </main>

    <el-dialog :model-value="!!transformDialog" :title="t('editor.transformTitle')" width="min(500px, calc(100vw - 32px))" @close="transformDialog = null">
      <p>{{ t('editor.transformText') }}</p>
      <template #footer>
        <div class="dialog-actions">
          <el-button text @click="transformDialog = null">{{ t('common.cancel') }}</el-button>
          <el-button @click="performTransform(transformDialog!, false)">{{ t('editor.keepAnchors') }}</el-button>
          <el-button type="primary" @click="performTransform(transformDialog!, true)">{{ t('editor.clearAnchors') }}</el-button>
        </div>
      </template>
    </el-dialog>

    <el-dialog :model-value="anchorDialog && !!anchorDraft" :title="anchorDraft?.id ? t('editor.anchorEdit') : t('editor.anchorMode')" width="min(500px, calc(100vw - 32px))" @close="anchorDialog = false">
      <el-input v-if="anchorDraft" v-model="anchorDraft.note" type="textarea" :rows="4" maxlength="500" :placeholder="t('editor.anchorPlaceholder')" />
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <template #footer>
        <div class="dialog-actions">
          <el-button v-if="anchorDraft?.id" type="danger" text @click="deleteAnchor">{{ t('editor.anchorDelete') }}</el-button>
          <span class="dialog-spacer"></span>
          <el-button text @click="anchorDialog = false">{{ t('common.cancel') }}</el-button>
          <el-button type="primary" @click="saveAnchor">{{ t('editor.anchorSave') }}</el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>
