<script setup lang="ts">
import { Plus } from '@element-plus/icons-vue'

const props = withDefaults(defineProps<{
  label: string
  to?: string
  standalone?: boolean
  disabled?: boolean
}>(), {
  to: '',
  standalone: false,
  disabled: false,
})
const emit = defineEmits<{ select: [] }>()
</script>

<template>
  <RouterLink v-if="props.to" custom :to="props.to" v-slot="{ navigate }">
    <button class="create-fab" :class="{ 'is-standalone': props.standalone }" type="button" :disabled="props.disabled" :aria-label="props.label" :title="props.label" @click="navigate">
      <el-icon aria-hidden="true"><Plus /></el-icon>
    </button>
  </RouterLink>
  <button v-else class="create-fab" :class="{ 'is-standalone': props.standalone }" type="button" :disabled="props.disabled" :aria-label="props.label" :title="props.label" @click="emit('select')">
    <el-icon aria-hidden="true"><Plus /></el-icon>
  </button>
</template>

<style scoped>
.create-fab { position: fixed; z-index: 45; right: 22px; bottom: calc(82px + env(safe-area-inset-bottom)); display: grid; width: 56px; height: 56px; place-items: center; padding: 0; border: 1px solid transparent; border-radius: 18px; background: var(--accent); box-shadow: 0 12px 30px color-mix(in srgb, var(--accent) 30%, transparent); color: var(--on-accent); cursor: pointer; font-size: 24px; transition: background-color .16s ease, transform .16s ease, box-shadow .16s ease; }
.create-fab:hover:not(:disabled) { background: var(--accent-hover); box-shadow: 0 14px 34px color-mix(in srgb, var(--accent) 36%, transparent); transform: translateY(-2px); }
.create-fab:active:not(:disabled) { background: var(--accent-pressed); transform: translateY(0); }
.create-fab:focus-visible { outline: 3px solid var(--focus); outline-offset: 4px; }
.create-fab.is-standalone { bottom: calc(20px + env(safe-area-inset-bottom)); }
@media (max-width: 768px) {
  .create-fab { right: 16px; width: 52px; height: 52px; border-radius: 16px; font-size: 22px; }
}
@media (prefers-reduced-motion: reduce) {
  .create-fab { transition: none; }
}
</style>
