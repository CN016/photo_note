<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { onBeforeRouteLeave, RouterLink, useRouter } from 'vue-router'
import { ArrowLeft, Check, Delete, Picture, Plus, UploadFilled } from '@element-plus/icons-vue'
import AppHeader from '../components/AppHeader.vue'
import { ApiError, type InspirationImageMetadata } from '../api/types'
import { api } from '../api/client'
import { createEditorDocument } from '../../../../packages/editor-core/src'
import {
  analyzeImageColors, decodeImagePreview, makeWorkingJpeg, previewObjectUrl, readInspirationImageMetadata, releaseCanvas,
  type ColorAnalysis,
} from '../../../../packages/image-processing/src'

/** 单张图片在批次中的处理阶段，用于区分预览准备、上传和失败重试。 */
type UploadStage = 'preparing' | 'ready' | 'uploading' | 'done' | 'failed'

/** 单张图片在批次中的前端状态；上传分阶段保存 key，以便失败后只重试未完成步骤。 */
interface InspirationUploadItem {
  id: number
  file: File
  stage: UploadStage
  previewUrl: string
  previewFailed: boolean
  imageMetadata: InspirationImageMetadata | null
  colorAnalysis: ColorAnalysis | null
  sourceImageKey: string
  workingImageKey: string
  progressLabel: string
  error: string
}

const { t, locale } = useI18n()
const router = useRouter()
const fileInput = ref<HTMLInputElement | null>(null)
const items = ref<InspirationUploadItem[]>([])
const preparing = ref(false)
const uploading = ref(false)
let nextItemID = 0
let disposed = false

const completedCount = computed(() => items.value.filter(item => item.stage === 'done').length)
const selectedCount = computed(() => items.value.length)
const hasRetryableItems = computed(() => items.value.some(item => !item.previewFailed && item.previewUrl && (item.stage === 'ready' || item.stage === 'failed')))
const canChooseFiles = computed(() => !preparing.value && !uploading.value)

/** 将单个文件错误映射到当前语言，服务端原始文本可能包含路径等不宜直接展示的细节。 */
function localError(cause: unknown): string {
  if (cause instanceof ApiError) {
    if (cause.code === 'network') return t('common.errors.network')
    const key = `common.errors.${cause.code}`
    const translated = t(key)
    if (translated !== key) return translated
  }
  return t('inspirationUpload.uploadFailed')
}

/** 打开隐藏的多选文件框；上传和预览生成期间禁用，避免队列状态被并发修改。 */
function chooseFiles(): void {
  if (canChooseFiles.value) fileInput.value?.click()
}

/** 从文件框读取本次选择，重置 input 以允许用户再次选择同名文件。 */
function onFileInput(event: Event): void {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  void addFiles(files)
}

/** 支持将多张图片拖入预览区，与文件选择器使用同一预览和属性读取流程。 */
function onDrop(event: DragEvent): void {
  if (!canChooseFiles.value) return
  void addFiles(Array.from(event.dataTransfer?.files ?? []))
}

/** 按张准备预览，避免同时解码多张原尺寸图片；元数据始终从用户选中的原始 File 读取。 */
async function addFiles(files: File[]): Promise<void> {
  if (!files.length || !canChooseFiles.value) return
  preparing.value = true
  for (const file of files) {
    const item: InspirationUploadItem = {
      id: nextItemID++, file, stage: 'preparing', previewUrl: '', previewFailed: false,
      imageMetadata: null, colorAnalysis: null, sourceImageKey: '', workingImageKey: '', progressLabel: '', error: '',
    }
    items.value.push(item)
    try {
      item.imageMetadata = await readInspirationImageMetadata(file)
      if (disposed) return
      item.previewUrl = await previewObjectUrl(file, 720)
      item.stage = 'ready'
    } catch {
      item.stage = 'failed'
      item.previewFailed = true
      item.error = t('inspirationUpload.previewFailed')
    }
    if (disposed) {
      releaseItemPreview(item)
      return
    }
  }
  preparing.value = false
}

/** 释放缩略图对象地址，避免移除文件或离开页面后继续占用浏览器资源。 */
function releaseItemPreview(item: InspirationUploadItem): void {
  if (!item.previewUrl) return
  URL.revokeObjectURL(item.previewUrl)
  item.previewUrl = ''
}

/** 从待上传批次移除一张图片；已成功提交的记录不能通过此按钮误删。 */
function removeItem(item: InspirationUploadItem): void {
  if (uploading.value || item.stage === 'done') return
  releaseItemPreview(item)
  items.value = items.value.filter(candidate => candidate.id !== item.id)
  if (items.value.length > 0 && items.value.every(candidate => candidate.stage === 'done')) void router.replace({ name: 'gallery' })
}

/** 按当前语言显示文件大小；原始字节数仍完整保留在 File 属性和元数据中。 */
function formatFileSize(bytes: number): string {
  const kilobytes = bytes / 1024
  return `${new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }).format(kilobytes)} KB`
}

/** 从一个原始 File 生成独立的工作副本，并保留后台解析原图属性需要的原图 key。 */
async function uploadItem(item: InspirationUploadItem): Promise<void> {
  item.stage = 'uploading'
  item.error = ''
  try {
    if (!item.sourceImageKey) {
      item.progressLabel = t('inspirationUpload.uploadingOriginal')
      item.sourceImageKey = (await api.upload(item.file, item.file.name)).storekey
    }

    if (!item.workingImageKey) {
      item.progressLabel = t('inspirationUpload.preparingWorkingCopy')
      const decoded = await decodeImagePreview(item.file)
      let working: Blob
      try {
        // 与旧单图流程一致地生成颜色画像，随创建数据保存，批量新增后无需逐张打开详情触发分析。
        try { item.colorAnalysis = analyzeImageColors(decoded.canvas) }
        catch { item.colorAnalysis = null }
        working = await makeWorkingJpeg(decoded.canvas)
      }
      finally { releaseCanvas(decoded.canvas) }
      const baseName = item.file.name.replace(/\.[^.]+$/, '') || 'inspiration'
      item.progressLabel = t('inspirationUpload.uploadingPreview')
      item.workingImageKey = (await api.upload(working, `${baseName}.jpg`)).storekey
    }

    item.progressLabel = t('inspirationUpload.creatingRecord')
    const data: Record<string, unknown> = { ...createEditorDocument(), source_image_key: item.sourceImageKey }
    if (item.colorAnalysis) data.color_analysis = item.colorAnalysis
    const metadata = item.imageMetadata ?? { source_file_size: item.file.size, extra: {} }
    await api.createInspiration(item.workingImageKey, item.file.name, data, metadata)
    item.stage = 'done'
    item.progressLabel = t('inspirationUpload.uploaded')
  } catch (cause) {
    item.stage = 'failed'
    item.progressLabel = ''
    item.error = localError(cause)
  }
}

/** 顺序提交可重试图片；已成功项保持 done，原图和工作图已完成的步骤会复用其 key。 */
async function uploadAll(): Promise<void> {
  if (uploading.value || preparing.value || !hasRetryableItems.value) return
  uploading.value = true
  const pending = items.value.filter(item => !item.previewFailed && item.previewUrl && (item.stage === 'ready' || item.stage === 'failed'))
  for (const item of pending) await uploadItem(item)
  uploading.value = false
  if (items.value.length > 0 && items.value.every(item => item.stage === 'done')) {
    await router.replace({ name: 'gallery' })
  }
}

/** 上传或预览准备期间阻止离开，避免正在处理的文件批次失去可见状态。 */
onBeforeRouteLeave(to => {
  if (to.path === '/spaces' && items.value.some(item => item.stage !== 'done')) return false
  if (!preparing.value && !uploading.value) return true
  ElMessage.warning(t('inspirationUpload.waitBeforeLeaving'))
  return false
})

/** 离开页面时释放每张预览图的对象地址；后台属性解析和创建仍使用独立的原始 File。 */
onBeforeUnmount(() => {
  disposed = true
  for (const item of items.value) releaseItemPreview(item)
})
</script>

<template>
  <div class="workspace-page inspiration-upload-page">
    <AppHeader />
    <main class="upload-main">
      <header class="upload-heading">
        <RouterLink class="back-link" to="/inspirations"><el-icon aria-hidden="true"><ArrowLeft /></el-icon>{{ t('common.back') }}</RouterLink>
        <p class="eyebrow">{{ t('common.product') }}</p>
        <h1>{{ t('inspirationUpload.title') }}</h1>
        <p class="upload-subtitle">{{ t('inspirationUpload.subtitle') }}</p>
      </header>

      <input ref="fileInput" class="file-input-hidden" type="file" accept="image/*,.heic,.heif" multiple :disabled="!canChooseFiles" @change="onFileInput" />

      <section class="upload-panel" :aria-label="t('inspirationUpload.previewLabel')" :aria-busy="preparing || uploading">
        <!-- 整个拖放区域复用原生按钮语义，点击、回车和空格均可选图，内部不再嵌套第二个按钮。 -->
        <button v-if="items.length === 0" type="button" class="new-upload-dropzone" :disabled="!canChooseFiles" :aria-label="t('inspirationUpload.chooseImages')" @click="chooseFiles" @dragover.prevent @drop.prevent="onDrop">
          <span class="upload-dropzone-icon"><el-icon aria-hidden="true"><UploadFilled /></el-icon></span>
          <strong class="dropzone-title">{{ t('inspirationUpload.dropTitle') }}</strong>
          <span class="dropzone-hint">{{ t('inspirationUpload.dropHint') }}</span>
          <span class="primary-button"><el-icon aria-hidden="true"><Plus /></el-icon>{{ t('inspirationUpload.chooseImages') }}</span>
        </button>

        <template v-else>
          <div class="upload-toolbar">
            <div>
              <h2>{{ t('inspirationUpload.selectedCount', { count: selectedCount }) }}</h2>
              <p>{{ t('inspirationUpload.previewHint') }}</p>
            </div>
            <el-button plain :disabled="!canChooseFiles" @click="chooseFiles"><el-icon aria-hidden="true"><Plus /></el-icon>{{ t('inspirationUpload.addImages') }}</el-button>
          </div>

          <div class="upload-grid">
            <article v-for="item in items" :key="item.id" class="upload-card" :class="{ 'is-done': item.stage === 'done', 'has-error': item.stage === 'failed' }">
              <div class="upload-card-preview">
                <img v-if="item.previewUrl" :src="item.previewUrl" :alt="item.file.name" />
                <div v-else class="upload-card-placeholder" role="img" :aria-label="t('inspirationUpload.previewFailed')"><el-icon aria-hidden="true"><Picture /></el-icon></div>
                <el-button v-if="item.stage !== 'done'" class="remove-image-button" circle :disabled="uploading || preparing" :aria-label="t('inspirationUpload.removeImage', { name: item.file.name })" @click="removeItem(item)"><el-icon aria-hidden="true"><Delete /></el-icon></el-button>
                <span v-else class="uploaded-mark" :aria-label="t('inspirationUpload.uploaded')"><el-icon aria-hidden="true"><Check /></el-icon></span>
              </div>
              <div class="upload-card-info">
                <strong :title="item.file.name">{{ item.file.name }}</strong>
                <span>{{ formatFileSize(item.file.size) }}</span>
                <p v-if="item.stage === 'preparing'" class="upload-card-status is-pending" role="status">{{ t('inspirationUpload.preparingPreview') }}</p>
                <p v-else-if="item.stage === 'ready'" class="upload-card-status is-ready">{{ t('inspirationUpload.ready') }}</p>
                <p v-else-if="item.stage === 'uploading'" class="upload-card-status is-pending" role="status">{{ item.progressLabel }}</p>
                <p v-else-if="item.stage === 'done'" class="upload-card-status is-done-text">{{ item.progressLabel }}</p>
                <p v-else class="upload-card-status is-error" role="alert">{{ item.error || t('inspirationUpload.uploadFailed') }}</p>
              </div>
            </article>
          </div>

          <div class="upload-actions">
            <p v-if="uploading || completedCount" class="upload-progress" role="status">{{ t('inspirationUpload.progress', { done: completedCount, total: selectedCount }) }}</p>
            <span v-else class="upload-progress-note">{{ t('inspirationUpload.readyToUpload') }}</span>
            <el-button class="primary-button upload-submit" type="primary" :disabled="!hasRetryableItems || preparing || uploading" :loading="uploading" @click="uploadAll">
              {{ items.some(item => item.stage === 'done') ? t('inspirationUpload.retryFailed') : t('inspirationUpload.uploadAll') }}
              <el-icon v-if="!uploading" aria-hidden="true"><Check /></el-icon>
            </el-button>
          </div>
        </template>
      </section>
    </main>
    <footer class="workspace-footer"><span>{{ t('common.brand') }}</span><span>{{ t('common.footer') }}</span></footer>
  </div>
</template>

<style scoped>
/* 界面表面与控件使用全局主题令牌；图片、文档和用户自选封面颜色保持原数据。 */
.upload-main { width: min(100% - 48px, 1160px); margin: 0 auto; padding: 42px 0 72px; }
.upload-heading { margin-bottom: 30px; }
.back-link { display: inline-flex; align-items: center; gap: 7px; color: var(--muted); font-size: 13px; text-decoration: none; margin-bottom: 24px; }
.back-link:hover { color: var(--accent-hover); }
.upload-heading > .eyebrow { margin: 0 0 8px; color: var(--muted); font-size: 12px; font-weight: 700; letter-spacing: .11em; text-transform: uppercase; }
h1 { margin: 0; color: var(--ink); font-family: 'Manrope', sans-serif; font-size: clamp(30px, 4vw, 42px); font-weight: 700; line-height: 1.15; letter-spacing: -.05em; }
.upload-subtitle { max-width: 600px; margin: 12px 0 0; color: var(--ink-soft); font-size: 15px; line-height: 1.7; }
.file-input-hidden { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; clip-path: inset(50%); }
.upload-panel { overflow: hidden; border: 1px solid var(--line); border-radius: var(--radius-card); background: var(--surface); box-shadow: var(--soft-shadow); }
.new-upload-dropzone { display: flex; width: calc(100% - 32px); color: var(--ink); cursor: pointer; min-height: 340px; align-items: center; justify-content: center; flex-direction: column; padding: 44px 24px; border: 1px dashed var(--line-strong); border-radius: var(--radius-control); margin: 16px; background: var(--surface-soft); text-align: center; transition: border-color .18s ease, background-color .18s ease; }
.new-upload-dropzone:hover:not(:disabled) { border-color: var(--accent); background: var(--accent-soft); }
.upload-dropzone-icon { display: grid; width: 64px; height: 64px; place-items: center; border-radius: 20px; background: var(--accent-soft); color: var(--accent); font-size: 27px; }
.new-upload-dropzone .dropzone-title { margin: 20px 0 7px; color: var(--ink); font-size: 19px; }
.new-upload-dropzone .dropzone-hint { margin: 0 0 20px; color: var(--ink-soft); font-size: 13px; }
.upload-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 22px 24px; border-bottom: 1px solid var(--line); }
.upload-toolbar h2 { margin: 0; color: var(--ink); font-size: 16px; }
.upload-toolbar p { margin: 6px 0 0; color: var(--ink-soft); font-size: 12px; }
.upload-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 18px; padding: 24px; }
.upload-card { overflow: hidden; min-width: 0; border: 1px solid var(--line); border-radius: var(--radius-card); background: var(--surface); transition: border-color .18s ease, box-shadow .18s ease; }
.upload-card:hover { border-color: var(--accent); box-shadow: var(--soft-shadow); }
.upload-card.is-done { border-color: var(--success); }
.upload-card.has-error { border-color: var(--danger); }
.upload-card-preview { position: relative; overflow: hidden; aspect-ratio: 4 / 5; background: var(--surface-soft); }
.upload-card-preview img { display: block; width: 100%; height: 100%; object-fit: contain; }
.upload-card-placeholder { display: grid; width: 100%; height: 100%; place-items: center; color: var(--muted); font-size: 34px; }
.remove-image-button { position: absolute; top: 10px; right: 10px; width: 44px; height: 44px; box-shadow: 0 2px 10px rgb(0 0 0 / 13%); }
.uploaded-mark { position: absolute; top: 10px; right: 10px; display: grid; width: 30px; height: 30px; place-items: center; border: 2px solid var(--surface); border-radius: 50%; background: var(--success); color: var(--on-success); box-shadow: 0 2px 10px rgb(0 0 0 / 13%); }
.upload-card-info { padding: 12px 13px 14px; }
.upload-card-info strong { display: block; overflow: hidden; color: var(--ink); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.upload-card-info > span { display: block; margin-top: 5px; color: var(--muted); font-size: 11px; }
.upload-card-status { min-height: 17px; margin: 9px 0 0; font-size: 11px; line-height: 1.45; }
.is-pending, .is-ready { color: var(--ink-soft); }
.is-done-text { color: var(--success); }
.is-error { color: var(--danger); }
.upload-actions { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 18px 24px; border-top: 1px solid var(--line); background: var(--surface-soft); }
.upload-progress, .upload-progress-note { margin: 0; color: var(--ink-soft); font-size: 13px; }
.upload-submit { min-width: 180px; }
.upload-toolbar :deep(.el-button) { min-height: 44px; }
.workspace-footer { width: min(100% - 48px, 1160px); margin: 0 auto; padding: 18px 0 24px; color: var(--muted); font-size: 11px; }

@media (max-width: 960px) {
  .upload-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
@media (max-width: 680px) {
  .upload-main { width: min(100% - 28px, 1160px); padding: 28px 0 50px; }
  .upload-heading { margin-bottom: 22px; }
  .back-link { margin-bottom: 20px; }
  .new-upload-dropzone { width: calc(100% - 20px); min-height: 290px; padding: 32px 18px; margin: 10px; }
  .upload-toolbar { align-items: flex-start; padding: 18px 16px; }
  .upload-toolbar .el-button { flex: 0 0 auto; }
  .upload-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 11px; padding: 14px; }
  .upload-card-info { padding: 10px 10px 12px; }
  .upload-actions { align-items: stretch; flex-direction: column; padding: 16px; }
  .upload-submit { width: 100%; }
  .workspace-footer { width: min(100% - 28px, 1160px); }
}
</style>
