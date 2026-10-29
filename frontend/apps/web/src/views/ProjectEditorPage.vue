<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import type { Editor } from '@tiptap/core'
import { ElMessage } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import ProjectDocumentEditor from '../projects/views/ProjectDocumentEditor.vue'
import { useI18n } from 'vue-i18n'
import AppHeader from '../components/AppHeader.vue'
import ProjectCoverImage from '../components/ProjectCoverImage.vue'
import ProjectInspirationPicker from '../projects/views/ProjectInspirationPicker.vue'
import { api } from '../api/client'
import type { InspirationRecord, ProjectRecord, ProjectSaveInput } from '../api/types'
import { createProjectInspiration } from '../projects/createProjectInspiration'
import { fallbackProjectUploadLimit, projectUploadLimit } from '../projects/projectUploadLimit'

const route = useRoute()
const router = useRouter()
const { t } = useI18n({ useScope: 'global' })
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
const mediaInput = ref<HTMLInputElement | null>(null)
const attachmentInput = ref<HTMLInputElement | null>(null)
const maxUploadBytes = ref(fallbackProjectUploadLimit)
let saveTimer: number | undefined
let changeVersion = 0
let saveFlight: Promise<boolean> | null = null
let hydrating = true
let uploadingAttachments = 0

// 仅在加载项目时更新初始正文，保存响应不能覆盖请求期间产生的新编辑。
const initialDocument = shallowRef<Record<string, unknown>>({ type: 'doc', content: [{ type: 'paragraph' }] })
const editor = shallowRef<Editor>()

/** 编辑器实例由开源组件创建；接入页面唯一的保存管线，正文由组件按初始 JSON 恢复。 */
function editorReady(instance: Editor) {
  editor.value = instance
}

function saveInput(): ProjectSaveInput {
  return {
    title: title.value.trim(), cover_emoji: coverEmoji.value, cover_image_key: coverImageKey.value,
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

/** 剪贴板和文件选择共用校验与上传流程；无扩展名的剪贴板图片按 MIME 补全文件名。 */
async function importFiles(files: File[]) {
  uploadingAttachments += 1
  try {
  let hasNewAttachments = false
  for (const source of files) {
    const suffix = clipboardImageExtensions[source.type]
    const file = !imageExtensions.has(extOf(source.name)) && !supportedExtensions.has(extOf(source.name)) && suffix && (!source.name || !source.name.includes('.'))
      ? new File([source], `${source.name || 'clipboard'}.${suffix}`, { type: source.type, lastModified: source.lastModified }) : source
    const extension = extOf(file.name)
    if (!imageExtensions.has(extension) && !supportedExtensions.has(extension)) { ElMessage.error(t('projects.fileTypeError')); continue }
    if (file.size > maxUploadBytes.value) { ElMessage.error(t('projects.fileTooLarge')); continue }
    try {
      if (imageExtensions.has(extension)) {
        const inspiration = await createProjectInspiration(file)
        insertInspiration(inspiration)
        continue
      }
      const uploaded = await api.upload(file, file.name)
      const contentType = file.type || mimeByExtension[extension] || 'application/octet-stream'
      editor.value?.chain().focus().insertContent({
        type: 'projectAttachment', attrs: { storekey: uploaded.storekey, fileName: file.name, contentType, fileSize: file.size, width: 100, layout: 'original' },
      }).run()
      hasNewAttachments = true
    } catch { ElMessage.error(t('projects.saveError')) }
  }
  // 新附件需要先进入项目关联清单，之后项目授权预览接口才能读取；此处提前提交一次正文快照。
  if (hasNewAttachments) await saveNow()
  } finally { uploadingAttachments -= 1 }
}

const clipboardImageExtensions: Record<string, string> = { 'image/png': 'png', 'image/jpeg': 'jpg', 'image/webp': 'webp', 'image/heic': 'heic', 'image/heif': 'heif', 'image/gif': 'gif' }

const mimeByExtension: Record<string, string> = {
  gif: 'image/gif', mp4: 'video/mp4', webm: 'video/webm', ogv: 'video/ogg', pdf: 'application/pdf',
  doc: 'application/msword', docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  xls: 'application/vnd.ms-excel', xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
}

const saveLabel = computed(() => ({ saved: t('projects.saved'), saving: t('projects.saving'), unsaved: t('projects.unsaved'), error: t('projects.saveError') })[savingState.value])

// 同步观察使加载期间的标题赋值仍受 hydrating 保护，打开旧文档不会触发无意义保存。
watch(title, () => scheduleSave(), { flush: 'sync' })
watch(projectID, () => { if (projectID.value) void loadProject() })
onMounted(() => { void loadProject(); void loadUploadLimit() })
onBeforeUnmount(() => { if (saveTimer) window.clearTimeout(saveTimer) })
onBeforeRouteLeave(async () => {
  // 整条导入链包括解码与多次上传，不能只等待单个 HTTP 请求。
  if (documentIncompatible.value) return true
  if (uploadingAttachments > 0 || loading.value) return false
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
          <el-input v-model="title" :disabled="documentIncompatible" class="project-title-input" maxlength="120" :placeholder="t('projects.editorTitlePlaceholder')" />
        </div>
        <span class="project-save-status" :class="`is-${savingState}`" role="status">{{ saveLabel }}</span>
      </header>

      <ProjectDocumentEditor
        :key="projectID"
        :document="initialDocument"
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
