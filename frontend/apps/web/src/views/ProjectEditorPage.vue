<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { EditorContent, useEditor } from '@tiptap/vue-3'
import StarterKit from '@tiptap/starter-kit'
import { BackgroundColor, TextStyle } from '@tiptap/extension-text-style'
import Color from '@tiptap/extension-color'
import Highlight from '@tiptap/extension-highlight'
import TextAlign from '@tiptap/extension-text-align'
import TaskList from '@tiptap/extension-task-list'
import TaskItem from '@tiptap/extension-task-item'
import { Table } from '@tiptap/extension-table'
import { TableRow } from '@tiptap/extension-table-row'
import { TableHeader } from '@tiptap/extension-table-header'
import { TableCell } from '@tiptap/extension-table-cell'
import Placeholder from '@tiptap/extension-placeholder'
import { ElMessage } from 'element-plus'
import {
  ArrowLeft, Brush, BrushFilled, ChatLineSquare, Document, Finished, Grid, Link,
  List, Memo, Menu, Minus, Monitor, Operation, Paperclip, Picture, RefreshLeft, RefreshRight,
  Right, Sort,
} from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import AppHeader from '../components/AppHeader.vue'
import ProjectCoverImage from '../components/ProjectCoverImage.vue'
import ProjectInspirationPicker from '../projects/views/ProjectInspirationPicker.vue'
import { FontSize, InspirationImage, ProjectAttachment } from '../projects/projectExtensions'
import { api } from '../api/client'
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
const attachmentInput = ref<HTMLInputElement | null>(null)
const fontSize = ref('16px')
const textColor = ref('#2d3433')
const backgroundColor = ref('#fff1b8')
const maxUploadBytes = ref(fallbackProjectUploadLimit)
let saveTimer: number | undefined
let changeVersion = 0
let saveFlight: Promise<boolean> | null = null
let hydrating = true
let uploadingAttachments = 0

const editor = useEditor({
  // 段落对齐作用于段落和标题；任务清单依赖独立的列表与任务项扩展。
  extensions: [
    StarterKit.configure({ link: { openOnClick: false } }),
    TextStyle, Color, BackgroundColor, Highlight.configure({ multicolor: true }),
    TextAlign.configure({ types: ['heading', 'paragraph'] }),
    TaskList, TaskItem.configure({ nested: true }),
    Table.configure({ resizable: true }), TableRow, TableHeader, TableCell,
    Placeholder.configure({ placeholder: () => t('projects.editorPlaceholder') }),
    FontSize, InspirationImage, ProjectAttachment,
  ],
  content: { type: 'doc', content: [{ type: 'paragraph' }] },
  editorProps: { attributes: { class: 'project-prosemirror' } },
  onUpdate: () => scheduleSave(),
})

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
  if (hydrating || !project.value) return
  changeVersion += 1
  savingState.value = 'unsaved'
  if (saveTimer) window.clearTimeout(saveTimer)
  saveTimer = window.setTimeout(() => { void saveNow() }, 750)
}

async function saveNow(): Promise<boolean> {
  if (!project.value) return false
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
  hydrating = true
  try {
    const result = await api.project(projectID.value)
    project.value = result
    title.value = result.title
    coverEmoji.value = result.cover_emoji
    coverImageKey.value = result.cover_image_key
    coverFileName.value = result.cover_file_name
    editor.value?.commands.setContent(result.document, { emitUpdate: false })
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
  uploadingAttachments += 1
  try {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  let hasNewAttachments = false
  input.value = ''
  for (const file of files) {
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

const mimeByExtension: Record<string, string> = {
  gif: 'image/gif', mp4: 'video/mp4', webm: 'video/webm', ogv: 'video/ogg', pdf: 'application/pdf',
  doc: 'application/msword', docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  xls: 'application/vnd.ms-excel', xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
}

function setTextSize() { editor.value?.chain().focus().setMark('textStyle', { fontSize: fontSize.value }).run() }
function setTextColor(event: Event) { textColor.value = (event.target as HTMLInputElement).value; editor.value?.chain().focus().setColor(textColor.value).run() }
function setBackgroundColor(event: Event) { backgroundColor.value = (event.target as HTMLInputElement).value; editor.value?.chain().focus().setBackgroundColor(backgroundColor.value).run() }
function addLink() {
  const value = window.prompt(t('projects.linkPrompt'))?.trim()
  if (!value) return
  if (!/^https?:\/\//i.test(value) && !/^mailto:/i.test(value)) { ElMessage.error(t('projects.linkUrlInvalid')); return }
  const currentEditor = editor.value
  if (!currentEditor) return
  if (currentEditor.state.selection.empty) {
    currentEditor.chain().focus().insertContent({ type: 'text', text: value, marks: [{ type: 'link', attrs: { href: value } }] }).run()
    return
  }
  currentEditor.chain().focus().extendMarkRange('link').setLink({ href: value }).run()
}
function toggleBlock(name: 'paragraph' | 'heading' | 'bulletList' | 'orderedList' | 'taskList' | 'blockquote' | 'codeBlock') {
  const current = editor.value?.chain().focus()
  if (!current) return
  if (name === 'heading') current.toggleHeading({ level: 2 }).run()
  else if (name === 'paragraph') current.setParagraph().run()
  else if (name === 'bulletList') current.toggleBulletList().run()
  else if (name === 'orderedList') current.toggleOrderedList().run()
  else if (name === 'taskList') current.toggleTaskList().run()
  else if (name === 'blockquote') current.toggleBlockquote().run()
  else current.toggleCodeBlock().run()
}

const saveLabel = computed(() => ({ saved: t('projects.saved'), saving: t('projects.saving'), unsaved: t('projects.unsaved'), error: t('projects.saveError') })[savingState.value])

watch(title, () => scheduleSave())
watch(locale, () => { editor.value?.setOptions({ editorProps: { attributes: { class: 'project-prosemirror' } } }) })
watch(projectID, () => { if (projectID.value) void loadProject() })
onMounted(() => { void loadProject(); void loadUploadLimit() })
onBeforeUnmount(() => { if (saveTimer) window.clearTimeout(saveTimer) })
onBeforeRouteLeave(async () => {
  // 整条导入链包括解码与多次上传，不能只等待单个 HTTP 请求。
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
  <AppHeader />
  <main class="project-editor-main" :class="{ 'is-loading': loading }">
    <div v-if="loading" class="project-editor-state">{{ t('common.loading') }}</div>
    <div v-else-if="loadFailed || !project" class="project-editor-state project-editor-error"><p>{{ t('projects.loadError') }}</p><el-button @click="loadProject">{{ t('common.retry') }}</el-button></div>
    <template v-else>
      <header class="project-editor-header">
        <el-button class="project-back" text :icon="ArrowLeft" @click="router.push('/projects')">{{ t('projects.back') }}</el-button>
        <div class="project-title-area">
          <ProjectCoverImage :storekey="coverImageKey" :emoji="coverEmoji" :label="title" />
          <el-input v-model="title" class="project-title-input" maxlength="120" :placeholder="t('projects.editorTitlePlaceholder')" />
        </div>
        <span class="project-save-status" :class="`is-${savingState}`" role="status">{{ saveLabel }}</span>
      </header>

      <div class="project-toolbar" role="toolbar" :aria-label="t('projects.title')">
        <el-button-group>
          <el-tooltip :content="t('projects.toolbar.undo')" placement="bottom"><el-button size="small" :aria-label="t('projects.toolbar.undo')" :disabled="!editor?.can().undo()" @click="editor?.chain().focus().undo().run()"><el-icon><RefreshLeft /></el-icon></el-button></el-tooltip>
          <el-tooltip :content="t('projects.toolbar.redo')" placement="bottom"><el-button size="small" :aria-label="t('projects.toolbar.redo')" :disabled="!editor?.can().redo()" @click="editor?.chain().focus().redo().run()"><el-icon><RefreshRight /></el-icon></el-button></el-tooltip>
        </el-button-group>
        <el-select v-model="fontSize" class="toolbar-font-size" size="small" :aria-label="t('projects.toolbar.fontSize')" @change="setTextSize">
          <el-option v-for="size in ['12px','14px','16px','18px','20px','24px','30px','36px','48px']" :key="size" :label="size" :value="size" />
        </el-select>
        <el-button-group>
          <el-tooltip :content="t('projects.toolbar.bold')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive('bold') }" :aria-label="t('projects.toolbar.bold')" @click="editor?.chain().focus().toggleBold().run()"><strong>B</strong></el-button></el-tooltip>
          <el-tooltip :content="t('projects.toolbar.italic')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive('italic') }" :aria-label="t('projects.toolbar.italic')" @click="editor?.chain().focus().toggleItalic().run()"><i>I</i></el-button></el-tooltip>
          <el-tooltip :content="t('projects.toolbar.underline')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive('underline') }" :aria-label="t('projects.toolbar.underline')" @click="editor?.chain().focus().toggleUnderline().run()"><u>U</u></el-button></el-tooltip>
          <el-tooltip :content="t('projects.toolbar.strike')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive('strike') }" :aria-label="t('projects.toolbar.strike')" @click="editor?.chain().focus().toggleStrike().run()"><s>S</s></el-button></el-tooltip>
        </el-button-group>
        <el-tooltip :content="t('projects.toolbar.textColor')" placement="bottom"><label class="toolbar-color"><el-icon aria-hidden="true"><Brush /></el-icon><input type="color" :value="textColor" :aria-label="t('projects.toolbar.textColor')" @input="setTextColor" /></label></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.backgroundColor')" placement="bottom"><label class="toolbar-color toolbar-background-color"><el-icon aria-hidden="true"><BrushFilled /></el-icon><input type="color" :value="backgroundColor" :aria-label="t('projects.toolbar.backgroundColor')" @input="setBackgroundColor" /></label></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.highlight')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive('highlight') }" :aria-label="t('projects.toolbar.highlight')" @click="editor?.chain().focus().toggleHighlight({ color: '#fde68a' }).run()"><el-icon><BrushFilled /></el-icon></el-button></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.link')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive('link') }" :aria-label="t('projects.toolbar.link')" @click="addLink"><el-icon><Link /></el-icon></el-button></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.paragraph')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive('paragraph') }" :aria-label="t('projects.toolbar.paragraph')" @click="toggleBlock('paragraph')"><el-icon><Document /></el-icon></el-button></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.heading')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive('heading') }" :aria-label="t('projects.toolbar.heading')" @click="toggleBlock('heading')"><el-icon><Memo /></el-icon></el-button></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.bulletList')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive('bulletList') }" :aria-label="t('projects.toolbar.bulletList')" @click="toggleBlock('bulletList')"><el-icon><List /></el-icon></el-button></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.orderedList')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive('orderedList') }" :aria-label="t('projects.toolbar.orderedList')" @click="toggleBlock('orderedList')"><el-icon><Sort /></el-icon></el-button></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.taskList')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive('taskList') }" :aria-label="t('projects.toolbar.taskList')" @click="toggleBlock('taskList')"><el-icon><Finished /></el-icon></el-button></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.blockquote')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive('blockquote') }" :aria-label="t('projects.toolbar.blockquote')" @click="toggleBlock('blockquote')"><el-icon><ChatLineSquare /></el-icon></el-button></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.codeBlock')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive('codeBlock') }" :aria-label="t('projects.toolbar.codeBlock')" @click="toggleBlock('codeBlock')"><el-icon><Monitor /></el-icon></el-button></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.divider')" placement="bottom"><el-button size="small" :aria-label="t('projects.toolbar.divider')" @click="editor?.chain().focus().setHorizontalRule().run()"><el-icon><Minus /></el-icon></el-button></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.table')" placement="bottom"><el-button size="small" :aria-label="t('projects.toolbar.table')" @click="editor?.chain().focus().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run()"><el-icon><Grid /></el-icon></el-button></el-tooltip>
        <el-button-group>
          <el-tooltip :content="t('projects.toolbar.alignLeft')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive({ textAlign: 'left' }) }" :aria-label="t('projects.toolbar.alignLeft')" @click="editor?.chain().focus().setTextAlign('left').run()"><el-icon><Menu /></el-icon></el-button></el-tooltip>
          <el-tooltip :content="t('projects.toolbar.alignCenter')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive({ textAlign: 'center' }) }" :aria-label="t('projects.toolbar.alignCenter')" @click="editor?.chain().focus().setTextAlign('center').run()"><el-icon><Operation /></el-icon></el-button></el-tooltip>
          <el-tooltip :content="t('projects.toolbar.alignRight')" placement="bottom"><el-button size="small" :class="{ 'is-active': editor?.isActive({ textAlign: 'right' }) }" :aria-label="t('projects.toolbar.alignRight')" @click="editor?.chain().focus().setTextAlign('right').run()"><el-icon><Right /></el-icon></el-button></el-tooltip>
        </el-button-group>
        <el-tooltip :content="t('projects.toolbar.insertInspiration')" placement="bottom"><el-button size="small" :aria-label="t('projects.toolbar.insertInspiration')" @click="pickerOpen = true"><el-icon><Picture /></el-icon></el-button></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.uploadAttachment')" placement="bottom"><el-button size="small" :aria-label="t('projects.toolbar.uploadAttachment')" @click="attachmentInput?.click()"><el-icon><Paperclip /></el-icon></el-button></el-tooltip>
        <input ref="attachmentInput" class="hidden-file-input" type="file" multiple accept="image/*,.gif,.mp4,.webm,.ogv,.pdf,.doc,.docx,.xls,.xlsx" @change="uploadFiles" />
      </div>

      <section class="project-editor-paper">
        <EditorContent :editor="editor" />
      </section>
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
.project-toolbar { position: sticky; top: 0; z-index: 8; display: flex; min-width: 0; align-items: center; gap: 5px; overflow-x: auto; padding: 8px; border: 1px solid var(--line); border-radius: var(--radius-control); background: var(--surface); box-shadow: var(--soft-shadow); scrollbar-width: thin; }
.project-toolbar :deep(.el-button) { min-width: 40px; min-height: 40px; padding: 6px 8px; }
.project-toolbar :deep(.el-button.is-active) { border-color: var(--accent); background: var(--accent-soft); color: var(--accent-hover); }
/* 工具组保持自身宽度，由外层横向滚动承载，避免触控目标被 flex 压缩。 */
.project-toolbar > * { flex-shrink: 0; }
.project-toolbar :deep(.el-icon) { font-size: 18px; }
.toolbar-font-size { width: 80px; flex: 0 0 80px; }
.toolbar-color { position: relative; display: grid; width: 40px; height: 40px; flex: 0 0 40px; place-items: center; border: 1px solid var(--line); border-radius: var(--radius-control); color: var(--ink); cursor: pointer; }
.toolbar-color input { position: absolute; width: 1px; height: 1px; opacity: 0; }
.toolbar-color:focus-within { outline: 3px solid var(--focus); outline-offset: 2px; }
.hidden-file-input { display: none; }
.project-editor-paper { min-height: 65vh; margin-top: 18px; padding: 36px clamp(20px, 5vw, 72px) 70px; border: 1px solid var(--line); border-radius: var(--radius-card); background: var(--surface); box-shadow: var(--soft-shadow); }
.project-editor-paper :deep(.ProseMirror) { min-height: 55vh; outline: none; color: var(--ink); font-size: 16px; line-height: 1.8; overflow-wrap: anywhere; }
.project-editor-paper :deep(.ProseMirror em) { font-style: italic; }
.project-editor-paper :deep(.ProseMirror em:lang(zh)) { display: inline-block; transform: skewX(-12deg); }
.project-editor-paper :deep(.ProseMirror .project-resource-node) { display: block; }
.project-editor-paper :deep(.ProseMirror .ProseMirror-selectednode) { outline: 2px solid var(--accent); outline-offset: 3px; }
.project-editor-paper :deep(.ProseMirror .ProseMirror-dropcursor) { border-color: var(--accent); border-width: 3px; }
.project-editor-paper :deep(.ProseMirror > * + *) { margin-top: .7em; }
.project-editor-paper :deep(.ProseMirror h1) { font-size: 2em; line-height: 1.25; }
.project-editor-paper :deep(.ProseMirror h2) { font-size: 1.55em; line-height: 1.3; }
.project-editor-paper :deep(.ProseMirror h3) { font-size: 1.25em; line-height: 1.4; }
.project-editor-paper :deep(.ProseMirror blockquote) { margin: 1em 0; padding: 4px 16px; border-left: 3px solid var(--accent); color: var(--muted); }
.project-editor-paper :deep(.ProseMirror pre) { overflow: auto; padding: 14px; border-radius: var(--radius-control); background: var(--surface-muted); font-size: 13px; }
.project-editor-paper :deep(.ProseMirror table) { width: 100%; border-collapse: collapse; table-layout: fixed; }
.project-editor-paper :deep(.ProseMirror td), .project-editor-paper :deep(.ProseMirror th) { min-width: 80px; padding: 7px; border: 1px solid var(--line); vertical-align: top; }
.project-editor-paper :deep(.ProseMirror th) { background: var(--surface-muted); text-align: left; }
.project-editor-paper :deep(.ProseMirror ul[data-type=taskList]) { padding-left: 0; list-style: none; }
.project-editor-paper :deep(.ProseMirror li[data-type=taskItem]) { display: flex; align-items: flex-start; gap: 8px; }
.project-editor-paper :deep(.ProseMirror li[data-type=taskItem] > label) { flex: 0 0 auto; margin: 0; padding-top: 5px; }
.project-editor-paper :deep(.ProseMirror li[data-type=taskItem] > div) { min-width: 0; flex: 1 1 auto; }
.project-editor-paper :deep(.ProseMirror li[data-type=taskItem] p) { margin: 0; }
.project-editor-paper :deep(.ProseMirror li[data-type=taskItem] input) { margin: 0; accent-color: var(--accent); }
.project-editor-paper :deep(.ProseMirror a) { color: var(--accent-hover); text-decoration: underline; }
.project-editor-paper :deep(.ProseMirror p.is-editor-empty:first-child::before) { float: left; height: 0; color: var(--muted); content: attr(data-placeholder); pointer-events: none; }
.project-editor-state { display: grid; min-height: 60vh; justify-items: center; align-content: center; gap: 12px; color: var(--muted); text-align: center; }
.project-editor-error { color: var(--danger); }
@media (max-width: 768px) {
  .project-editor-main { width: 100%; margin: 0; padding: 16px 12px calc(94px + env(safe-area-inset-bottom)); }
  .project-editor-header { flex-wrap: wrap; gap: 5px 10px; margin-bottom: 11px; }
  .project-back { order: 0; width: 100%; justify-content: flex-start; font-size: 11px; }
  .project-title-area { order: 1; }
  .project-title-area :deep(.project-cover-image) { width: 38px; height: 38px; flex-basis: 38px; font-size: 22px; }
  .project-title-input :deep(input) { height: 38px; font-size: 20px; }
  .project-save-status { order: 2; margin-left: auto; font-size: 9px; }
  .project-toolbar { position: fixed; inset: auto 0 0; z-index: 20; min-height: calc(58px + env(safe-area-inset-bottom)); gap: 5px; padding: 7px 9px calc(7px + env(safe-area-inset-bottom)); border-width: 1px 0 0; border-radius: 0; background: var(--surface); box-shadow: var(--shadow); }
  .project-toolbar :deep(.el-button) { min-width: 44px; min-height: 44px; padding: 5px 7px; }
  .toolbar-color { width: 44px; height: 44px; flex-basis: 44px; }
  .project-editor-paper { min-height: 65vh; margin-top: 10px; padding: 22px 16px 32px; }
  .project-editor-paper :deep(.ProseMirror) { min-height: 60vh; font-size: 15px; }
}
</style>
