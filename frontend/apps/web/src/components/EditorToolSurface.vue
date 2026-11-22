<script setup lang="ts">
/** 所有工具都使用锚定按钮的非模态玻璃浮层，避免独立弹窗与详情材质割裂。 */
const props = defineProps<{ panel: string; title: string; anchor?: HTMLElement; ambient?: string }>()
const emit = defineEmits<{ close: [panel: string] }>()
function updateVisible(value: boolean) { if (!value) emit('close', props.panel) }
</script>
<template>
  <el-popover
    :visible="true"
    :virtual-ref="anchor"
    virtual-triggering
    trigger="click"
    :placement="panel === 'properties' ? 'bottom-end' : 'top'"
    :width="panel === 'properties' ? 392 : 360"
    :offset="10"
    :persistent="false"
    :title="title"
    :popper-class="['image-tool-popover', `image-tool-popover--${panel}`]"
    :popper-style="ambient ? { '--detail-ambient': ambient } : undefined"
    @update:visible="updateVisible"
  ><slot /></el-popover>
</template>
<style>
.image-tool-popover.el-popover { width: min(360px, calc(100vw - 24px)) !important; max-width: calc(100vw - 24px); padding: 16px; border-radius: 18px; }
.image-tool-popover--properties.el-popover { width: min(392px, calc(100vw - 24px)) !important; padding: 18px; }
.image-tool-popover.el-popover.el-popper { overflow: hidden; border: 1px solid color-mix(in srgb, var(--ink) 12%, transparent); border-radius: 24px; background: radial-gradient(130% 180% at 8% 0%, color-mix(in srgb, var(--detail-ambient, var(--accent)) 22%, transparent), transparent 60%), linear-gradient(180deg, rgb(255 255 255 / 24%), rgb(255 255 255 / 4%)), color-mix(in srgb, var(--ink) 8%, transparent); box-shadow: inset 0 1px 0 rgb(255 255 255 / 62%), 0 18px 48px rgb(0 0 0 / 18%); backdrop-filter: blur(32px) saturate(200%); -webkit-backdrop-filter: blur(32px) saturate(200%); }
.image-tool-popover .el-popper__arrow::before { border-color: color-mix(in srgb, var(--ink) 12%, transparent); background: color-mix(in srgb, var(--surface) 72%, transparent); }
.image-tool-popover .el-select__wrapper { min-height: 46px; border-radius: 14px; background: color-mix(in srgb, var(--surface) 36%, transparent); box-shadow: inset 0 1px 0 rgb(255 255 255 / 54%), 0 0 0 1px color-mix(in srgb, var(--ink) 9%, transparent); }
.image-tool-popover .el-select__tags-text { color: var(--ink); }
.image-tool-popover .el-tag { border-color: color-mix(in srgb, var(--ink) 10%, transparent); background: color-mix(in srgb, var(--surface) 54%, transparent); color: var(--ink-soft); }
.image-tag-select-popper.el-popper { overflow: hidden; border: 1px solid color-mix(in srgb, var(--ink) 12%, transparent); border-radius: 18px; background: linear-gradient(180deg, rgb(255 255 255 / 18%), rgb(255 255 255 / 3%)), color-mix(in srgb, var(--ink) 8%, transparent); box-shadow: inset 0 1px 0 rgb(255 255 255 / 50%), 0 14px 36px rgb(0 0 0 / 16%); backdrop-filter: blur(28px) saturate(190%); -webkit-backdrop-filter: blur(28px) saturate(190%); }
.image-tag-select-popper .el-select-dropdown__item { color: var(--ink-soft); }
.image-tag-select-popper .el-select-dropdown__item.is-hovering, .image-tag-select-popper .el-select-dropdown__item.is-selected { background: color-mix(in srgb, var(--accent) 14%, transparent); color: var(--accent-hover); }
.image-tool-popover > .image-tool-panel { max-height: min(340px, 48dvh); overflow-y: auto; overscroll-behavior: contain; }
.image-tool-popover--properties > .image-tool-panel { max-height: min(520px, calc(100dvh - 128px)); }
.image-tool-popover .control-section { padding: 8px 0; }
.image-tool-popover .section-title-row h2 { font-size: 13px; }
.image-tool-popover .anchor-size-help { display: none; }
.image-tool-popover .notes-section .note-list { max-height: 140px; overflow-y: auto; }
.image-tool-popover .transform-buttons { display: flex; gap: 6px; }
.image-tool-popover .transform-buttons .tool-button { flex: 1; min-width: 0; min-height: 44px; padding: 6px; font-size: 12px; }
</style>
