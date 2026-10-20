<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NodeViewWrapper, type NodeViewProps } from '@tiptap/vue-3'
import { Delete, Rank } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { api } from '../../api/client'

const ProjectAttachmentPreview = defineAsyncComponent(() => import('../../components/ProjectAttachmentPreview.vue'))
const props = defineProps<NodeViewProps>()
const route = useRoute()
const { t } = useI18n({ useScope: 'global' })
const url = ref('')
const failed = ref(false)
const loading = ref(false)
const previewOpen = ref(false)
const draftWidth = ref(100)
const resizing = ref(false)
const videoElement = ref<HTMLVideoElement | null>(null)
const width = computed(() => Math.max(30, Math.min(100, Number(props.node.attrs.width) || 100)))
const fileName = computed(() => String(props.node.attrs.fileName || 'attachment'))
const mediaType = computed(() => String(props.node.attrs.contentType || 'application/octet-stream'))
const storekey = computed(() => String(props.node.attrs.storekey || ''))
const isVideo = computed(() => mediaType.value.startsWith('video/'))
const isImage = computed(() => mediaType.value.startsWith('image/'))
const projectID = computed(() => String(route.params.id || ''))
let requestVersion = 0
let lastCommittedWidth = width.value
let player: { dispose(): void } | null = null

/** 只有会显示在正文中的图片和视频才预取内容；Office 和 PDF 在打开详情时再按项目权限读取。 */
async function load() {
  if (!isImage.value && !isVideo.value) { loading.value = false; failed.value = false; return }
  if (!storekey.value || !projectID.value) { failed.value = true; return }
  const current = ++requestVersion
  loading.value = true
  failed.value = false
  try {
    const blob = await api.projectAttachmentContent(projectID.value, storekey.value)
    if (current === requestVersion) url.value = URL.createObjectURL(blob)
  } catch { if (current === requestVersion) failed.value = true }
  finally { if (current === requestVersion) loading.value = false }
}

function disposePlayer() { if (player) { player.dispose(); player = null } }
function beginResize() { resizing.value = true }
function previewWidth(event: Event) {
  const value = Number((event.target as HTMLInputElement).value)
  draftWidth.value = Math.max(30, Math.min(100, value))
}

/** 尺寸滑杆只更新本地草稿；指针或键盘操作结束时才提交一次文档变更。 */
function commitWidth() {
  const nextWidth = draftWidth.value
  resizing.value = false
  if (nextWidth === lastCommittedWidth) return
  lastCommittedWidth = nextWidth
  props.updateAttributes({ width: nextWidth, layout: nextWidth === 100 ? 'original' : 'small' })
}

function remove() { props.deleteNode() }
function openPreview() { previewOpen.value = true }

/** Video.js 和样式只在项目正文实际包含视频时加载，避免普通编辑页付出播放器开销。 */
async function mountVideoPlayer(source: string) {
  await import('video.js/dist/video-js.css')
  const videoModule = await import('video.js')
  if (source !== url.value || !videoElement.value) return
  player = videoModule.default(videoElement.value, { controls: true, fluid: true, preload: 'metadata', sources: [{ src: source, type: mediaType.value }] })
}

watch(() => [storekey.value, projectID.value], () => {
  requestVersion += 1
  disposePlayer()
  if (url.value) URL.revokeObjectURL(url.value)
  url.value = ''
  void load()
})
watch(width, value => {
  lastCommittedWidth = value
  if (!resizing.value) draftWidth.value = value
})
watch([url, isVideo], ([source, video]) => {
  if (!source || !video || !videoElement.value) return
  disposePlayer()
  void mountVideoPlayer(source)
}, { flush: 'post' })
onMounted(() => { draftWidth.value = width.value; void load() })
onBeforeUnmount(() => { requestVersion += 1; disposePlayer(); if (url.value) URL.revokeObjectURL(url.value) })
</script>

<template>
  <NodeViewWrapper
    class="project-resource-node attachment-node"
    :class="{ 'is-selected': selected }"
    :style="{ width: `calc(${width}% - 12px)` }"
    draggable="true"
    :title="t('projects.dragHint')"
    @dblclick.prevent.stop="openPreview"
  >
    <div
      class="attachment-frame"
      role="button"
      tabindex="0"
      :aria-label="t('projects.openAttachmentDetails', { name: fileName })"
      @keydown.enter.stop.prevent="openPreview"
      @keydown.space.stop.prevent="openPreview"
    >
      <video v-if="url && isVideo" ref="videoElement" class="video-js vjs-default-skin" playsinline controls preload="metadata"></video>
      <img v-else-if="url && isImage" class="attachment-inline-image" :src="url" :alt="fileName" draggable="false" />
      <div v-else class="attachment-file">
        <span class="attachment-file-mark">{{ mediaType === 'application/pdf' ? 'PDF' : fileName.split('.').pop()?.toUpperCase() }}</span>
        <span><strong>{{ fileName }}</strong><small>{{ loading ? t('common.loading') : failed ? t('projects.unsupportedPreview') : t('projects.openAttachmentDetails', { name: fileName }) }}</small></span>
      </div>
    </div>
    <div v-if="selected" class="resource-controls" contenteditable="false" draggable="false" role="toolbar" :aria-label="t('projects.resourceActions')" @dblclick.stop>
      <span class="resource-drag-handle" draggable="true" :title="t('projects.dragHint')" aria-hidden="true"><el-icon><Rank /></el-icon></span>
      <label class="resource-width" :title="t('projects.width')">
        <input
          type="range"
          min="30"
          max="100"
          :value="draftWidth"
          :aria-label="t('projects.width')"
          @pointerdown.stop="beginResize"
          @pointerup.stop="commitWidth"
          @pointercancel.stop="commitWidth"
          @keydown.stop="beginResize"
          @keyup.stop="commitWidth"
          @input.stop="previewWidth"
          @change.stop="commitWidth"
          @blur="commitWidth"
        />
        <span>{{ draftWidth }}%</span>
      </label>
      <el-button size="small" circle text type="danger" :title="t('projects.removeBlock')" :aria-label="t('projects.removeBlock')" @pointerdown.prevent @click.stop="remove"><el-icon><Delete /></el-icon></el-button>
    </div>
    <ProjectAttachmentPreview v-if="previewOpen" v-model="previewOpen" :project-id="projectID" :storekey="storekey" :file-name="fileName" :content-type="mediaType" />
  </NodeViewWrapper>
</template>

<style scoped>
.project-resource-node { position: relative; box-sizing: border-box; display: inline-block; max-width: calc(100% - 12px); margin: 6px; vertical-align: top; outline: none; cursor: grab; transition: width 150ms ease; }
.project-resource-node:active { cursor: grabbing; }
.project-resource-node.is-selected .attachment-frame { outline: 2px solid var(--accent); outline-offset: 2px; }
.attachment-frame { display: grid; min-height: 64px; overflow: hidden; place-items: center; border: 1px solid var(--line); border-radius: 8px; background: var(--surface-muted); cursor: inherit; }
.attachment-inline-image { display: block; width: 100%; height: auto; max-height: 70vh; object-fit: contain; }
.attachment-frame :deep(.video-js) { width: 100%; max-height: 70vh; aspect-ratio: 16 / 9; }
.attachment-file { display: flex; width: 100%; min-height: 64px; align-items: center; gap: 12px; padding: 14px; color: var(--ink); text-align: left; }
.attachment-file-mark { display: grid; width: 43px; height: 43px; flex: 0 0 43px; place-items: center; border-radius: 7px; background: var(--accent-soft); color: var(--accent-hover); font-size: 11px; font-weight: 800; }
.attachment-file > span:last-child { display: grid; min-width: 0; gap: 4px; }
.attachment-file strong { overflow: hidden; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.attachment-file small { color: var(--muted); font-size: 10px; }
.resource-controls { position: absolute; top: -40px; right: 0; z-index: 5; display: flex; min-height: 34px; align-items: center; gap: 6px; padding: 3px 6px; border: 1px solid var(--line); border-radius: 8px; background: var(--surface); box-shadow: var(--shadow); cursor: default; }
.resource-controls .el-button { width: 28px; height: 28px; margin: 0; padding: 4px; }
.resource-drag-handle { display: grid; width: 28px; height: 28px; flex: 0 0 28px; place-items: center; color: var(--muted); cursor: grab; }
.resource-width { display: flex; align-items: center; gap: 7px; color: var(--muted); font-size: 11px; }
.resource-width input { width: 110px; margin: 0; accent-color: var(--accent); cursor: ew-resize; touch-action: pan-y; }
.resource-width > span { min-width: 34px; text-align: right; }
@media (max-width: 768px) { .resource-controls { top: -38px; gap: 3px; padding: 3px; } .resource-width input { width: 88px; } }
</style>
