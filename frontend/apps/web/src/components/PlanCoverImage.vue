<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { PictureFilled } from '@element-plus/icons-vue'
import { api } from '../api/client'

const props = defineProps<{ storekey: string; label: string; loadingLabel: string; unavailableLabel: string }>()
const root = ref<HTMLElement | null>(null)
const url = ref('')
const failed = ref(false)
const loading = ref(false)
let observer: IntersectionObserver | null = null
let visible = false
let requestVersion = 0

/** 图片进入视口附近时才通过鉴权接口下载，避免计划列表一次性拉取所有封面。 */
async function loadImage() {
  if (!props.storekey || loading.value || url.value) return
  const version = ++requestVersion
  loading.value = true
  failed.value = false
  try {
    const blob = await api.download(props.storekey)
    if (version !== requestVersion) return
    url.value = URL.createObjectURL(blob)
  } catch {
    if (version === requestVersion) failed.value = true
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

/** 编辑数据切换时释放旧对象地址，并在当前卡片已接近视口时重新加载。 */
watch(() => props.storekey, () => {
  requestVersion += 1
  if (url.value) URL.revokeObjectURL(url.value)
  url.value = ''
  failed.value = false
  loading.value = false
  if (visible) void loadImage()
})

onMounted(() => {
  if ('IntersectionObserver' in window) {
    observer = new IntersectionObserver(entries => {
      if (!entries.some(entry => entry.isIntersecting)) return
      visible = true
      observer?.disconnect()
      observer = null
      void loadImage()
    }, { rootMargin: '120px' })
    if (root.value) observer.observe(root.value)
  } else {
    visible = true
    void loadImage()
  }
})

onBeforeUnmount(() => {
  requestVersion += 1
  observer?.disconnect()
  if (url.value) URL.revokeObjectURL(url.value)
})
</script>

<template>
  <div ref="root" class="plan-cover-image" :aria-label="label" role="img">
    <img v-if="url" :src="url" alt="" loading="lazy" />
    <span v-else-if="failed" class="plan-cover-fallback"><el-icon aria-hidden="true"><PictureFilled /></el-icon><small>{{ unavailableLabel }}</small></span>
    <span v-else class="plan-cover-fallback"><el-icon aria-hidden="true"><PictureFilled /></el-icon><small>{{ loading ? loadingLabel : loadingLabel }}</small></span>
  </div>
</template>

<style scoped>
.plan-cover-image { display: grid; width: 68px; height: 68px; flex: 0 0 68px; overflow: hidden; place-items: center; border-radius: 8px; background: var(--surface-muted, #f1f3f7); color: var(--muted, #7a8293); }
.plan-cover-image img { width: 100%; height: 100%; object-fit: cover; }
.plan-cover-fallback { display: grid; justify-items: center; gap: 3px; font-size: 18px; }
.plan-cover-fallback small { max-width: 60px; overflow: hidden; font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
</style>
