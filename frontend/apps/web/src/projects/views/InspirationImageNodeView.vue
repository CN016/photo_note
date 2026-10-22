<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NodeViewWrapper, type NodeViewProps } from '@tiptap/vue-3'
import { Delete, Edit, Rank } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '../../api/client'
import type { InspirationRecord } from '../../api/types'

const props = defineProps<NodeViewProps>()
const router = useRouter()
const { t, locale } = useI18n({ useScope: 'global' })
const url = ref('')
const loading = ref(false)
const failed = ref(false)
const detailsOpen = ref(false)
const detailsLoading = ref(false)
const detailsFailed = ref(false)
const record = ref<InspirationRecord | null>(null)
const draftWidth = ref(100)
const resizing = ref(false)
const width = computed(() => Math.max(30, Math.min(100, Number(props.node.attrs.width) || 100)))
const inspirationID = computed(() => String(props.node.attrs.src ?? '').replace(/^inspiration:\/\//, ''))
const imageDimensions = computed(() => {
  const metadata = record.value?.image_metadata
  if (!metadata?.image_width || !metadata.image_height) return ''
  return `${metadata.image_width} × ${metadata.image_height}`
})
let requestVersion = 0
let lastCommittedWidth = width.value
let recordRequest: { id: string; promise: Promise<InspirationRecord> } | null = null

/** 读取当前用户有权访问的灵感图记录；同一节点的图片和详情共用进行中的请求。 */
function loadRecord(): Promise<InspirationRecord> {
  const id = inspirationID.value
  if (!id) return Promise.reject(new Error('missing_inspiration_id'))
  if (record.value?.business_id === id) return Promise.resolve(record.value)
  if (recordRequest?.id === id) return recordRequest.promise
  let promise: Promise<InspirationRecord>
  promise = api.inspiration(id).then(item => {
    if (inspirationID.value === id) record.value = item
    return item
  }).finally(() => {
    if (recordRequest?.promise === promise) recordRequest = null
  })
  recordRequest = { id, promise }
  return promise
}

/** 从 inspiration:// 业务 ID 读取灵感图详情，再通过短期 object URL 显示受控图片。 */
async function load() {
  const current = ++requestVersion
  if (!inspirationID.value) { failed.value = true; return }
  loading.value = true
  failed.value = false
  try {
    const inspiration = await loadRecord()
    const blob = await api.download(inspiration.image_key)
    if (current === requestVersion) url.value = URL.createObjectURL(blob)
  } catch { if (current === requestVersion) failed.value = true }
  finally { if (current === requestVersion) loading.value = false }
}

async function openDetails() {
  detailsOpen.value = true
  detailsLoading.value = true
  detailsFailed.value = false
  try { await loadRecord() }
  catch { detailsFailed.value = true }
  finally { detailsLoading.value = false }
}

function openEditPage() {
  detailsOpen.value = false
  void router.push({ name: 'edit-inspiration', params: { id: inspirationID.value } })
}

function formatDate(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

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

watch(width, value => {
  lastCommittedWidth = value
  if (!resizing.value) draftWidth.value = value
})
watch(inspirationID, () => {
  requestVersion += 1
  if (url.value) URL.revokeObjectURL(url.value)
  url.value = ''
  record.value = null
  recordRequest = null
  detailsOpen.value = false
  void load()
})
onMounted(() => { draftWidth.value = width.value; void load() })
onBeforeUnmount(() => { requestVersion += 1; if (url.value) URL.revokeObjectURL(url.value) })
</script>

<template>
  <NodeViewWrapper
    class="project-resource-node inspiration-node"
    :class="{ 'is-selected': selected }"
    :style="{ width: `calc(${width}% - 12px)` }"
    draggable="true"
    :title="t('projects.dragHint')"
    @dblclick.prevent.stop="openDetails"
  >
    <figure
      class="project-resource-frame"
      role="button"
      tabindex="0"
      :aria-label="t('editor.detailTitle')"
      @keydown.enter.stop.prevent="openDetails"
      @keydown.space.stop.prevent="openDetails"
    >
      <img v-if="url" :src="url" :alt="record?.file_name || t('projects.toolbar.insertInspiration')" draggable="false" />
      <span v-else class="resource-placeholder">{{ loading ? t('common.loading') : failed ? t('projects.inspirationLoadFailed') : t('projects.toolbar.insertInspiration') }}</span>
    </figure>
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

    <el-dialog v-model="detailsOpen" :title="t('editor.detailTitle')" width="min(800px, 92vw)" append-to-body destroy-on-close>
      <div v-if="detailsLoading" class="inspiration-detail-state" role="status">{{ t('common.loading') }}</div>
      <div v-else-if="detailsFailed" class="inspiration-detail-state is-error" role="alert">{{ t('projects.inspirationLoadFailed') }}</div>
      <div v-else-if="record" class="inspiration-details">
        <div class="inspiration-detail-image"><img v-if="url" :src="url" :alt="record.file_name" /></div>
        <div class="inspiration-detail-content">
          <h3>{{ record.file_name }}</h3>
          <div v-if="record.tag_names?.length" class="inspiration-detail-row">
            <span>{{ t('editor.tags') }}</span>
            <div class="inspiration-detail-tags"><el-tag v-for="name in record.tag_names" :key="name" effect="plain" size="small">{{ name }}</el-tag></div>
          </div>
          <div v-if="imageDimensions" class="inspiration-detail-row"><span>{{ t('projects.inspirationDimensions') }}</span><strong>{{ imageDimensions }} px</strong></div>
          <div class="inspiration-detail-row"><span>{{ t('projects.inspirationCreated') }}</span><strong>{{ formatDate(record.created_at) }}</strong></div>
          <el-button type="primary" class="inspiration-edit-button" @click="openEditPage"><el-icon><Edit /></el-icon>{{ t('editor.edit') }}</el-button>
        </div>
      </div>
    </el-dialog>
  </NodeViewWrapper>
</template>

<style scoped>
/* 界面表面与控件使用全局主题令牌；图片、文档和用户自选封面颜色保持原数据。 */
.project-resource-node { position: relative; box-sizing: border-box; display: inline-block; max-width: calc(100% - 12px); margin: 6px; vertical-align: top; outline: none; cursor: grab; transition: width 150ms ease; }
.project-resource-node:active { cursor: grabbing; }
.project-resource-node.is-selected .project-resource-frame { outline: 2px solid var(--accent); outline-offset: 2px; }
.project-resource-frame { display: grid; min-height: 80px; margin: 0; overflow: hidden; justify-items: center; border: 1px solid var(--line); border-radius: var(--radius-control); background: var(--surface-muted); cursor: inherit; }
.project-resource-frame img { display: block; width: 100%; height: auto; max-height: 70vh; object-fit: contain; }
.resource-placeholder { align-self: center; padding: 18px; color: var(--muted); font-size: 12px; text-align: center; }
.resource-controls { position: absolute; top: -40px; right: 0; z-index: 5; display: flex; min-height: 34px; align-items: center; gap: 6px; padding: 3px 6px; border: 1px solid var(--line); border-radius: var(--radius-control); background: var(--surface-overlay); box-shadow: var(--shadow); cursor: default; }
.resource-controls .el-button { width: 28px; height: 28px; margin: 0; padding: 4px; }
.resource-drag-handle { display: grid; width: 28px; height: 28px; flex: 0 0 28px; place-items: center; color: var(--muted); cursor: grab; }
.resource-width { display: flex; align-items: center; gap: 7px; color: var(--muted); font-size: 11px; }
.resource-width input { width: 110px; margin: 0; accent-color: var(--accent); cursor: ew-resize; touch-action: pan-y; }
.resource-width > span { min-width: 34px; text-align: right; }
.inspiration-details { display: grid; grid-template-columns: minmax(0, 1.2fr) minmax(220px, .8fr); gap: 20px; align-items: start; }
.inspiration-detail-image { display: grid; min-height: 220px; max-height: 65vh; place-items: center; overflow: hidden; border-radius: var(--radius-control); background: var(--surface-muted); }
.inspiration-detail-image img { display: block; max-width: 100%; max-height: 65vh; object-fit: contain; }
.inspiration-detail-content { display: grid; align-content: start; gap: 14px; min-width: 0; }
.inspiration-detail-content h3 { margin: 0; overflow-wrap: anywhere; color: var(--ink); font-size: 15px; }
.inspiration-detail-row { display: grid; gap: 6px; color: var(--muted); font-size: 12px; }
.inspiration-detail-row strong { color: var(--ink); font-weight: 600; }
.inspiration-detail-tags { display: flex; flex-wrap: wrap; gap: 5px; }
.inspiration-edit-button { justify-self: start; margin-top: 4px; }
.inspiration-detail-state { padding: 42px 12px; color: var(--muted); text-align: center; }
.inspiration-detail-state.is-error { color: var(--danger); }
@media (max-width: 768px) {
  /* 资源条与底部编辑工具栏错层，44px 操作区域无需挤入图片自身宽度。 */
  .resource-controls { position: fixed; inset: auto 12px calc(72px + env(safe-area-inset-bottom)); min-height: 54px; justify-content: center; gap: 6px; padding: 4px; }
  .resource-controls .el-button, .resource-drag-handle { width: 44px; height: 44px; min-height: 44px; flex: 0 0 44px; }
  .resource-width { min-height: 44px; }
  .resource-width input { width: 88px; height: 44px; }
  .inspiration-details { grid-template-columns: 1fr; gap: 14px; }
  .inspiration-detail-image { min-height: 150px; }
}
</style>
