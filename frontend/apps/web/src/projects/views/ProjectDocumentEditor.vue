<script setup lang="ts">
import { computed, markRaw, shallowRef, watch } from 'vue'
import { Extension, getSchema, type Editor } from '@tiptap/core'
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
import { Paperclip, Picture, Rank } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { isDocumentCompatible } from '../documentCompatibility'
import { FontSize, InspirationImage, ProjectAttachment } from '../projectExtensions'
import { useColorTheme } from '../../theme'

/** 项目正文适配层：开源组件负责编辑交互，资源上传和持久化始终交回业务页面。 */
const props = defineProps<{ document: Record<string, unknown> }>()
const emit = defineEmits<{ ready: [editor: Editor]; incompatible: []; change: []; insertInspiration: []; uploadAttachment: [] }>()
const { t, locale } = useI18n({ useScope: 'global' })
const theme = useColorTheme()
const editor = shallowRef<Editor>()
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
  FontSize, InspirationImage, ProjectAttachment,
  Extension.create({
    name: 'projectEditorBridge',
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
</script>

<template>
  <div class="project-document-editor">
    <el-alert v-if="!compatible" :title="t('projects.documentIncompatible')" type="error" :closable="false" />
    <ProEditorElementPlus v-else :model-value="props.document" output="json" :extensions="extensions" :toolbar="toolbar"
      :toolbar-options="{ fontSizes: ['12px', '14px', '16px', '18px', '20px', '24px', '30px', '36px', '48px'] }"
      :editor-behavior-options="{ image: { allowUrl: false } }"
      :locale="language" :dark="theme === 'dark'" :autosave="false" :draft="false" :show-word-count="false"
      @update:model-value="emit('change')">
      <template #toolbar-after>
        <el-tooltip :content="t('projects.toolbar.insertInspiration')"><el-button :aria-label="t('projects.toolbar.insertInspiration')" text @click="emit('insertInspiration')"><el-icon><Picture /></el-icon></el-button></el-tooltip>
        <el-tooltip :content="t('projects.toolbar.uploadAttachment')"><el-button :aria-label="t('projects.toolbar.uploadAttachment')" text @click="emit('uploadAttachment')"><el-icon><Paperclip /></el-icon></el-button></el-tooltip>
      </template>
    </ProEditorElementPlus>
    <DragHandle v-if="editor" :editor="editor" class="project-block-drag-handle">
      <el-tooltip :content="t('projects.moveBlock')"><span :aria-label="t('projects.moveBlock')"><el-icon><Rank /></el-icon></span></el-tooltip>
    </DragHandle>
  </div>
</template>

<style scoped>
/* 仅适配主题和版心；列表、任务项及工具栏结构沿用上游组件样式。 */
.project-document-editor { position: relative; }
.project-document-editor :deep(.tvp-editor) { overflow: visible; border-color: var(--line); border-radius: var(--radius-card); background: var(--surface); }
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
  .project-document-editor :deep(.tvp-content) { padding: 20px 22px 90px; }
  .project-document-editor :deep(.tvp-toolbar .el-button) { min-width: 44px; min-height: 44px; }
}
</style>
