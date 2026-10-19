<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { InspirationRecord } from '../api/types'
import { api } from '../api/client'
import { previewObjectUrl } from '../../../../packages/image-processing/src'

type ThumbnailRecord = Pick<InspirationRecord, 'file_name' | 'image_key'> & { data?: Record<string, unknown> }
const props = defineProps<{ record: ThumbnailRecord }>()
const { t } = useI18n()
const element = ref<HTMLElement | null>(null)
const imageUrl = ref<string | null>(null)
const failed = ref(false)
const loading = ref(false)
let observer: IntersectionObserver | null = null
let requestVersion = 0

const imageName = computed(() => props.record.file_name || t('gallery.untitled'))
const imageLabel = computed(() => t('gallery.openImage', { name: imageName.value }))
const imageKey = computed(() => props.record.image_key)

/** 懒加载触发后鉴权读取图片并生成缩略图地址，过期请求会立即释放地址。 */
async function loadPreview() {
  const version = ++requestVersion
  loading.value = true
  failed.value = false
  try {
    const blob = await api.download(imageKey.value)
    const url = await previewObjectUrl(blob)
    if (version !== requestVersion) { URL.revokeObjectURL(url); return }
    imageUrl.value = url
  } catch { if (version === requestVersion) failed.value = true }
  finally { if (version === requestVersion) loading.value = false }
}

watch(imageKey, () => {
  if (imageUrl.value) URL.revokeObjectURL(imageUrl.value)
  imageUrl.value = null
  if (element.value && observer === null) void loadPreview()
})

onMounted(() => {
  if ('IntersectionObserver' in window) {
    observer = new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) { observer?.disconnect(); observer = null; void loadPreview() }
    }, { rootMargin: '240px' })
    if (element.value) observer.observe(element.value)
  } else void loadPreview()
})

onBeforeUnmount(() => {
  requestVersion += 1
  observer?.disconnect()
  if (imageUrl.value) URL.revokeObjectURL(imageUrl.value)
})
</script>

<template>
  <div ref="element" class="thumbnail-frame" :aria-label="imageLabel">
    <img v-if="imageUrl" :src="imageUrl" :alt="imageName" loading="lazy" />
    <span v-else-if="failed" class="thumbnail-state">{{ t('gallery.imageUnavailable') }}</span>
    <span v-else class="thumbnail-state"><i class="loading-dot"></i>{{ t('gallery.loadingImage') }}</span>
  </div>
</template>
