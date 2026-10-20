<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ViewerClient, type DocumentViewer, type ViewerTranslations } from '@zrimo/viewer'
import '@zrimo/viewer/styles.css'
import { api } from '../api/client'

const props = defineProps<{ modelValue: boolean; projectId: string; storekey: string; fileName: string; contentType: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
const { t } = useI18n({ useScope: 'global' })
const container = ref<HTMLElement | null>(null)
const loading = ref(false)
const failed = ref(false)
let client: ViewerClient | null = null
let viewer: DocumentViewer | null = null
let requestVersion = 0
const viewerTranslationKeys: (keyof ViewerTranslations)[] = [
  'previous', 'next', 'page', 'of', 'zoomIn', 'zoomOut', 'fitWidth', 'fitPage', 'search', 'searchPlaceholder',
  'matches', 'thumbnails', 'fullscreen', 'exitFullscreen', 'download', 'sheets', 'selectedRange', 'loading', 'close', 'noMatches',
]

/** 加载受项目授权校验的文件 Blob，并让 Zrimo 在浏览器本地预览 Office、PDF 和图像。 */
async function openViewer() {
  if (!props.modelValue || !container.value || loading.value) return
  const version = ++requestVersion
  loading.value = true
  failed.value = false
  try {
    const blob = await api.projectAttachmentContent(props.projectId, props.storekey)
    if (version !== requestVersion || !container.value) return
    client = ViewerClient.create({ assetBaseUrl: new URL(`${import.meta.env.BASE_URL}vendor/zrimo/`, window.location.href) })
    const viewerTranslations = Object.fromEntries(viewerTranslationKeys.map(key => [key, t(`projects.viewer.${key}`)])) as Partial<ViewerTranslations>
    viewer = client.createViewer({ container: container.value, ui: true, fit: 'width', layout: 'continuous', locale: 'en', translations: viewerTranslations })
    await viewer.load(blob, { fileName: props.fileName, contentType: props.contentType })
  } catch { if (version === requestVersion) failed.value = true }
  finally { if (version === requestVersion) loading.value = false }
}

async function closeViewer() {
  requestVersion += 1
  const activeViewer = viewer
  const activeClient = client
  viewer = null
  client = null
  if (activeViewer) await activeViewer.destroy()
  if (activeClient) await activeClient.destroy()
  emit('update:modelValue', false)
}

watch(() => props.modelValue, async open => { if (open) { await nextTick(); await openViewer() } else await closeViewer() })
onBeforeUnmount(() => { requestVersion += 1; void closeViewer() })
</script>

<template>
  <el-dialog :model-value="modelValue" class="project-preview-dialog" width="80vw" top="8vh" destroy-on-close :close-on-click-modal="false" @close="closeViewer">
    <template #header><div class="preview-heading"><strong>{{ fileName }}</strong><el-button text @click="closeViewer">{{ t('projects.closePreview') }}</el-button></div></template>
    <div ref="container" class="preview-container"></div>
    <div v-if="loading" class="preview-status">{{ t('common.loading') }}</div>
    <div v-if="failed" class="preview-status preview-error">{{ t('projects.unsupportedPreview') }}</div>
  </el-dialog>
</template>

<style>
.project-preview-dialog { max-width: 1120px; height: 84vh; margin-bottom: 0; overflow: hidden; }
.project-preview-dialog .el-dialog__body { height: calc(84vh - 68px); padding: 0 18px 18px; }
.project-preview-dialog .el-dialog__header { margin-right: 0; padding: 12px 18px; }
.project-preview-dialog .el-dialog__headerbtn { display: none; }
.preview-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.preview-heading strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.preview-container { width: 100%; height: 100%; min-height: 400px; overflow: hidden; border-radius: 6px; background: var(--surface-muted); }
.preview-status { position: absolute; inset: 50% 0 auto; z-index: 2; color: var(--muted); text-align: center; pointer-events: none; }
.preview-error { top: auto; bottom: 24px; color: var(--danger); }
@media (max-width: 768px) { .project-preview-dialog { width: 94vw !important; height: 82vh; margin: 0 auto; } .project-preview-dialog .el-dialog__body { height: calc(82vh - 56px); padding: 0 10px 10px; } .preview-container { min-height: 320px; } }
</style>
