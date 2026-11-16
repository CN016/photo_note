<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { loadDetailImage } from '../images/detailImages'

const props = withDefaults(defineProps<{ storekey: string; label: string; shape?: 'portrait' | 'square' }>(), { shape: 'portrait' })
const emit = defineEmits<{ preview: [] }>()
const { t } = useI18n()
const element = ref<HTMLElement | null>(null)
const imageUrl = ref('')
const failed = ref(false)
const loading = ref(false)
let requestVersion = 0

/** 列表挂载即通过 Access Token 预取完整附件，并撤销临时对象地址。 */
async function loadImage() {
  const version = ++requestVersion
  loading.value = true
  try {
    const blob = await loadDetailImage(props.storekey)
    if (version !== requestVersion) return
    imageUrl.value = URL.createObjectURL(blob)
  } catch {
    if (version === requestVersion) failed.value = true
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

/** 全部已加载条目主动预取，实际网络并发由共享缓存队列限制。 */
onMounted(() => { void loadImage() })

onBeforeUnmount(() => {
  requestVersion += 1
  if (imageUrl.value) URL.revokeObjectURL(imageUrl.value)
})
</script>

<template>
  <button ref="element" class="authenticated-image-thumbnail" :class="`is-${shape}`" type="button" :aria-label="label" @click="emit('preview')">
    <img v-if="imageUrl" :src="imageUrl" alt="" loading="eager" />
    <span v-else-if="failed">{{ t('console.imageUnavailable') }}</span>
    <span v-else aria-hidden="true"></span>
  </button>
</template>
