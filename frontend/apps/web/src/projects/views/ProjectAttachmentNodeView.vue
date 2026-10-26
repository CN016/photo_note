<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NodeViewWrapper, type NodeViewProps } from '@tiptap/vue-3'
import { ArrowLeft, ArrowRight, BottomRight, Delete, Rank } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { useResourceResize } from '../resourceResize'
import { useResourceMovement } from '../resourceMovement'
import { api } from '../../api/client'

const ProjectAttachmentPreview = defineAsyncComponent(() => import('../../components/ProjectAttachmentPreview.vue'))
const props = defineProps<NodeViewProps>()
const { canMoveBefore, canMoveAfter, move, touchDragging, startBlockTouch, startHandleTouch, selectResource, touchDevice } = useResourceMovement(props)
const route = useRoute()
const { t } = useI18n({ useScope: 'global' })
const url = ref('')
const failed = ref(false)
const loading = ref(false)
const previewOpen = ref(false)
const videoElement = ref<HTMLVideoElement | null>(null)
const { widthLabel, smallMode, originalMode, setSizePreset, draftWidth, cornerResizing, displayWidth, beginResize, previewWidth, commitWidth, startCornerResize, moveCornerResize, finishCornerResize, cancelCornerResize, resizeWithKeyboard } = useResourceResize(props)
const fileName = computed(() => String(props.node.attrs.fileName || 'attachment'))
const mediaType = computed(() => String(props.node.attrs.contentType || 'application/octet-stream'))
const storekey = computed(() => String(props.node.attrs.storekey || ''))
const isVideo = computed(() => mediaType.value.startsWith('video/'))
const isImage = computed(() => mediaType.value.startsWith('image/'))
const projectID = computed(() => String(route.params.id || ''))
let requestVersion = 0
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

watch([url, isVideo], ([source, video]) => {
  if (!source || !video || !videoElement.value) return
  disposePlayer()
  void mountVideoPlayer(source)
}, { flush: 'post' })
onMounted(() => { void load() })
onBeforeUnmount(() => { requestVersion += 1; disposePlayer(); if (url.value) URL.revokeObjectURL(url.value) })
</script>

<template>
  <NodeViewWrapper
    class="project-resource-node attachment-node"
    :class="{ 'is-selected': selected, 'is-corner-resizing': cornerResizing, 'is-touch-dragging': touchDragging }"
    :style="{ width: `calc(${displayWidth}% - 12px)` }"
    :draggable="!touchDevice"
    contenteditable="false"
    @mousedown="selectResource" @click="selectResource" @contextmenu.prevent
    @touchstart="startBlockTouch"
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
    <el-tooltip v-if="selected" :content="t('projects.resizeMedia')" placement="bottom">
      <button class="resource-resize-handle" type="button" contenteditable="false" draggable="false"
        :aria-label="t('projects.resizeMedia')" @pointerdown.stop.prevent="startCornerResize"
        @pointermove.stop.prevent="moveCornerResize" @pointerup.stop.prevent="finishCornerResize"
        @pointercancel.stop="cancelCornerResize" @lostpointercapture="cancelCornerResize"
        @keydown.stop="resizeWithKeyboard" @click.stop @dblclick.stop @dragstart.stop.prevent>
        <el-icon><BottomRight /></el-icon>
      </button>
    </el-tooltip>
    <div v-if="selected" class="resource-controls" contenteditable="false" draggable="false" role="toolbar" :aria-label="t('projects.resourceActions')" @dblclick.stop>
      <div class="resource-size-presets">
        <el-tooltip :content="t('projects.smallMediaHint')"><el-button size="small" :type="smallMode ? 'primary' : 'default'" :aria-pressed="smallMode" @pointerdown.prevent @click.stop="setSizePreset(true)">{{ t('projects.smallMedia') }}</el-button></el-tooltip>
        <el-tooltip :content="t('projects.originalMediaHint')"><el-button size="small" :type="originalMode ? 'primary' : 'default'" :aria-pressed="originalMode" @pointerdown.prevent @click.stop="setSizePreset(false)">{{ t('projects.originalMedia') }}</el-button></el-tooltip>
      </div>
      <el-tooltip :content="t('projects.moveBlock')" placement="top">
        <span class="resource-drag-handle" @touchstart.stop="startHandleTouch" data-drag-handle :draggable="!touchDevice" :aria-label="t('projects.moveBlock')"><el-icon><Rank /></el-icon></span>
      </el-tooltip>
      <el-tooltip :content="t('projects.moveBefore')"><el-button text :disabled="!canMoveBefore" :aria-label="t('projects.moveBefore')" @pointerdown.prevent @click.stop="move(-1)"><el-icon><ArrowLeft /></el-icon></el-button></el-tooltip>
      <el-tooltip :content="t('projects.moveAfter')"><el-button text :disabled="!canMoveAfter" :aria-label="t('projects.moveAfter')" @pointerdown.prevent @click.stop="move(1)"><el-icon><ArrowRight /></el-icon></el-button></el-tooltip>
      <el-tooltip :content="t('projects.width')" placement="top">
      <label class="resource-width">
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
        <span>{{ widthLabel }}</span>
      </label>
      </el-tooltip>
      <el-tooltip :content="t('projects.removeBlock')" placement="top"><el-button size="small" circle text type="danger" :aria-label="t('projects.removeBlock')" @pointerdown.prevent @click.stop="remove"><el-icon><Delete /></el-icon></el-button></el-tooltip>
    </div>
    <ProjectAttachmentPreview v-if="previewOpen" v-model="previewOpen" :project-id="projectID" :storekey="storekey" :file-name="fileName" :content-type="mediaType" />
  </NodeViewWrapper>
</template>

<style scoped>
/* 界面表面与控件使用全局主题令牌；图片、文档和用户自选封面颜色保持原数据。 */
.project-resource-node { position: relative; box-sizing: border-box; display: inline-block; max-width: calc(100% - 12px); margin: 6px; vertical-align: top; outline: none; transition: width 150ms ease; }
.project-resource-node.is-selected .attachment-frame { outline: 2px solid var(--accent); outline-offset: 2px; }
.attachment-frame { display: grid; min-height: 64px; overflow: hidden; place-items: center; border: 1px solid var(--line); border-radius: var(--radius-control); background: var(--surface-muted); cursor: inherit; }
.attachment-inline-image { display: block; width: 100%; height: auto; max-height: 70vh; object-fit: contain; }
.attachment-frame :deep(.video-js) { width: 100%; max-height: 70vh; aspect-ratio: 16 / 9; }
.attachment-file { display: flex; width: 100%; min-height: 64px; align-items: center; gap: 12px; padding: 14px; color: var(--ink); text-align: left; }
.attachment-file-mark { display: grid; width: 43px; height: 43px; flex: 0 0 43px; place-items: center; border-radius: var(--radius-control); background: var(--accent-soft); color: var(--accent-hover); font-size: 11px; font-weight: 800; }
.attachment-file > span:last-child { display: grid; min-width: 0; gap: 4px; }
.attachment-file strong { overflow: hidden; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.attachment-file small { color: var(--muted); font-size: 10px; }
.resource-controls { position: absolute; top: -8px; right: 0; transform: translateY(-100%); z-index: 12; display: flex; flex-wrap: wrap; width: max-content; max-width: calc(100vw - 24px); min-height: 34px; align-items: center; gap: 6px; padding: 3px 6px; border: 1px solid var(--line); border-radius: var(--radius-control); background: var(--surface-overlay); box-shadow: var(--shadow); cursor: default; }
.resource-controls .el-button { width: 28px; height: 28px; margin: 0; padding: 4px; }
.resource-size-presets { display: flex; width: 100%; align-items: center; justify-content: center; gap: 6px; }
.resource-size-presets .el-button { width: auto; padding: 0 10px; font-size: 11px; flex: 0 0 auto; }
.resource-drag-handle { display: grid; width: 28px; height: 28px; flex: 0 0 28px; place-items: center; border: 0; border-radius: var(--radius-control); background: transparent; color: var(--muted); cursor: grab; }
.resource-width { display: flex; align-items: center; gap: 7px; color: var(--muted); font-size: 11px; }
.resource-width input { width: 110px; margin: 0; accent-color: var(--accent); cursor: ew-resize; touch-action: pan-y; }
.resource-width > span { min-width: 34px; text-align: right; }
/* 手机上将资源操作固定在编辑工具栏上方，避免窄图片的浮动按钮超出屏幕。 */
@media (max-width: 768px) {
  .resource-controls { position: fixed; width: auto; transform: none; inset: auto 12px calc(var(--project-keyboard-bottom, 0px) + 64px + env(safe-area-inset-bottom)); min-height: 54px; justify-content: center; gap: 6px; padding: 4px; }
  .resource-controls .el-button, .resource-drag-handle { width: 44px; height: 44px; min-height: 44px; flex: 0 0 44px; }
  .resource-size-presets .el-button { width: auto; flex: 0 0 auto; padding: 0 12px; }
  .resource-width { min-height: 44px; }
  .resource-width input { width: 88px; height: 44px; }
}
.resource-drag-handle { touch-action: none; }
.is-touch-dragging { outline: 3px dashed var(--accent); opacity: .7; }
</style>
