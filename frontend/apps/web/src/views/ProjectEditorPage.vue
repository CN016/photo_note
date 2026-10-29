<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, shallowReactive, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import type { Editor } from '@tiptap/core'
import { ElMessage } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import ProjectDocumentEditor from '../projects/views/ProjectDocumentEditor.vue'
import { useI18n } from 'vue-i18n'
import AppHeader from '../components/AppHeader.vue'
import ProjectCoverImage from '../components/ProjectCoverImage.vue'
import ProjectInspirationPicker from '../projects/views/ProjectInspirationPicker.vue'
import { api, getSessionInfo } from '../api/client'
import { addUploadPlaceholders, hasUploadPlaceholder, removeUploadPlaceholder, replaceUploadPlaceholder } from '../projects/uploadPlaceholders'
import type { InspirationRecord, ProjectRecord, ProjectSaveInput } from '../api/types'
import { createProjectInspiration } from '../projects/createProjectInspiration'
import { fallbackProjectUploadLimit, projectUploadLimit } from '../projects/projectUploadLimit'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n({ useScope: 'global' })
const projectID = computed(() => String(route.params.id || ''))
const project = ref<ProjectRecord | null>(null)
const title = ref('')
const coverEmoji = ref('')
const coverImageKey = ref('')
const coverFileName = ref('')
const loading = ref(true)
const loadFailed = ref(false)
const savingState = ref<'saved' | 'unsaved' | 'saving' | 'error'>('saved')
const pickerOpen = ref(false)
const documentIncompatible = ref(false)
const readOnly = ref(false)
const mediaInput = ref<HTMLInputElement | null>(null)
const attachmentInput = ref<HTMLInputElement | null>(null)
const maxUploadBytes = ref(fallbackProjectUploadLimit)
let saveTimer: number | undefined
let changeVersion = 0
let saveFlight: Promise<boolean> | null = null
let hydrating = true
const uploadingAttachments = ref(0)

// 仅在加载项目时更新初始正文，保存响应不能覆盖请求期间产生的新编辑。
const initialDocument = shallowRef<Record<string, unknown>>({ type: 'doc', content: [{ type: 'paragraph' }] })
const editor = shallowRef<Editor>()

/** 编辑器实例由开源组件创建；接入页面唯一的保存管线，正文由组件按初始 JSON 恢复。 */
function editorReady(instance: Editor) {
  editor.value = instance
}

function saveInput(): ProjectSaveInput {
  return {
    default_read_only: readOnly.value, title: title.value.trim(), cover_emoji: coverEmoji.value, cover_image_key: coverImageKey.value,
    cover_file_name: coverFileName.value, document_schema_version: 1,
    document: editor.value?.getJSON() ?? { type: 'doc', content: [{ type: 'paragraph' }] },
    revision: project.value?.revision ?? 1,
  }
}

/** 标题、封面和正文作为同一版本化快照延迟保存；并发编辑时由服务端 revision 拒绝旧快照。 */
function scheduleSave() {
  if (hydrating || !project.value || documentIncompatible.value) return
  changeVersion += 1
  savingState.value = 'unsaved'
  if (saveTimer) window.clearTimeout(saveTimer)
  saveTimer = window.setTimeout(() => { void saveNow() }, 750)
}

async function saveNow(): Promise<boolean> {
  if (!project.value || !editor.value || documentIncompatible.value) return false
  if (saveFlight) return saveFlight
  if (saveTimer) { window.clearTimeout(saveTimer); saveTimer = undefined }
  const version = changeVersion
  savingState.value = 'saving'
  saveFlight = (async () => {
    try {
      const updated = await api.saveProject(projectID.value, saveInput())
      project.value = updated
      savingState.value = version === changeVersion ? 'saved' : 'unsaved'
      if (version !== changeVersion) { if (saveTimer) window.clearTimeout(saveTimer); saveTimer = window.setTimeout(() => { void saveNow() }, 100) }
      return true
    } catch {
      savingState.value = 'error'
      ElMessage.error(t('projects.saveError'))
      return false
    } finally { saveFlight = null }
  })()
  return saveFlight
}

/** 加载单条项目后恢复版本号、标题和 Tiptap 文档；文件资源仍由自定义节点异步读取。 */
async function loadProject() {
  loading.value = true
  loadFailed.value = false
  documentIncompatible.value = false
  editor.value = undefined
  hydrating = true
  try {
    const result = await api.project(projectID.value)
    project.value = result
    initialDocument.value = result.document
    readOnly.value = result.default_read_only ?? false
    title.value = result.title
    coverEmoji.value = result.cover_emoji
    coverImageKey.value = result.cover_image_key
    coverFileName.value = result.cover_file_name
    savingState.value = 'saved'
  } catch { loadFailed.value = true }
  finally { hydrating = false; loading.value = false }
}

async function loadUploadLimit() {
  maxUploadBytes.value = await projectUploadLimit()
}

/** 切换模式同时保存为下次打开默认值；正文保存仍受修订号保护。 */
function switchMode() {
  readOnly.value = !readOnly.value
  scheduleSave()
}

function insertInspiration(item: InspirationRecord) {
  editor.value?.chain().focus().insertContent({ type: 'inspirationImage', attrs: { src: `inspiration://${item.business_id}`, width: 100, layout: 'original' } }).run()
}

function extOf(name: string) { return name.toLowerCase().split('.').pop() ?? '' }
const supportedExtensions = new Set(['gif', 'mp4', 'webm', 'ogv', 'pdf', 'doc', 'docx', 'xls', 'xlsx'])
const imageExtensions = new Set(['jpg', 'jpeg', 'png', 'webp', 'heic', 'heif'])

/** 常见静态图片沿用灵感图压缩、元数据和引用流程；视频、GIF、Office、PDF 保持原文件作为附件。 */
async function uploadFiles(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  await importFiles(files)
}

type UploadStage = 'queued' | 'preparing' | 'original' | 'file' | 'processing' | 'working' | 'creating' | 'sending' | 'waiting' | 'failed'
type UploadJob = {
  id: string; file: File; scope: string; projectID: string; element: HTMLElement; status: HTMLElement
  progress: HTMLProgressElement; retry: HTMLButtonElement; remove: HTMLButtonElement; previewURL: string
  stage: UploadStage; busy: boolean; done: Promise<void>; complete: () => void
}
const uploadJobs = shallowReactive(new Map<string, UploadJob>())
/** 将账户和空间代次绑定到整条异步上传链，不能把旧文件重试到新空间。 */
function uploadScope(): string {
  const info = getSessionInfo()
  return info ? `${info.session_id}:${info.orgid}:${info.session_version}` : ''
}
function checkUploadScope(job: UploadJob) {
  if (!job.scope || uploadScope() !== job.scope || projectID.value !== job.projectID) throw new Error('space_changed')
}
function discardUpload(job: UploadJob) {
  if (job.busy) return
  uploadJobs.delete(job.id)
  job.complete()
  if (editor.value) removeUploadPlaceholder(editor.value, job.id)
  if (job.previewURL) URL.revokeObjectURL(job.previewURL)
}
/** 状态和进度属于临时 UI，不写入策划 JSON；百分比仅表示当前上传请求已发送的字节。 */
function setUploadStage(job: UploadJob, stage: UploadStage) {
  job.stage = stage
  job.status.textContent = t(`projects.uploadStage.${stage}`)
  job.progress.hidden = stage !== 'original' && stage !== 'working' && stage !== 'file'
  job.progress.removeAttribute('value')
  job.retry.hidden = stage !== 'failed'
  job.remove.hidden = stage !== 'failed'
}
function createUploadJob(file: File): UploadJob {
  const element = document.createElement('div'); element.className = 'project-upload-placeholder'; element.contentEditable = 'false'
  const previewURL = /^image\/(png|jpeg|webp|gif)$/.test(file.type) ? URL.createObjectURL(file) : ''
  if (previewURL) { const img = document.createElement('img'); img.src = previewURL; img.alt = ''; element.append(img) }
  const content = document.createElement('div'); content.className = 'project-upload-content'
  const name = document.createElement('strong'); name.className = 'project-upload-name'; name.textContent = file.name
  const status = document.createElement('span'); status.className = 'project-upload-status'; status.setAttribute('role', 'status')
  const progress = document.createElement('progress'); progress.className = 'project-upload-progress'; progress.max = 100; progress.setAttribute('aria-label', t('projects.uploadProgress'))
  const actions = document.createElement('div'); actions.className = 'project-upload-actions'
  const retry = document.createElement('button'); retry.type = 'button'; retry.textContent = t('common.retry')
  const remove = document.createElement('button'); remove.type = 'button'; remove.textContent = t('projects.removeBlock')
  actions.append(retry, remove); content.append(name, status, progress, actions); element.append(content)
  let complete = () => {}
  const done = new Promise<void>(resolve => { complete = resolve })
  const id = typeof crypto.randomUUID === 'function' ? crypto.randomUUID() : `upload-${Date.now()}-${Math.random().toString(36).slice(2)}`
  const job: UploadJob = { id, file, scope: uploadScope(), projectID: projectID.value, element, status, progress, retry, remove, previewURL, stage: 'queued', busy: false, done, complete }
  retry.addEventListener('click', () => { void runUpload(job) })
  remove.addEventListener('click', () => discardUpload(job))
  setUploadStage(job, 'queued')
  return job
}
/** 逐文件执行原有上传链，完成时仅替换对应占位块；失败保留文件供用户重试或移除。 */
async function runUpload(job: UploadJob) {
  if (job.busy || !uploadJobs.has(job.id)) return
  job.busy = true; uploadingAttachments.value += 1
  try {
    checkUploadScope(job)
    const extension = extOf(job.file.name)
    const report = (loaded: number, total: number) => {
      if (total > 0) { const value = Math.min(100, Math.floor(loaded * 100 / total)); job.progress.value = value; job.stage = value === 100 ? 'waiting' : 'sending'; job.status.textContent = value === 100 ? t('projects.uploadStage.waiting') : `${t('projects.uploadStage.sending')} ${value}%` }
    }
    let content: { type: string; attrs: Record<string, unknown> }
    if (imageExtensions.has(extension)) {
      const inspiration = await createProjectInspiration(job.file, { stage: stage => setUploadStage(job, stage), progress: report, checkScope: () => checkUploadScope(job) })
      checkUploadScope(job)
      content = { type: 'inspirationImage', attrs: { src: `inspiration://${inspiration.business_id}`, width: 100, layout: 'original' } }
    } else {
      setUploadStage(job, 'file')
      const uploaded = await api.uploadWithProgress(job.file, job.file.name, report)
      checkUploadScope(job)
      content = { type: 'projectAttachment', attrs: { storekey: uploaded.storekey, fileName: job.file.name, contentType: job.file.type || mimeByExtension[extension] || 'application/octet-stream', fileSize: job.file.size, width: 100, layout: 'original' } }
    }
    if (!uploadJobs.has(job.id)) return
    if (!editor.value || !replaceUploadPlaceholder(editor.value, job.id, content)) throw new Error('placeholder_missing')
    uploadJobs.delete(job.id)
    job.complete()
    if (job.previewURL) URL.revokeObjectURL(job.previewURL)
    // 附件预览依赖项目关联关系，插入后及时保存正文快照。
    if (content.type === 'projectAttachment') await saveNow()
  } catch {
    if (uploadJobs.has(job.id) && editor.value && hasUploadPlaceholder(editor.value, job.id)) setUploadStage(job, 'failed')
    else { uploadJobs.delete(job.id); job.complete(); if (job.previewURL) URL.revokeObjectURL(job.previewURL) }
  }
  finally { job.busy = false; uploadingAttachments.value -= 1 }
}

/** 所有有效文件立即产生各自的占位块，随后依次上传，保持粘贴顺序。 */
async function importFiles(files: File[]) {
  if (!editor.value || readOnly.value) return
  const jobs: UploadJob[] = []
  for (const source of files) {
    const suffix = clipboardImageExtensions[source.type]
    const file = !imageExtensions.has(extOf(source.name)) && !supportedExtensions.has(extOf(source.name)) && suffix && (!source.name || !source.name.includes('.'))
      ? new File([source], `${source.name || 'clipboard'}.${suffix}`, { type: source.type, lastModified: source.lastModified }) : source
    const extension = extOf(file.name)
    if (!imageExtensions.has(extension) && !supportedExtensions.has(extension)) { ElMessage.error(t('projects.fileTypeError')); continue }
    if (file.size > maxUploadBytes.value) { ElMessage.error(t('projects.fileTooLarge')); continue }
    const job = createUploadJob(file); uploadJobs.set(job.id, job); jobs.push(job)
  }
  if (!jobs.length) return
  addUploadPlaceholders(editor.value, jobs.map(job => ({ id: job.id, element: job.element, onDiscard: () => {
    // 编辑正文删除占位位置时，释放内存并阻止迟到结果重新插入。
    if (!uploadJobs.has(job.id)) return
    uploadJobs.delete(job.id); job.complete(); if (job.previewURL) URL.revokeObjectURL(job.previewURL)
  } })))
  for (const job of jobs) { await runUpload(job); await job.done }
}

const clipboardImageExtensions: Record<string, string> = { 'image/png': 'png', 'image/jpeg': 'jpg', 'image/webp': 'webp', 'image/heic': 'heic', 'image/heif': 'heif', 'image/gif': 'gif' }

const mimeByExtension: Record<string, string> = {
  gif: 'image/gif', mp4: 'video/mp4', webm: 'video/webm', ogv: 'video/ogg', pdf: 'application/pdf',
  doc: 'application/msword', docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  xls: 'application/vnd.ms-excel', xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
}

const saveLabel = computed(() => ({ saved: t('projects.saved'), saving: t('projects.saving'), unsaved: t('projects.unsaved'), error: t('projects.saveError') })[savingState.value])

// 上传块不进入 Vue 模板，切换语言时同步刷新其可见文案。
watch(locale, () => {
  for (const job of uploadJobs.values()) {
    job.retry.textContent = t('common.retry'); job.remove.textContent = t('projects.removeBlock')
    job.progress.setAttribute('aria-label', t('projects.uploadProgress'))
    job.status.textContent = job.stage === 'sending' ? `${t('projects.uploadStage.sending')} ${job.progress.value}%` : t(`projects.uploadStage.${job.stage}`)
  }
})

// 同步观察使加载期间的标题赋值仍受 hydrating 保护，打开旧文档不会触发无意义保存。
watch(title, () => scheduleSave(), { flush: 'sync' })
watch(projectID, () => { if (projectID.value) void loadProject() })
function warnPendingUploads(event: BeforeUnloadEvent) { if (uploadJobs.size) { event.preventDefault(); event.returnValue = '' } }
onMounted(() => { window.addEventListener('beforeunload', warnPendingUploads); void loadProject(); void loadUploadLimit() })
onBeforeUnmount(() => { window.removeEventListener('beforeunload', warnPendingUploads); if (saveTimer) window.clearTimeout(saveTimer); for (const job of uploadJobs.values()) { job.complete(); if (job.previewURL) URL.revokeObjectURL(job.previewURL) }
  uploadJobs.clear() })
onBeforeRouteLeave(async () => {
  // 整条导入链包括解码与多次上传，不能只等待单个 HTTP 请求。
  if (documentIncompatible.value) return true
  if (uploadingAttachments.value > 0 || uploadJobs.size > 0 || loading.value) { ElMessage.warning(t('projects.uploadPendingLeave')); return false }
  while (saveTimer || savingState.value === 'unsaved' || savingState.value === 'saving' || savingState.value === 'error') {
    if (saveFlight) await saveFlight
    else if (!await saveNow()) return false
    if (savingState.value === 'error') return false
  }
  return true
})
</script>

<template>
  <div class="project-app-header"><AppHeader /></div>
  <main class="project-editor-main" :class="{ 'is-loading': loading }">
    <div v-if="loading" class="project-editor-state">{{ t('common.loading') }}</div>
    <div v-else-if="loadFailed || !project" class="project-editor-state project-editor-error"><p>{{ t('projects.loadError') }}</p><el-button @click="loadProject">{{ t('common.retry') }}</el-button></div>
    <template v-else>
      <header class="project-editor-header">
        <el-button class="project-back" text :icon="ArrowLeft" @click="router.push('/projects')">{{ t('projects.back') }}</el-button>
        <div class="project-title-area">
          <ProjectCoverImage :storekey="coverImageKey" :emoji="coverEmoji" :label="title" />
          <el-input v-model="title" :disabled="documentIncompatible || readOnly" class="project-title-input" maxlength="120" :placeholder="t('projects.editorTitlePlaceholder')" />
        </div>
        <el-button :disabled="documentIncompatible || uploadingAttachments > 0 || uploadJobs.size > 0" @click="switchMode">{{ readOnly ? t('projects.switchToEdit') : t('projects.switchToRead') }}</el-button>
        <span class="project-save-status" :class="`is-${savingState}`" role="status">{{ saveLabel }}</span>
      </header>

      <ProjectDocumentEditor
        :key="projectID"
        :document="initialDocument"
        :read-only="readOnly"
        @ready="editorReady"
        @incompatible="documentIncompatible = true"
        @change="scheduleSave"
        @insert-inspiration="pickerOpen = true"
        @upload-attachment="attachmentInput?.click()"
        @upload-media="mediaInput?.click()"
        @paste-files="importFiles"
      />
      <input ref="mediaInput" class="hidden-file-input" type="file" multiple accept="image/*,video/*" @change="uploadFiles" />
      <input ref="attachmentInput" class="hidden-file-input" type="file" multiple accept=".pdf,.doc,.docx,.xls,.xlsx" @change="uploadFiles" />
    </template>
  </main>
  <ProjectInspirationPicker v-model="pickerOpen" @select="insertInspiration" />
</template>

<style scoped>
/* 界面表面与控件使用全局主题令牌；图片、文档和用户自选封面颜色保持原数据。 */
.project-editor-main { width: min(calc(100vw - 310px), 1120px); min-height: 100svh; margin-left: calc(226px + max(0px, (100vw - 1346px) / 2)); padding: 25px 0 90px; }
.project-editor-main.is-loading { display: grid; place-items: center; }
.project-editor-header { display: flex; align-items: center; justify-content: space-between; gap: 14px; margin-bottom: 16px; }
.project-back { flex: 0 0 auto; padding-left: 0; color: var(--muted); }
.project-title-area { display: flex; min-width: 0; flex: 1; align-items: center; gap: 12px; }
.project-title-area :deep(.project-cover-image) { width: 48px; height: 48px; flex-basis: 48px; font-size: 26px; }
.project-title-input :deep(.el-input__wrapper) { padding: 0; background: transparent; box-shadow: none; }
.project-title-input :deep(input) { height: 44px; color: var(--ink); font-size: clamp(20px, 3vw, 28px); font-weight: 700; }
.project-save-status { flex: 0 0 auto; color: var(--muted); font-size: 11px; }
.project-save-status.is-error { color: var(--danger); }
.project-save-status.is-saved { color: var(--success); }
.hidden-file-input { display: none; }
.project-editor-state { display: grid; min-height: 60vh; justify-items: center; align-content: center; gap: 12px; color: var(--muted); text-align: center; }
.project-editor-error { color: var(--danger); }
@media (max-width: 768px) {
  .project-app-header { display: none; }
  .project-editor-main { width: 100%; margin: 0; padding: 16px 12px calc(94px + env(safe-area-inset-bottom)); }
  .project-editor-header { flex-wrap: wrap; gap: 5px 10px; margin-bottom: 11px; }
  .project-back { order: 0; width: 100%; justify-content: flex-start; font-size: 11px; }
  .project-title-area { order: 1; }
  .project-title-area :deep(.project-cover-image) { width: 38px; height: 38px; flex-basis: 38px; font-size: 22px; }
  .project-title-input :deep(input) { height: 38px; font-size: 20px; }
  .project-save-status { order: 2; margin-left: auto; font-size: 9px; }
}
</style>
