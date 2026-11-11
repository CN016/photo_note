<script setup lang="ts">
/** 属性为独立窗口，编辑参数使用锚定按钮的非模态浮层，画布始终可交互。 */
const props = defineProps<{ panel: string; title: string; anchor?: HTMLElement }>()
const emit = defineEmits<{ close: [panel: string] }>()
function updateVisible(value: boolean) { if (!value) emit('close', props.panel) }
</script>
<template>
  <el-dialog v-if="panel === 'properties'" :model-value="true" :title="title" width="min(560px, calc(100vw - 32px))" class="image-tools-dialog" append-to-body @close="emit('close', 'properties')"><slot /></el-dialog>
  <el-popover v-else :visible="true" :virtual-ref="anchor" virtual-triggering trigger="click" placement="top" :width="360" :persistent="false" :title="title" popper-class="image-tool-popover" @update:visible="updateVisible"><slot /></el-popover>
</template>
<style>
.image-tool-popover.el-popover { width: min(360px, calc(100vw - 24px)) !important; max-width: calc(100vw - 24px); padding: 16px; border-radius: 18px; }
.image-tool-popover > .image-tool-panel { max-height: min(340px, 48dvh); overflow-y: auto; overscroll-behavior: contain; }
.image-tool-popover .control-section { padding: 8px 0; }
.image-tool-popover .section-title-row h2 { font-size: 13px; }
.image-tool-popover .anchor-size-help { display: none; }
.image-tool-popover .notes-section .note-list { max-height: 140px; overflow-y: auto; }
.image-tool-popover .transform-buttons { display: flex; gap: 6px; }
.image-tool-popover .transform-buttons .tool-button { flex: 1; min-width: 0; min-height: 44px; padding: 6px; font-size: 12px; }
</style>
