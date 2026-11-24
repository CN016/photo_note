<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { InspirationRecord } from '../api/types'
import { anchorToView, clampAnchorLabelScale, parseEditorDocument, type EditorDocument } from '../../../../packages/editor-core/src'
import { decodeImagePreview, releaseCanvas, renderDocumentPreview } from '../../../../packages/image-processing/src'
import { loadThumbnail } from '../images/thumbnails'
import ImageAnchorMarker from './ImageAnchorMarker.vue'
import { anchorNoteTitle, thumbnailAnchorTextScale } from '../views/editorAnchorLayout'
import {
  inspirationDetailKey,
  inspirationDetails,
  preloadInspirationDetail,
  type InspirationDetailSeed,
} from '../images/detailImages'

type ThumbnailRecord = Pick<InspirationRecord, 'file_name' | 'image_key'> & {
  business_id?: string
  updated_at?: string
  data?: Record<string, unknown>
  file_hash?: string | null
}
const props = defineProps<{
  record: ThumbnailRecord
  loadImage?: (key: string) => Promise<Blob>
  showAnchors?: boolean
  detailTransition?: boolean
}>()
const { t } = useI18n()
const element = ref<HTMLElement | null>(null)
const detailImage = ref<HTMLImageElement | null>(null)
const previewUrl = ref<string | null>(null)
const detailUrl = ref<string | null>(null)
const detailReady = ref(false)
const failed = ref(false)
const loading = ref(false)
const imageSize = ref({ width: 0, height: 0 })
const frameSize = ref({ width: 0, height: 0 })
const resolvedDocument = ref<EditorDocument | null>(null)
let resizeObserver: ResizeObserver | null = null
let detailObserver: IntersectionObserver | null = null
let previewVersion = 0
let detailVersion = 0
let detailRequested = false

const listDocumentData = computed(() => parseEditorDocument(props.record.data ?? {}))
const documentData = computed(() => resolvedDocument.value ?? listDocumentData.value)
const canResolveDetail = computed(() => Boolean(props.detailTransition && props.showAnchors && props.record.business_id && props.record.updated_at))

/** 归一化编辑坐标先映射旋转裁切，再补偿缩略图 object-fit:cover 的居中裁切。 */
const anchors = computed(() => {
  if (!props.showAnchors || !imageSize.value.width || !frameSize.value.width) return []
  const { width, height } = frameSize.value
  const scale = Math.max(width / imageSize.value.width, height / imageSize.value.height)
  const renderedWidth = imageSize.value.width * scale
  const renderedHeight = imageSize.value.height * scale
  return documentData.value.anchors.flatMap((anchor, index) => {
    const point = anchorToView(anchor, documentData.value)
    const x = point.x * renderedWidth - (renderedWidth - width) / 2
    const y = point.y * renderedHeight - (renderedHeight - height) / 2
    if (!point.visible || x < 0 || y < 0 || x > width || y > height) return []
    const title = anchorNoteTitle(anchor.note)
    const imageTextScale = thumbnailAnchorTextScale(renderedWidth)
    const labelScale = clampAnchorLabelScale(anchor.label_scale ?? 1)
    const visualScale = imageTextScale * labelScale
    // 列表标题只保留第一行，并跟随图片实际渲染宽度缩放；落点坐标始终独立于文字尺寸。
    return [{
      id: anchor.id,
      title,
      x,
      y,
      style: {
        left: `${x}px`,
        top: `${y}px`,
        '--anchor-title-font-size': `${14 * imageTextScale}px`,
        '--anchor-label-scale': `${labelScale}`,
        '--anchor-marker-dot-size': `${14 * visualScale * .56}px`,
        '--anchor-marker-rule-width': `${14 * visualScale * 3}px`,
        '--anchor-marker-gap': `${14 * visualScale * .36}px`,
        '--thumbnail-anchor-note-width': `${Math.max(36 * visualScale, Math.min(width - x - 8, 160 * visualScale))}px`,
      },
    }]
  })
})
const imageName = computed(() => props.record.file_name || t('gallery.untitled'))
const imageLabel = computed(() => t('gallery.openImage', { name: imageName.value }))
const imageKey = computed(() => props.record.image_key)

function imageLoaded(event: Event): void {
  const image = event.target as HTMLImageElement
  imageSize.value = { width: image.naturalWidth, height: image.naturalHeight }
}

function detailLoaded(): void {
  detailReady.value = true
}

/** 高清图完成解码后同步刷新尺寸与透明度，锚点立即使用最终像素比例。 */
function detailImageLoaded(event: Event): void {
  imageLoaded(event)
  detailLoaded()
}

/** 先绘制低清预览；高清图加载完成后仅用透明度交接像素，避免布局跳动。 */
async function loadPreview(): Promise<void> {
  const version = ++previewVersion
  loading.value = true
  failed.value = false
  try {
    let blob = await (props.loadImage ? props.loadImage(imageKey.value) : loadThumbnail(imageKey.value, props.record.file_hash || ''))
    if (props.showAnchors) {
      let source: HTMLCanvasElement | null = null, output: HTMLCanvasElement | null = null
      try {
        source = (await decodeImagePreview(blob)).canvas
        output = renderDocumentPreview(source, listDocumentData.value)
        blob = await new Promise<Blob>((resolve, reject) => output!.toBlob(value => value ? resolve(value) : reject(new Error('thumbnail_encode_failed')), 'image/png'))
      } finally { releaseCanvas(source); releaseCanvas(output) }
    }
    const url = props.loadImage
      ? await (await import('../../../../packages/image-processing/src')).previewObjectUrl(blob)
      : URL.createObjectURL(blob)
    if (version !== previewVersion) { URL.revokeObjectURL(url); return }
    if (previewUrl.value) URL.revokeObjectURL(previewUrl.value)
    previewUrl.value = url
  } catch {
    if (version === previewVersion) failed.value = true
  } finally {
    if (version === previewVersion) loading.value = false
  }
}

/** 高清详情图沿用现有队列，命中本地缓存后无需联网；版本变化时才重新下载。 */
async function loadDetailPreview(): Promise<void> {
  if (!canResolveDetail.value || detailRequested || !props.record.business_id || !props.record.updated_at) return
  detailRequested = true
  const version = ++detailVersion
  const record: InspirationDetailSeed = {
    business_id: props.record.business_id!,
    updated_at: props.record.updated_at!,
    image_key: props.record.image_key,
    file_hash: props.record.file_hash || null,
    data: props.record.data ?? {},
  }
  try {
    const pending = inspirationDetails.peek(inspirationDetailKey(record)) ?? preloadInspirationDetail(record)
    const prepared = await pending
    if (version !== detailVersion) return
    resolvedDocument.value = parseEditorDocument(prepared.record.data)
    detailReady.value = false
    detailUrl.value = prepared.image.src
    await nextTick()
    if (detailImage.value?.complete) detailLoaded()
  } catch {
    // 高清图失败时继续使用已显示的预览图；点击详情仍会沿统一错误流程重试。
  }
}

/** 只在接近视口时准备高清图，避免初次列表为所有屏幕外图片同时占用解码资源。 */
function observeDetail(): void {
  detailObserver?.disconnect()
  if (!canResolveDetail.value || !element.value) return
  if (typeof IntersectionObserver === 'undefined') { void loadDetailPreview(); return }
  detailObserver = new IntersectionObserver(entries => {
    if (!entries.some(entry => entry.isIntersecting)) return
    detailObserver?.disconnect()
    void loadDetailPreview()
  }, { rootMargin: '320px 0px' })
  detailObserver.observe(element.value)
}

function resetImages(): void {
  if (previewUrl.value) URL.revokeObjectURL(previewUrl.value)
  previewUrl.value = null
  detailUrl.value = null
  detailReady.value = false
  resolvedDocument.value = null
  detailRequested = false
  detailVersion += 1
}

watch(() => `${imageKey.value}:${props.record.file_hash || ''}:${props.record.updated_at || ''}:${props.detailTransition ? JSON.stringify(listDocumentData.value) : ''}`, () => {
  resetImages()
  if (element.value) void loadPreview().finally(observeDetail)
})

onMounted(() => {
  if (element.value) {
    const measure = () => { frameSize.value = { width: element.value!.clientWidth, height: element.value!.clientHeight } }
    measure()
    resizeObserver = new ResizeObserver(measure)
    resizeObserver.observe(element.value)
  }
  void loadPreview().finally(observeDetail)
})

onBeforeUnmount(() => {
  previewVersion += 1
  detailVersion += 1
  resizeObserver?.disconnect()
  detailObserver?.disconnect()
  if (previewUrl.value) URL.revokeObjectURL(previewUrl.value)
})
</script>

<template>
  <div ref="element" class="thumbnail-frame" :class="{ 'has-detail': detailReady }" :aria-label="imageLabel">
    <img v-if="previewUrl" class="thumbnail-image thumbnail-preview" :src="previewUrl" :alt="imageName" loading="eager" @load="imageLoaded" />
    <img v-if="detailUrl" ref="detailImage" class="thumbnail-image thumbnail-detail" :class="{ ready: detailReady }" :src="detailUrl" :alt="imageName" loading="eager" @load="detailImageLoaded" />
    <span v-if="failed && !previewUrl && !detailUrl" class="thumbnail-state">{{ t('gallery.imageUnavailable') }}</span>
    <span v-else-if="loading && !previewUrl" class="thumbnail-state" aria-hidden="true"></span>
    <span
      v-for="anchor in anchors"
      :key="anchor.id"
      class="thumbnail-anchor"
      :style="anchor.style"
      :title="anchor.title"
    >
      <ImageAnchorMarker />
      <span class="thumbnail-anchor-title">{{ anchor.title }}</span>
    </span>
  </div>
</template>

<style scoped>
.thumbnail-image { position: absolute; z-index: 1; inset: 0; width: 100%; height: 100%; object-fit: cover; }
.thumbnail-preview { opacity: 1; transition: opacity 420ms ease; }
.thumbnail-detail { opacity: 0; transform: scale(1.008); transition: opacity 420ms ease, transform 520ms ease; }
.thumbnail-detail.ready { opacity: 1; transform: scale(1); }
.thumbnail-frame.has-detail .thumbnail-preview { opacity: 0; }
.thumbnail-state { position: relative; z-index: 2; }
.thumbnail-anchor { position: absolute; z-index: 3; width: 0; height: 0; pointer-events: none; }
.thumbnail-anchor-title {
  position: absolute;
  bottom: calc(var(--anchor-marker-dot-size, 8px) / 2 + var(--anchor-marker-gap, 5px));
  left: 0;
  width: max-content;
  max-width: var(--thumbnail-anchor-note-width, 120px);
  color: #fff;
  font-size: calc(var(--anchor-title-font-size, 14px) * var(--anchor-label-scale, 1));
  font-weight: 600;
  line-height: 1.2;
  overflow-wrap: anywhere;
  text-shadow: 0 1px 3px rgb(0 0 0 / 68%);
}
@media (prefers-reduced-motion: reduce) {
  .thumbnail-preview, .thumbnail-detail { transition: none; }
}
</style>
