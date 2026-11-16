<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { InspirationRecord } from '../api/types'
import { anchorToView, parseEditorDocument } from '../../../../packages/editor-core/src'
import { decodeImagePreview, releaseCanvas, renderDocumentPreview } from '../../../../packages/image-processing/src'
import { loadThumbnail } from '../images/thumbnails'
import { loadDetailImage } from '../images/detailImages'


type ThumbnailRecord = Pick<InspirationRecord, 'file_name' | 'image_key'> & { data?: Record<string, unknown>; file_hash?: string | null }
const props = defineProps<{ record: ThumbnailRecord; loadImage?: (key: string) => Promise<Blob>; showAnchors?: boolean }>()
const { t } = useI18n()
const element = ref<HTMLElement | null>(null)
const imageUrl = ref<string | null>(null)
const failed = ref(false)
const loading = ref(false)
const imageSize = ref({ width: 0, height: 0 })
const frameSize = ref({ width: 0, height: 0 })
let resizeObserver: ResizeObserver | null = null
const documentData = computed(() => parseEditorDocument(props.record.data ?? {}))
/** 归一化编辑坐标先映射旋转裁切，再补偿缩略图object-fit:cover的居中裁切；不可见点不显示。 */
const anchors = computed(() => {
  if (!props.showAnchors || !imageUrl.value || !imageSize.value.width || !frameSize.value.width) return []
  const { width, height } = frameSize.value
  const scale = Math.max(width / imageSize.value.width, height / imageSize.value.height)
  const renderedWidth = imageSize.value.width * scale, renderedHeight = imageSize.value.height * scale
  return documentData.value.anchors.flatMap((anchor, index) => {
    const point = anchorToView(anchor, documentData.value)
    const x = point.x * renderedWidth - (renderedWidth - width) / 2
    const y = point.y * renderedHeight - (renderedHeight - height) / 2
    if (!point.visible || x < 0 || y < 0 || x > width || y > height) return []
    return [{ ...anchor, number: index + 1, x, y, right: x < width / 2, maxWidth: Math.max(24, (x < width / 2 ? width - x : x) - 22) }]
  })
})
/** 读取图片实际解码尺寸，响应式布局变化时仍保持锚点与像素位置一致。 */
function imageLoaded(event: Event) {
  const image = event.target as HTMLImageElement
  imageSize.value = { width: image.naturalWidth, height: image.naturalHeight }
}
let requestVersion = 0

const imageName = computed(() => props.record.file_name || t('gallery.untitled'))
const imageLabel = computed(() => t('gallery.openImage', { name: imageName.value }))
const imageKey = computed(() => props.record.image_key)

/** 列表条目创建即加载缩略图并预取详情工作图，后台错误不显示额外 loading。 */
async function loadPreview() {
  const version = ++requestVersion
  loading.value = true
  failed.value = false
  try {
    if (!props.loadImage) void loadDetailImage(imageKey.value, props.record.file_hash || '').catch(() => {})
    let blob = await (props.loadImage ? props.loadImage(imageKey.value) : loadThumbnail(imageKey.value, props.record.file_hash || ''))
    // 只在图库启用：对小尺寸缩略图应用与详情一致的编辑变换，避免锚点落在未编辑的图上。
    if (props.showAnchors) {
      let source: HTMLCanvasElement | null = null, output: HTMLCanvasElement | null = null
      try {
        source = (await decodeImagePreview(blob)).canvas
        output = renderDocumentPreview(source, documentData.value)
        blob = await new Promise<Blob>((resolve, reject) => output!.toBlob(value => value ? resolve(value) : reject(new Error('thumbnail_encode_failed')), 'image/png'))
      } finally { releaseCanvas(source); releaseCanvas(output) }
    }
    // 管理工作台自带专用授权loader，保留原来的兼容解码；普通图库直接使用服务器JPEG。
    const url = props.loadImage ? await (await import('../../../../packages/image-processing/src')).previewObjectUrl(blob) : URL.createObjectURL(blob)
    if (version !== requestVersion) { URL.revokeObjectURL(url); return }
    imageUrl.value = url
  } catch { if (version === requestVersion) failed.value = true }
  finally { if (version === requestVersion) loading.value = false }
}

watch(() => `${imageKey.value}:${props.record.file_hash || ''}:${props.showAnchors ? JSON.stringify(documentData.value) : ''}`, () => {
  if (imageUrl.value) URL.revokeObjectURL(imageUrl.value)
  imageUrl.value = null
  if (element.value) void loadPreview()
})

onMounted(() => {
  if (element.value) {
    const measure = () => { frameSize.value = { width: element.value!.clientWidth, height: element.value!.clientHeight } }
    measure()
    resizeObserver = new ResizeObserver(measure); resizeObserver.observe(element.value)
  }
  void loadPreview()
})

onBeforeUnmount(() => {
  requestVersion += 1
  resizeObserver?.disconnect()
  if (imageUrl.value) URL.revokeObjectURL(imageUrl.value)
})
</script>

<template>
  <div ref="element" class="thumbnail-frame" :aria-label="imageLabel">
    <img v-if="imageUrl" :src="imageUrl" :alt="imageName" loading="eager" @load="imageLoaded" />
    <span v-else-if="failed" class="thumbnail-state">{{ t('gallery.imageUnavailable') }}</span>
    <span v-else class="thumbnail-state" aria-hidden="true"></span>
    <span v-for="anchor in anchors" :key="anchor.id" class="thumbnail-anchor" :class="{ right: anchor.right }" :style="{ left: `${anchor.x}px`, top: `${anchor.y}px`, '--note-width': `${anchor.maxWidth}px` }" :title="anchor.note"><span class="thumbnail-anchor-point" aria-hidden="true"></span><span class="thumbnail-anchor-note">{{ anchor.note }}</span></span>
  </div>
</template>

<style scoped>
.thumbnail-anchor { position: absolute; display: flex; flex-direction: row-reverse; align-items: center; gap: 5px; transform: translate(calc(-100% + 8px), -50%); pointer-events: none; }
.thumbnail-anchor.right { flex-direction: row; transform: translate(-8px, -50%); }
.thumbnail-anchor-point { width: 16px; height: 16px; flex: 0 0 16px; border: 2px solid #cdd0ca; border-radius: 50%; background: #f3df00; box-shadow: inset 0 0 0 3px rgb(35 37 38 / 82%); }
.thumbnail-anchor-note { max-width: var(--note-width); padding: 4px 7px; border: 1px solid rgb(194 197 193 / 84%); border-radius: 12px; background: rgb(38 40 41 / 65%); color: white; font-size: 11px; line-height: 1.35; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
