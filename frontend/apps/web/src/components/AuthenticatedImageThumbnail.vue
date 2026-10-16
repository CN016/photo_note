<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../api/client'

const props = withDefaults(defineProps<{ storekey: string; label: string; shape?: 'portrait' | 'square' }>(), { shape: 'portrait' })
const emit = defineEmits<{ preview: [] }>()
const { t } = useI18n()
const element = ref<HTMLElement | null>(null)
const imageUrl = ref('')
const failed = ref(false)
const loading = ref(false)
let observer: IntersectionObserver | null = null
let requestVersion = 0

/** 图片进入可视区域后通过 Access Token 下载附件，并撤销临时对象地址。 */
async function loadImage() {
  const version = ++requestVersion
  loading.value = true
  try {
    const blob = await api.download(props.storekey)
    if (version !== requestVersion) return
    imageUrl.value = URL.createObjectURL(blob)
  } catch {
    if (version === requestVersion) failed.value = true
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

/** 延后读取屏幕外的附件，反馈列表长时避免同时下载所有图片。 */
onMounted(() => {
  if ('IntersectionObserver' in window) {
    observer = new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) {
        observer?.disconnect()
        observer = null
        void loadImage()
      }
    }, { rootMargin: '180px' })
    if (element.value) observer.observe(element.value)
  } else void loadImage()
})

onBeforeUnmount(() => {
  requestVersion += 1
  observer?.disconnect()
  if (imageUrl.value) URL.revokeObjectURL(imageUrl.value)
})
</script>

<template>
  <button ref="element" class="authenticated-image-thumbnail" :class="`is-${shape}`" type="button" :aria-label="label" @click="emit('preview')">
    <img v-if="imageUrl" :src="imageUrl" alt="" loading="lazy" />
    <span v-else-if="failed">{{ t('console.imageUnavailable') }}</span>
    <span v-else>{{ loading ? t('console.imageLoading') : t('console.imageLoading') }}</span>
  </button>
</template>
