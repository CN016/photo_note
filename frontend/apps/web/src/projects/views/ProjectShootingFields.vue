<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { ProjectShooting } from '../../api/types'
import { chinaDateTimeInput, toChinaTimestamp } from '../../schedule/scheduleUtils'

const props = defineProps<{ modelValue: ProjectShooting; readOnly: boolean; error?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: ProjectShooting] }>()
const { t } = useI18n()
/** 字段更新生成小对象快照，交给策划现有修订保存管线；不直接写接口或更改资源归属。 */
function update<K extends keyof ProjectShooting>(key: K, value: ProjectShooting[K]) {
  emit('update:modelValue', { ...props.modelValue, [key]: value })
}
function timeInput(key: 'start_at' | 'end_at', event: Event) {
  const value = (event.target as HTMLInputElement).value
  update(key, value ? toChinaTimestamp(value) : null)
}
</script>

<template>
  <section class="shooting-details" :aria-label="t('projects.shooting.title')">
    <h2>{{ t('projects.shooting.title') }}</h2>
    <div class="shooting-fields">
      <label for="shoot-start">{{ t('projects.shooting.start') }}<input id="shoot-start" type="datetime-local" :value="modelValue.start_at ? chinaDateTimeInput(modelValue.start_at) : ''" :disabled="readOnly" @change="timeInput('start_at', $event)" /></label>
      <label for="shoot-end">{{ t('projects.shooting.end') }}<input id="shoot-end" type="datetime-local" :value="modelValue.end_at ? chinaDateTimeInput(modelValue.end_at) : ''" :disabled="readOnly" @change="timeInput('end_at', $event)" /></label>
      <label for="shoot-location">{{ t('projects.shooting.location') }}<el-input id="shoot-location" :model-value="modelValue.location" maxlength="240" :disabled="readOnly" @update:model-value="update('location', $event)" /></label>
      <label for="shoot-person">{{ t('projects.shooting.person') }}<el-input id="shoot-person" :model-value="modelValue.person" maxlength="120" :disabled="readOnly" @update:model-value="update('person', $event)" /></label>
      <div class="shoot-kind"><span id="shoot-kind-label">{{ t('projects.shooting.kind') }}</span><el-radio-group :model-value="modelValue.kind" :disabled="readOnly" aria-labelledby="shoot-kind-label" @update:model-value="update('kind', $event as ProjectShooting['kind'])"><el-radio-button value="photo">{{ t('projects.shooting.photo') }}</el-radio-button><el-radio-button value="video">{{ t('projects.shooting.video') }}</el-radio-button></el-radio-group></div>
    </div>
    <p v-if="error" class="shooting-error" role="alert">{{ error }}</p>
    <slot />
  </section>
</template>

<style scoped>
.shooting-details { margin: 0 0 24px; padding: 20px 24px; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); }
h2 { font-size: 16px; font-weight: 500; margin: 0 0 16px; }
.shooting-fields { display: grid; grid-template-columns: repeat(2, minmax(0,1fr)); gap: 16px 24px; }
label, .shoot-kind { display: flex; min-width: 0; flex-direction: column; gap: 8px; font-size: 13px; color: var(--muted); }
input { width: 100%; min-width: 0; min-height: 44px; border: 1px solid var(--line); border-radius: 8px; padding: 8px 12px; font: inherit; font-size: 16px; color: var(--ink); background: var(--surface); color-scheme: inherit; box-sizing: border-box; }
.shooting-error { margin: 12px 0 0; color: var(--danger); font-size: 14px; }
input:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.shooting-fields :deep(.el-input__wrapper) { min-height: 44px; box-sizing: border-box; }
.shoot-kind { grid-column: 1 / -1; }
.shoot-kind :deep(.el-radio-button__inner) { min-height: 44px; display: flex; align-items: center; }
.shoot-kind :deep(.el-radio-button.is-active .el-radio-button__inner) { color: var(--on-accent); }
@media (max-width: 640px) { .shooting-details { padding: 16px; } .shooting-fields { grid-template-columns: minmax(0,1fr); gap: 16px; } }
</style>
