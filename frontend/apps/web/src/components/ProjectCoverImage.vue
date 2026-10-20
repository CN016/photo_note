<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Files } from '@element-plus/icons-vue'
import { api } from '../api/client'

const props = defineProps<{ storekey: string; emoji: string; label: string }>()
const url = ref('')
const failed = ref(false)
let version = 0

/** 项目封面只在卡片组件挂载时通过鉴权接口读取，并在卸载时释放临时对象地址。 */
async function load() {
  if (!props.storekey) return
  const current = ++version
  failed.value = false
  try {
    const blob = await api.download(props.storekey)
    if (current === version) url.value = URL.createObjectURL(blob)
  } catch { if (current === version) failed.value = true }
}

watch(() => props.storekey, async () => {
  version += 1
  if (url.value) URL.revokeObjectURL(url.value)
  url.value = ''
  await load()
})
onMounted(() => { void load() })
onBeforeUnmount(() => { version += 1; if (url.value) URL.revokeObjectURL(url.value) })
</script>

<template>
  <div class="project-cover-image" :aria-label="label" role="img">
    <img v-if="url && !failed" :src="url" alt="" />
    <span v-else-if="emoji">{{ emoji }}</span>
    <el-icon v-else><Files /></el-icon>
  </div>
</template>

<style scoped>
.project-cover-image { display: grid; width: 70px; height: 70px; flex: 0 0 70px; overflow: hidden; place-items: center; border-radius: 9px; background: var(--surface-muted, #f1f3f7); font-size: 34px; }
.project-cover-image img { width: 100%; height: 100%; object-fit: cover; }
@media (max-width: 768px) { .project-cover-image { width: 62px; height: 62px; flex-basis: 62px; font-size: 30px; } }
</style>
