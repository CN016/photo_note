<script setup lang="ts">
import ContentZoom from '../../components/ContentZoom.vue'
import { computed, markRaw, shallowRef, watch, onMounted, onBeforeUnmount, provide, ref } from 'vue'
import { Extension, getSchema, type Editor } from '@tiptap/core'
import { Plugin } from '@tiptap/pm/state'
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
import { DragHandle } from '@tiptap/extension-drag-handle-vue-3'
import { ProEditorElementPlus, type ToolbarConfig } from 'tiptap-vue-pro-element-plus'
import 'tiptap-vue-pro-element-plus/style.css'
import { Paperclip, Picture, Rank, UploadFilled } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { isDocumentCompatible } from '../documentCompatibility'
import { FontSize, InspirationImage, ProjectAttachment } from '../projectExtensions'
import { useColorTheme } from '../../theme'
import ProjectImageViewer from './ProjectImageViewer.vue'
import { projectReadContext } from '../projectReadContext'
import { ProjectUploadPlaceholders } from '../uploadPlaceholders'

/** 项目正文适配层：开源组件负责编辑交互，资源上传和持久化始终交回业务页面。 */
const props = defineProps<{ document: Record<string, unknown>; readOnly: boolean }>()
const emit = defineEmits<{ ready: [editor: Editor]; incompatible: []; change: []; insertInspiration: []; uploadAttachment: []; uploadMedia: []; pasteFiles: [files: File[]] }>()
const { t, locale } = useI18n({ useScope: 'global' })
const theme = useColorTheme()
const editor = shallowRef<Editor>()
const viewerPosition = ref<number | null>(null)
// 节点视图通过注入打开同一策划的图片浏览器，位置区分重复引用的图片。
provide(projectReadContext, { readOnly: computed(() => props.readOnly), openImage: position => { viewerPosition.value = position } })
const language = computed(() => locale.value === 'en-US' ? 'en-US' : 'zh-CN')
const toolbar: ToolbarConfig = [
  ['undo', 'redo'], ['heading', 'fontSize'], ['bold', 'italic', 'underline', 'strike', 'color', 'highlight'],
  ['align', 'bulletList', 'orderedList', 'taskList'], ['blockquote', 'codeBlock', 'hr', 'link', 'table', 'clearFormat'],
]
// 只注册 V1 正文已经支持的节点；覆盖组件默认集合，禁止绕过私有资源接口插入 image/file URL。
const extensions = markRaw([
  StarterKit.configure({ link: { openOnClick: false } }),
  TextStyle, Color, BackgroundColor, Highlight.configure({ multicolor: true }),
  TextAlign.configure({ types: ['heading', 'paragraph'] }),
  TaskList, TaskItem.configure({ nested: true, a11y: { checkboxLabel: node => node.textContent || t('projects.toolbar.taskList') } }),
  Table.configure({ resizable: true }), TableRow, TableHeader, TableCell,
  Placeholder.configure({ placeholder: () => t('projects.editorPlaceholder') }),
  FontSize, InspirationImage, ProjectAttachment, ProjectUploadPlaceholders,
  Extension.create({
    name: 'projectEditorBridge',
    addProseMirrorPlugins() {
      return [new Plugin({ props: { handlePaste: (_view, event) => {
        // 只接管剪贴板真实文件，文本和编辑器内部内容继续走原生粘贴管线。
        if (props.readOnly) return false
        const files = Array.from(event.clipboardData?.files ?? [])
        if (!files.length) return false
        emit('pasteFiles', files)
        return true
      } } })]
    },
    onCreate() {
      editor.value = this.editor
      emit('ready', this.editor)
    },
  }),
])
/** 未知节点或属性不能被编辑器静默删掉；阻止挂载与保存，保留服务端原文。 */
const schema = getSchema(extensions)
const compatible = computed(() => isDocumentCompatible(props.document, schema))
watch(compatible, valid => { if (!valid) emit('incompatible') }, { immediate: true })
/** 可视视口收缩时让底部工具栏跟随键盘，并把正在输入的光标滚到工具栏上方。 */
let viewportFrame = 0
function updateViewport() {
  cancelAnimationFrame(viewportFrame)
  viewportFrame = requestAnimationFrame(() => {
    const viewport = window.visualViewport
    const bottom = viewport ? Math.max(0, window.innerHeight - viewport.height - viewport.offsetTop) : 0
    document.documentElement.style.setProperty('--project-keyboard-bottom', `${bottom}px`)
    if (!editor.value?.isFocused || !window.matchMedia('(max-width: 768px)').matches) return
    const rect = editor.value.view.coordsAtPos(editor.value.state.selection.head)
    const top = (viewport?.offsetTop ?? 0) + 20
    const limit = (viewport?.offsetTop ?? 0) + (viewport?.height ?? window.innerHeight) - 90
    if (rect.bottom > limit) window.scrollBy(0, rect.bottom - limit)
    else if (rect.top < top) window.scrollBy(0, rect.top - top)
  })
}
onMounted(() => {
  window.visualViewport?.addEventListener('resize', updateViewport)
  window.visualViewport?.addEventListener('scroll', updateViewport)
  window.addEventListener('resize', updateViewport)
})
watch(editor, (value, previous) => {
  previous?.off('selectionUpdate', updateViewport)
  value?.on('selectionUpdate', updateViewport)
  value?.on('focus', updateViewport)
})
onBeforeUnmount(() => {
  cancelAnimationFrame(viewportFrame)
  window.visualViewport?.removeEventListener('resize', updateViewport)
  window.visualViewport?.removeEventListener('scroll', updateViewport)
  window.removeEventListener('resize', updateViewport)
  editor.value?.off('selectionUpdate', updateViewport)
  editor.value?.off('focus', updateViewport)
  document.documentElement.style.removeProperty('--project-keyboard-bottom')
})
</script>

<template>
  <ContentZoom selector=".tvp-content-wrap"><div class="project-document-editor" :class="{ 'is-readonly': props.readOnly }">
    <el-alert v-if="!compatible" :title="t('projects.documentIncompatible')" type="error" :closable="false" />
    <ProEditorElementPlus v-else :model-value="props.document" output="json" :extensions="extensions" :toolbar="toolbar"
      :toolbar-options="{ fontSizes: ['12px', '14px', '16px', '18px', '20px', '24px', '30px', '36px', '48px'] }"
      :editor-behavior-options="{ image: { allowUrl: false } }" :readonly="props.readOnly"
      :locale="language" :dark="theme === 'dark'" :autosave="false" :draft="false" :show-word-count="false"
      @update:model-value="emit('change')">
      <template #toolbar-before>
        <el-tooltip :content="t('projects.toolbar.uploadMedia')"><el-button :aria-label="t('projects.toolbar.uploadMedia')" text @click="emit('uploadMedia')"><el-icon><UploadFilled /></el-icon></el-button></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.insertInspiration')"><el-button :aria-label="t('projects.toolbar.insertInspiration')" text @click="emit('insertInspiration')"><el-icon><Picture /></el-icon></el-button></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.uploadAttachment')"><el-button :aria-label="t('projects.toolbar.uploadAttachment')" text @click="emit('uploadAttachment')"><el-icon><Paperclip /></el-icon></el-button></el-tooltip>
      </template>
    </ProEditorElementPlus>
    <DragHandle v-if="editor && !props.readOnly" :editor="editor" class="project-block-drag-handle">
      <el-tooltip :content="t('projects.moveBlock')"><span :aria-label="t('projects.moveBlock')"><el-icon><Rank /></el-icon></span></el-tooltip>
    </DragHandle>
    <ProjectImageViewer v-if="editor && viewerPosition !== null" :editor="editor" :position="viewerPosition" @close="viewerPosition = null" />
  </div></ContentZoom>
</template>

<style scoped>
/* 上传装饰不进入文档：图片区、状态和操作始终显示在粘贴位置。 */
.project-document-editor :deep(.project-upload-placeholder) { display: flex; align-items: center; gap: 12px; width: min(100%, 520px); margin: 12px 0; padding: 12px; border: 1px solid var(--line); border-radius: var(--radius-control); background: var(--surface-muted); }
.project-document-editor :deep(.project-upload-placeholder img) { width: 64px; height: 64px; flex: 0 0 64px; object-fit: cover; border-radius: 5px; }
.project-document-editor :deep(.project-upload-content) { display: grid; flex: 1; min-width: 0; gap: 6px; }
.project-document-editor :deep(.project-upload-name) { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.project-document-editor :deep(.project-upload-status) { font-size: 12px; color: var(--muted); }
.project-document-editor :deep(.project-upload-progress) { width: 100%; accent-color: var(--accent); }
.project-document-editor :deep(.project-upload-actions) { display: flex; gap: 8px; }
.project-document-editor :deep(.project-upload-actions button) { padding: 5px 10px; border: 1px solid var(--line); border-radius: var(--radius-control); background: var(--surface); color: var(--ink); cursor: pointer; }
/* 仅适配主题和版心；列表、任务项及工具栏结构沿用上游组件样式。 */
.project-document-editor { position: relative; }
.project-document-editor :deep(.tvp-editor) { overflow: visible; border-color: var(--line); border-radius: var(--radius-card); background: var(--surface); }
.project-document-editor.is-readonly :deep(.tvp-toolbar) { display: none; }
.project-document-editor :deep(.tvp-toolbar) { position: sticky; top: 0; z-index: 8; border-radius: var(--radius-card) var(--radius-card) 0 0; background: var(--surface); }
.project-document-editor :deep(.tvp-content-wrap) { overflow: visible; max-height: none; }
.project-document-editor :deep(.tvp-content) { padding: 32px clamp(24px, 5vw, 72px) 70px; }
/* 允许系统字体合成中文斜体，避免全站 font-synthesis:none 吞掉文档格式。 */
.project-document-editor :deep(.ProseMirror) { min-height: 55vh; color: var(--ink); font-synthesis: style weight; }
/* Tiptap 3.32 的 checkbox 后方为隐藏说明；保留说明并恢复原生框，避免上游旧版伪装样式隐藏勾选状态。 */
.project-document-editor :deep(.tvp-content .ProseMirror ul[data-type=taskList] li > label > input[type=checkbox]) { position: static; width: 16px; height: 16px; opacity: 1; accent-color: var(--accent); }
/* 私有媒体使用自己的操作条；隐藏上游针对公共 URL 节点的浮层，避免窄屏隐藏浮层撑宽页面。 */
.project-document-editor :deep(.tvp-img-bubble), .project-document-editor :deep(.tvp-media-bubble), .project-document-editor :deep(.tvp-file-bubble) { display: none; }
/* 节点宽度包含左右间距，缩小后自然并排，剩余宽度不足时自动换行。 */
.project-document-editor :deep(.project-resource-node) { display: inline-block; vertical-align: top; }
/* 角落手柄独立于拖动排序，使用组件库图标与足够的触控面积；实时缩放关闭宽度过渡。 */
.project-document-editor :deep(.project-resource-node.is-corner-resizing) { transition: none; }
.project-document-editor :deep(.resource-resize-handle) { position: absolute; right: -8px; bottom: -8px; z-index: 6; display: grid; width: 26px; height: 26px; place-items: center; padding: 2px; border: 1px solid var(--accent); border-radius: 6px; background: var(--surface); color: var(--accent); cursor: nwse-resize; touch-action: none; }
.project-document-editor :deep(.resource-resize-handle:focus-visible) { outline: 3px solid var(--focus); outline-offset: 2px; }
@media (pointer: coarse) { .project-document-editor :deep(.resource-resize-handle) { width: 44px; height: 44px; right: -12px; bottom: -12px; } }
.project-block-drag-handle { z-index: 12; display: grid; width: 28px; height: 32px; place-items: center; border-radius: var(--radius-control); background: var(--surface); color: var(--muted); cursor: grab; }
@media (max-width: 768px) {
  .project-document-editor :deep(.tvp-content) { padding: 20px 16px 180px; }
  /* 横向滚动保留全部格式按钮，插图与附件入口优先显示；固定层随可视视口避让键盘。 */
  .project-document-editor :deep(.tvp-toolbar) { position: fixed; inset: auto 0 var(--project-keyboard-bottom, 0px); z-index: 30; display: flex; flex-wrap: nowrap; overflow-x: auto; max-width: 100vw; border-radius: 12px 12px 0 0; padding-bottom: env(safe-area-inset-bottom); }
  .project-document-editor :deep(.tvp-toolbar > *) { flex-shrink: 0; }
  .project-block-drag-handle { display: none; }
  .project-document-editor :deep(.tvp-toolbar .el-button) { min-width: 44px; min-height: 44px; }
}
/* 拖动镜像和落点挂到 body，避免编辑器 DOM 重绘/滚动遮挡反馈。 */
:global(.project-resource-drag-ghost) { position: fixed; z-index: 2000; pointer-events: none; opacity: .7; border: 2px solid var(--accent); border-radius: 8px; background: var(--surface); box-shadow: var(--shadow); }
:global(.project-resource-drag-ghost img) { display: block; max-width: 100%; max-height: 150px; object-fit: contain; }
:global(.project-resource-drop-indicator) { position: fixed; z-index: 2001; pointer-events: none; background: var(--accent); color: var(--accent); font-size: 12px; font-weight: 700; white-space: nowrap; text-indent: 8px; line-height: 24px; }
</style>
