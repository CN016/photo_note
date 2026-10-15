<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import type { UploadFile } from 'element-plus'
import { api } from '../api/client'
import { ApiError } from '../api/types'
import { prepareImageUpload, type PreparedImageUpload } from '../imageUpload'

const props = defineProps<{ editorRoute: boolean; hasMobileNav: boolean }>()
const { t } = useI18n()
const visible = ref(false)
const mobile = ref(typeof window !== 'undefined' && window.matchMedia('(max-width: 768px)').matches)
const content = ref('')
const files = ref(new Map<number, PreparedImageUpload>())
const preparing = ref(false)
const submitting = ref(false)
const error = ref('')
const direction = computed<'btt' | 'rtl'>(() => mobile.value ? 'btt' : 'rtl')
const drawerSize = computed(() => mobile.value ? 'min(78vh, 620px)' : '430px')
const imageCount = computed(() => files.value.size)

function updateViewport() { mobile.value = window.matchMedia('(max-width: 768px)').matches }
if (typeof window !== 'undefined') window.addEventListener('resize', updateViewport)
function beforeClose(done: () => void) { if (!submitting.value) done() }

async function selectImage(file: UploadFile) {
  if (!file.raw || files.value.has(file.uid)) return
  preparing.value = true
  error.value = ''
  try {
    const prepared = await prepareImageUpload(file.raw)
    files.value.set(file.uid, prepared)
    files.value = new Map(files.value)
  } catch { error.value = t('feedback.imageInvalid') }
  finally { preparing.value = false }
}

function removeImage(file: UploadFile) {
  const prepared = files.value.get(file.uid)
  if (prepared) URL.revokeObjectURL(prepared.previewUrl)
  files.value.delete(file.uid)
  files.value = new Map(files.value)
}

async function submit() {
  error.value = ''
  const trimmed = content.value.trim()
  if (!trimmed && imageCount.value === 0) { error.value = t('feedback.required'); return }
  if (Array.from(content.value).length > 2000) { error.value = t('feedback.tooLong'); return }
  if (preparing.value || submitting.value) return
  submitting.value = true
  try {
    const images = [...files.value.values()]
    await api.feedback(trimmed, images.map(image => image.blob), images.map(image => image.filename))
    ElMessage.success(t('feedback.success'))
    resetForm()
    visible.value = false
  } catch (cause) {
    if (cause instanceof ApiError && cause.code === 'network') error.value = t('common.errors.network')
    else if (cause instanceof ApiError && cause.status === 429) error.value = t('feedback.rateLimited')
    else error.value = t('feedback.submitFailed')
  } finally { submitting.value = false }
}

function resetForm() {
  content.value = ''
  for (const image of files.value.values()) URL.revokeObjectURL(image.previewUrl)
  files.value.clear()
  files.value = new Map()
  error.value = ''
}

onBeforeUnmount(() => {
  window.removeEventListener('resize', updateViewport)
  for (const image of files.value.values()) URL.revokeObjectURL(image.previewUrl)
})
</script>

<template>
  <div class="feedback-entry" :class="{ 'feedback-entry-editor': props.editorRoute, 'feedback-entry-no-nav': !props.hasMobileNav }">
    <el-button class="feedback-trigger" type="primary" :aria-label="t('feedback.open')" @click="visible = true">
      <svg class="feedback-trigger-icon" aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <path d="M20 11.5a7.5 7.5 0 0 1-7.5 7.5H7l-3.5 2v-9.5A7.5 7.5 0 0 1 11 4h1.5a7.5 7.5 0 0 1 7.5 7.5Z" />
        <path d="M8 10h8M8 14h5" />
      </svg>
      <span class="feedback-trigger-label">{{ t('feedback.open') }}</span>
    </el-button>
  </div>
  <el-drawer v-model="visible" :direction="direction" :size="drawerSize" :title="t('feedback.title')" :append-to-body="true" :close-on-click-modal="!submitting" :before-close="beforeClose">
    <div class="feedback-panel">
      <p class="feedback-description">{{ t('feedback.description') }}</p>
      <el-input v-model="content" type="textarea" :rows="6" maxlength="2000" show-word-limit :placeholder="t('feedback.contentPlaceholder')" />
      <div class="feedback-image-label"><span>{{ t('feedback.images') }}</span><small>{{ t('feedback.imageCount', { count: imageCount }) }}</small></div>
      <el-upload action="#" :auto-upload="false" list-type="picture-card" accept="image/*,.heic,.heif" multiple :limit="5" :on-change="selectImage" :on-remove="removeImage" :disabled="preparing || submitting">
        <span class="upload-plus">＋</span>
      </el-upload>
      <p class="feedback-upload-help">{{ t('feedback.imageHelp') }}</p>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <div class="feedback-actions">
        <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="preparing || submitting" @click="submit">{{ t('feedback.submit') }}</el-button>
      </div>
    </div>
  </el-drawer>
</template>
