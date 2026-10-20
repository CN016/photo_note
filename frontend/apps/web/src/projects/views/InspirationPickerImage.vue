<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '../../api/client'

const props = defineProps<{ storekey: string; label: string }>()
const url = ref('')
let requestVersion = 0

/** 灵感图库选择器只下载视口附近的缩略来源图片，卸载时释放 object URL。 */
onMounted(async () => {
  const version = ++requestVersion
  try { const blob = await api.download(props.storekey); if (version === requestVersion) url.value = URL.createObjectURL(blob) }
  catch { /* 列表仍显示无图占位，不影响选择其它灵感图。 */ }
})
onBeforeUnmount(() => { requestVersion += 1; if (url.value) URL.revokeObjectURL(url.value) })
</script>

<template><span class="picker-image"><img v-if="url" :src="url" :alt="label" loading="lazy" /><span v-else>{{ label.slice(0, 1) }}</span></span></template>

<style scoped>
.picker-image { display: grid; width: 100%; aspect-ratio: 1; overflow: hidden; place-items: center; border-radius: 7px; background: var(--surface-muted); color: var(--muted); font-size: 24px; }
.picker-image img { width: 100%; height: 100%; object-fit: cover; }
</style>
