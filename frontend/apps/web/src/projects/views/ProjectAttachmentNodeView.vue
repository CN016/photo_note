<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NodeViewWrapper, type NodeViewProps } from '@tiptap/vue-3'
import { ArrowDown, ArrowUp } from '@element-plus/icons-vue'
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
const menuOpen = ref(false)
const videoElement = ref<HTMLVideoElement | null>(null)
const width = computed(() => Math.max(30, Math.min(100, Number(props.node.attrs.width) || 100)))
const fileName = computed(() => String(props.node.attrs.fileName || 'attachment'))
const mediaType = computed(() => String(props.node.attrs.contentType || 'application/octet-stream'))
const storekey = computed(() => String(props.node.attrs.storekey || ''))
const isVideo = computed(() => mediaType.value.startsWith('video/'))
const isImage = computed(() => mediaType.value.startsWith('image/'))
const projectID = computed(() => String(route.params.id || ''))
let requestVersion = 0
let player: { dispose(): void } | null = null

/** 正文媒体只从项目范围附件接口取回，并通过 object URL 提供给浏览器播放器或 GIF。 */
async function load() {
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
function setWidth(event: Event) { props.updateAttributes({ width: Number((event.target as HTMLInputElement).value) }) }
function setSmall() { props.updateAttributes({ width: 33, layout: 'small' }); menuOpen.value = false }
function setOriginal() { props.updateAttributes({ width: 100, layout: 'original' }); menuOpen.value = false }
function remove() { props.deleteNode() }
function moveNode(direction: -1 | 1) {
  const position = props.getPos()
  if (typeof position !== 'number') return
  const document = props.editor.state.doc
  const current = document.nodeAt(position)
  if (!current) return
  const previous = direction < 0 ? document.resolve(position).nodeBefore : null
  if (direction < 0 && !previous) return
  const siblingPosition = direction < 0 ? position - previous!.nodeSize : position + current.nodeSize
  const sibling = document.nodeAt(siblingPosition)
  if (!sibling) return
  const transaction = props.editor.state.tr.delete(position, position + current.nodeSize)
  const target = direction < 0 ? position - sibling.nodeSize : position + sibling.nodeSize
  props.editor.view.dispatch(transaction.insert(target, current).scrollIntoView())
}

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
  <NodeViewWrapper class="project-resource-node attachment-node" :class="{ 'is-selected': selected }" :style="{ width: `${width}%` }" @contextmenu.prevent="menuOpen = !menuOpen">
    <div class="attachment-frame">
      <video v-if="url && isVideo" ref="videoElement" class="video-js vjs-default-skin" playsinline controls preload="metadata"></video>
      <img v-else-if="url && isImage" class="attachment-inline-image" :src="url" :alt="fileName" draggable="false" />
      <button v-else class="attachment-file" type="button" :disabled="failed || loading" @click="previewOpen = true">
        <span class="attachment-file-mark">{{ mediaType === 'application/pdf' ? 'PDF' : fileName.split('.').pop()?.toUpperCase() }}</span>
        <span><strong>{{ fileName }}</strong><small>{{ failed ? t('projects.unsupportedPreview') : t('projects.preview') }}</small></span>
      </button>
    </div>
    <div class="resource-controls" contenteditable="false">
      <el-button size="small" @mousedown.prevent @click="setSmall">{{ t('projects.smallMode') }}</el-button>
      <el-button size="small" @mousedown.prevent @click="setOriginal">{{ t('projects.originalRatio') }}</el-button>
      <el-button size="small" type="danger" text @mousedown.prevent @click="remove">{{ t('projects.removeBlock') }}</el-button>
      <el-button size="small" :title="t('projects.moveUp')" :aria-label="t('projects.moveUp')" @mousedown.prevent @click="moveNode(-1)"><el-icon><ArrowUp /></el-icon></el-button>
      <el-button size="small" :title="t('projects.moveDown')" :aria-label="t('projects.moveDown')" @mousedown.prevent @click="moveNode(1)"><el-icon><ArrowDown /></el-icon></el-button>
      <label class="resource-width"><span>{{ t('projects.width') }}</span><input type="range" min="30" max="100" :value="width" @input="setWidth" /></label>
    </div>
    <div v-if="menuOpen" class="resource-context-menu" contenteditable="false">
      <button type="button" @mousedown.prevent @click="setSmall">{{ t('projects.smallMode') }}</button>
      <button type="button" @mousedown.prevent @click="setOriginal">{{ t('projects.originalRatio') }}</button>
      <button type="button" @mousedown.prevent @click="remove">{{ t('projects.removeBlock') }}</button>
      <button type="button" @mousedown.prevent @click="moveNode(-1)">{{ t('projects.moveUp') }}</button>
      <button type="button" @mousedown.prevent @click="moveNode(1)">{{ t('projects.moveDown') }}</button>
    </div>
    <ProjectAttachmentPreview v-if="previewOpen" v-model="previewOpen" :project-id="projectID" :storekey="storekey" :file-name="fileName" :content-type="mediaType" />
  </NodeViewWrapper>
</template>

<style scoped>
.project-resource-node { position: relative; display: block; max-width: 100%; margin: 12px auto; outline: none; }
.project-resource-node.is-selected .attachment-frame { outline: 2px solid var(--accent); outline-offset: 2px; }
.attachment-frame { display: grid; min-height: 64px; overflow: hidden; place-items: center; border: 1px solid var(--line); border-radius: 8px; background: var(--surface-muted); }
.attachment-inline-image { display: block; width: 100%; height: auto; max-height: 70vh; object-fit: contain; }
.attachment-frame :deep(.video-js) { width: 100%; max-height: 70vh; aspect-ratio: 16 / 9; }
.attachment-file { display: flex; width: 100%; align-items: center; gap: 12px; padding: 14px; border: 0; background: transparent; color: var(--ink); text-align: left; }
.attachment-file-mark { display: grid; width: 43px; height: 43px; flex: 0 0 43px; place-items: center; border-radius: 7px; background: var(--accent-soft); color: var(--accent-hover); font-size: 11px; font-weight: 800; }
.attachment-file > span:last-child { display: grid; min-width: 0; gap: 4px; }
.attachment-file strong { overflow: hidden; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.attachment-file small { color: var(--muted); font-size: 10px; }
.resource-controls { display: flex; align-items: center; flex-wrap: wrap; gap: 5px; padding: 7px 1px; }
.resource-controls .el-button { margin: 0; }
.resource-width { display: flex; align-items: center; gap: 6px; margin-left: auto; color: var(--muted); font-size: 10px; }
.resource-width input { width: 82px; accent-color: var(--accent); }
.resource-context-menu { position: absolute; z-index: 4; top: 28px; right: 7px; display: grid; min-width: 128px; padding: 5px; border: 1px solid var(--line); border-radius: 7px; background: var(--surface); box-shadow: var(--shadow); }
.resource-context-menu button { padding: 8px; border: 0; background: transparent; color: var(--ink); text-align: left; }
@media (max-width: 768px) { .resource-controls { gap: 3px; } .resource-controls .el-button { font-size: 10px; } .resource-width { width: 100%; margin-left: 0; } .resource-width input { flex: 1; } }
</style>
