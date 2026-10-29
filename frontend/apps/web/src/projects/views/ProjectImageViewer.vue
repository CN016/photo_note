<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Editor } from '@tiptap/core'
import { ArrowLeft, ArrowRight, Close } from '@element-plus/icons-vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { anchorToView, parseEditorDocument } from '../../../../../packages/editor-core/src'
import { decodeImagePreview, releaseCanvas, renderDocumentPreview } from '../../../../../packages/image-processing/src'
import { api } from '../../api/client'
const props = defineProps<{ editor: Editor; position: number }>()
const emit = defineEmits<{ close: [] }>()
const route = useRoute(); const { t } = useI18n({ useScope: 'global' })
const entries: { position: number; type: 'inspiration' | 'attachment'; key: string; name: string }[] = []
// 保留正文顺序和重复引用，只有图片参与浏览。
props.editor.state.doc.descendants((node, position) => {
  if (node.type.name === 'inspirationImage' && typeof node.attrs.src === 'string') entries.push({ position, type: 'inspiration', key: node.attrs.src.replace(/^inspiration:\/\//, ''), name: '' })
  else if (node.type.name === 'projectAttachment' && String(node.attrs.contentType).startsWith('image/')) entries.push({ position, type: 'attachment', key: String(node.attrs.storekey), name: String(node.attrs.fileName || '') })
})
const index = ref(Math.max(0, entries.findIndex(item => item.position === props.position)))
const current = computed(() => entries[index.value]); const imageURL = ref(''); const imageName = ref('')
const loading = ref(false); const failed = ref(false)
const anchors = ref<{ id: string; note: string; x: number; y: number }[]>([])
let requestVersion = 0; let touchStartX = 0; let touchStartY = 0
/** 使用灵感图同一工作图、变换和锚点坐标算法，并释放每次预览产生的资源。 */
async function loadImage() {
  const version = ++requestVersion
  if (imageURL.value) URL.revokeObjectURL(imageURL.value)
  imageURL.value = ''; anchors.value = []; failed.value = false; loading.value = true
  const entry = current.value
  if (!entry) { loading.value = false; failed.value = true; return }
  let source: HTMLCanvasElement | null = null; let output: HTMLCanvasElement | null = null
  try {
    let blob: Blob; let name = entry.name
    if (entry.type === 'inspiration') {
      const record = await api.inspiration(entry.key); blob = await api.download(record.image_key)
      const doc = parseEditorDocument(record.data); source = (await decodeImagePreview(blob)).canvas
      output = renderDocumentPreview(source, doc)
      blob = await new Promise<Blob>((resolve, reject) => output!.toBlob(value => value ? resolve(value) : reject(new Error('encode_failed')), 'image/png'))
      if (version !== requestVersion) return
      anchors.value = doc.anchors.flatMap(anchor => { const point = anchorToView(anchor, doc); return point.visible ? [{ ...anchor, x: point.x, y: point.y }] : [] })
      name = record.file_name
    } else blob = await api.projectAttachmentContent(String(route.params.id || ''), entry.key)
    if (version === requestVersion) { imageName.value = name; imageURL.value = URL.createObjectURL(blob) }
  } catch { if (version === requestVersion) failed.value = true }
  finally { releaseCanvas(source); releaseCanvas(output); if (version === requestVersion) loading.value = false }
}
function move(delta: number) { index.value = Math.max(0, Math.min(entries.length - 1, index.value + delta)) }
function onKey(event: KeyboardEvent) {
  if (event.key === 'ArrowLeft') { event.preventDefault(); move(-1) }
  else if (event.key === 'ArrowRight') { event.preventDefault(); move(1) }
  else if (event.key === 'Escape') { event.preventDefault(); emit('close') }
}
function onTouchStart(event: TouchEvent) { touchStartX = event.touches[0]?.clientX ?? 0; touchStartY = event.touches[0]?.clientY ?? 0 }
function onTouchEnd(event: TouchEvent) {
  const touch = event.changedTouches[0]; if (!touch) return
  const dx = touch.clientX - touchStartX; const dy = touch.clientY - touchStartY
  if (Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy) * 1.3) move(dx < 0 ? 1 : -1)
}
watch(index, () => { void loadImage() })
onMounted(() => { window.addEventListener('keydown', onKey); void loadImage() })
onBeforeUnmount(() => { requestVersion += 1; window.removeEventListener('keydown', onKey); if (imageURL.value) URL.revokeObjectURL(imageURL.value) })
</script>
<template>
  <el-dialog :model-value="true" fullscreen :show-close="false" :close-on-click-modal="false" @close="emit('close')">
    <template #header><div class="viewer-header"><span>{{ imageName }} · {{ index + 1 }} / {{ entries.length }}</span><el-button :icon="Close" :aria-label="t('projects.closePreview')" @click="emit('close')" /></div></template>
    <div class="viewer-body" @touchstart.passive="onTouchStart" @touchend.passive="onTouchEnd">
      <el-button class="viewer-arrow" :icon="ArrowLeft" :disabled="index === 0" :aria-label="t('projects.previousImage')" @click="move(-1)" />
      <div class="viewer-stage"><span v-if="loading" role="status">{{ t('common.loading') }}</span><span v-else-if="failed" role="alert">{{ t('projects.imageLoadFailed') }}</span><div v-else-if="imageURL" class="viewer-image"><img :src="imageURL" :alt="imageName" /><div v-for="anchor in anchors" :key="anchor.id" class="viewer-anchor" :class="{ 'label-left': anchor.x > 0.65, 'label-up': anchor.y > 0.85 }" :style="{ left: `${anchor.x * 100}%`, top: `${anchor.y * 100}%` }"><span>{{ anchor.note }}</span></div></div></div>
      <el-button class="viewer-arrow" :icon="ArrowRight" :disabled="index >= entries.length - 1" :aria-label="t('projects.nextImage')" @click="move(1)" />
    </div>
  </el-dialog>
</template>
<style scoped>
.viewer-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.viewer-header span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.viewer-body { display: flex; align-items: center; justify-content: center; gap: 10px; height: calc(100dvh - 100px); }
.viewer-arrow { flex: 0 0 auto; width: 44px; height: 44px; }
.viewer-stage { display: grid; min-width: 0; flex: 1; place-items: center; }
.viewer-image { position: relative; max-width: 100%; line-height: 0; }
.viewer-image img { display: block; max-width: 100%; max-height: calc(100dvh - 120px); object-fit: contain; }
.viewer-anchor { position: absolute; z-index: 1; width: 12px; height: 12px; border: 2px solid white; border-radius: 50%; background: var(--accent); transform: translate(-50%, -50%); line-height: 1.3; }
.viewer-anchor span { position: absolute; left: 12px; top: -8px; max-width: min(220px, 35vw); padding: 4px 7px; border-radius: 5px; background: var(--surface); color: var(--ink); font-size: 12px; overflow-wrap: anywhere; }
.viewer-anchor.label-left span { left: auto; right: 12px; }
.viewer-anchor.label-up span { top: auto; bottom: -8px; }
@media (max-width: 600px) { .viewer-body { gap: 2px; } .viewer-arrow { width: 40px; } }
</style>
