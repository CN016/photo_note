<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { PictureFilled } from '@element-plus/icons-vue'
import { loadDetailImage } from '../images/detailImages'

const props = defineProps<{ storekey: string; label: string; loadingLabel: string; unavailableLabel: string }>()
const root = ref<HTMLElement | null>(null)
const url = ref('')
const failed = ref(false)
const loading = ref(false)
let requestVersion = 0

/** 计划列表挂载即后台加载完整封面，详情复用有界私有图片缓存。 */
async function loadImage() {
  if (!props.storekey || loading.value || url.value) return
  const version = ++requestVersion
  loading.value = true
  failed.value = false
  try {
    const blob = await loadDetailImage(props.storekey)
    if (version !== requestVersion) return
    url.value = URL.createObjectURL(blob)
  } catch {
    if (version === requestVersion) failed.value = true
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

/** 编辑数据切换时释放旧对象地址，并立即预加载新封面。 */
watch(() => props.storekey, () => {
  requestVersion += 1
  if (url.value) URL.revokeObjectURL(url.value)
  url.value = ''
  failed.value = false
  loading.value = false
  void loadImage()
})

onMounted(() => { void loadImage() })

onBeforeUnmount(() => {
  requestVersion += 1
  if (url.value) URL.revokeObjectURL(url.value)
})
</script>

<template>
  <div ref="root" class="plan-cover-image" :aria-label="label" role="img">
    <img v-if="url" :src="url" alt="" loading="eager" />
    <span v-else-if="failed" class="plan-cover-fallback"><el-icon aria-hidden="true"><PictureFilled /></el-icon><small>{{ unavailableLabel }}</small></span>
    <span v-else class="plan-cover-fallback"><el-icon aria-hidden="true"><PictureFilled /></el-icon></span>
  </div>
</template>

<style scoped>
.plan-cover-image { display: grid; width: 68px; height: 68px; flex: 0 0 68px; overflow: hidden; place-items: center; border-radius: 8px; background: var(--surface-muted, #eeece3); color: var(--muted, #6b7467); }
.plan-cover-image img { width: 100%; height: 100%; object-fit: cover; }
.plan-cover-fallback { display: grid; justify-items: center; gap: 3px; font-size: 18px; }
.plan-cover-fallback small { max-width: 60px; overflow: hidden; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
</style>
