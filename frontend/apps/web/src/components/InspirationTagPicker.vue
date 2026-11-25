<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Check, Close, Delete, EditPen, Plus } from '@element-plus/icons-vue'
import type { TagRecord } from '../api/types'

const props = withDefaults(defineProps<{
  modelValue: boolean
  selectedIds: string[]
  tags: TagRecord[]
  busy?: boolean
  error?: string
  max?: number
  onCreateCustom: (name: string) => Promise<TagRecord | null>
  onRenameCustom: (tag: TagRecord, name: string) => Promise<boolean>
  onDeleteCustom: (tag: TagRecord) => Promise<boolean>
}>(), { busy: false, error: '', max: 8 })

const emit = defineEmits<{ 'update:modelValue': [open: boolean]; confirm: [ids: string[]] }>()
const { t } = useI18n()
const categories = ['subject', 'composition', 'lighting', 'style'] as const
const activeCategory = ref<(typeof categories)[number]>('subject')
const draftIDs = ref<string[]>([])
const customInput = ref('')
const localError = ref('')
const customBusy = ref(false)

watch(() => props.modelValue, open => {
  if (!open) return
  activeCategory.value = 'subject'
  draftIDs.value = Array.from(new Set(props.selectedIds))
  customInput.value = ''
  localError.value = ''
})

const tagByID = computed(() => new Map(props.tags.map(tag => [tag.business_id, tag])))
const categoryTags = computed(() => props.tags.filter(tag => tag.tag_type === 'system' && tag.category === activeCategory.value))
const customTags = computed(() => props.tags
  .filter(tag => tag.tag_type === 'custom')
  .sort((left, right) => right.usage_count - left.usage_count
    || (Date.parse(right.last_used_at || '') || 0) - (Date.parse(left.last_used_at || '') || 0)))
const atLimit = computed(() => draftIDs.value.length >= props.max)
const overLimit = computed(() => draftIDs.value.length > props.max)
const errorText = computed(() => localError.value || props.error)

function close(): void {
  if (props.busy || customBusy.value) return
  emit('update:modelValue', false)
}

function toggle(id: string): void {
  localError.value = ''
  if (draftIDs.value.includes(id)) {
    draftIDs.value = draftIDs.value.filter(item => item !== id)
    return
  }
  if (atLimit.value) {
    localError.value = t('tagPicker.limit')
    return
  }
  draftIDs.value = [...draftIDs.value, id]
}

async function addCustom(): Promise<void> {
  const name = customInput.value.trim()
  if (!name) {
    localError.value = t('tagPicker.emptyTag')
    return
  }
  if (atLimit.value) {
    localError.value = t('tagPicker.limit')
    return
  }
  const existing = props.tags.find(tag => tag.content === name)
  if (existing) {
    toggle(existing.business_id)
    customInput.value = ''
    return
  }
  customBusy.value = true
  localError.value = ''
  try {
    const created = await props.onCreateCustom(name)
    if (created && !draftIDs.value.includes(created.business_id)) draftIDs.value = [...draftIDs.value, created.business_id]
    customInput.value = ''
  } catch {
    localError.value = t('tagPicker.saveFailed')
  } finally {
    customBusy.value = false
  }
}

async function renameCustom(tag: TagRecord): Promise<void> {
  try {
    const result = await ElMessageBox.prompt(t('tagPicker.renamePrompt'), t('tagPicker.rename'), {
      inputValue: tag.content,
      inputValidator: value => value.trim().length > 0 && Array.from(value.trim()).length <= 64 || t('tagPicker.emptyTag'),
      confirmButtonText: t('tagPicker.confirm'),
      cancelButtonText: t('tagPicker.cancel'),
      customClass: 'editor-glass-dialog',
    })
    customBusy.value = true
    if (!await props.onRenameCustom(tag, result.value.trim())) localError.value = t('tagPicker.saveFailed')
  } catch {
    // 取消输入或标签接口失败时保留弹窗当前选择，由父页面提供具体错误。
  } finally {
    customBusy.value = false
  }
}

async function deleteCustom(tag: TagRecord): Promise<void> {
  try {
    await ElMessageBox.confirm(t('tagPicker.deleteConfirm', { name: tag.content }), t('tagPicker.delete'), {
      confirmButtonText: t('tagPicker.confirm'),
      cancelButtonText: t('tagPicker.cancel'),
      type: 'warning',
      customClass: 'editor-glass-dialog',
    })
    customBusy.value = true
    if (await props.onDeleteCustom(tag)) draftIDs.value = draftIDs.value.filter(id => id !== tag.business_id)
    else localError.value = t('tagPicker.saveFailed')
  } catch {
    // 用户取消删除时保持已选标签不变。
  } finally {
    customBusy.value = false
  }
}

function confirm(): void {
  if (props.busy || customBusy.value || overLimit.value) return
  emit('confirm', draftIDs.value.filter(id => tagByID.value.has(id)))
}
</script>

<template>
  <Teleport to="body">
    <Transition name="tag-picker">
      <div v-if="modelValue" class="tag-picker-layer" @click.self="close">
        <section class="tag-picker" role="dialog" aria-modal="true" :aria-label="t('tagPicker.title')" @keydown.esc.prevent="close">
          <header class="tag-picker-header">
            <div><h2>{{ t('tagPicker.title') }}</h2><small :class="{ over: overLimit }">{{ draftIDs.length }} / {{ max }}</small></div>
            <button type="button" :aria-label="t('tagPicker.cancel')" @click="close"><el-icon><Close /></el-icon></button>
          </header>

          <div class="tag-picker-tabs" role="tablist" :aria-label="t('tagPicker.title')">
            <button v-for="category in categories" :key="category" type="button" role="tab" :aria-selected="activeCategory === category" :class="{ active: activeCategory === category }" @click="activeCategory = category">{{ t(`tagPicker.categories.${category}`) }}</button>
          </div>

          <div class="tag-picker-options" role="tabpanel">
            <button v-for="tag in categoryTags" :key="tag.business_id" type="button" class="tag-option" :class="{ selected: draftIDs.includes(tag.business_id) }" :aria-pressed="draftIDs.includes(tag.business_id)" :disabled="busy || customBusy || (!draftIDs.includes(tag.business_id) && atLimit)" @click="toggle(tag.business_id)">
              <el-icon v-if="draftIDs.includes(tag.business_id)"><Check /></el-icon><span>{{ tag.content }}</span>
            </button>
            <p v-if="!categoryTags.length">{{ t('tagPicker.empty') }}</p>
          </div>

          <section class="custom-tag-section">
            <div class="custom-tag-heading"><strong>{{ t('tagPicker.customTitle') }}</strong><span v-if="atLimit">{{ t('tagPicker.limit') }}</span></div>
            <el-input v-model="customInput" maxlength="64" :disabled="busy || customBusy || atLimit" :placeholder="t('tagPicker.customPlaceholder')" :aria-label="t('tagPicker.customPlaceholder')" @keydown.enter.prevent="addCustom">
              <template #append><el-button :disabled="busy || customBusy || atLimit || !customInput.trim()" :loading="customBusy" @click="addCustom"><el-icon><Plus /></el-icon></el-button></template>
            </el-input>
            <div v-if="customTags.length" class="custom-tag-list">
              <span v-for="tag in customTags" :key="tag.business_id" class="custom-tag-pill" :class="{ selected: draftIDs.includes(tag.business_id) }">
                <button type="button" class="custom-tag-select" :disabled="busy || customBusy || (!draftIDs.includes(tag.business_id) && atLimit)" :aria-pressed="draftIDs.includes(tag.business_id)" @click="toggle(tag.business_id)">
                  <el-icon v-if="draftIDs.includes(tag.business_id)"><Check /></el-icon><span>{{ tag.content }}</span><small v-if="tag.usage_count">{{ t('tagPicker.usage', { count: tag.usage_count }) }}</small>
                </button>
                <button type="button" class="custom-tag-manage" :aria-label="t('tagPicker.rename')" :disabled="customBusy" @click="renameCustom(tag)"><el-icon><EditPen /></el-icon></button>
                <button type="button" class="custom-tag-manage danger" :aria-label="t('tagPicker.delete')" :disabled="customBusy" @click="deleteCustom(tag)"><el-icon><Delete /></el-icon></button>
              </span>
            </div>
          </section>

          <p v-if="errorText" class="tag-picker-error" role="alert">{{ errorText }}</p>
          <footer class="tag-picker-footer">
            <el-button :disabled="busy || customBusy" @click="close">{{ t('tagPicker.cancel') }}</el-button>
            <el-button type="primary" :disabled="busy || customBusy || overLimit" @click="confirm">{{ t('tagPicker.confirm') }}</el-button>
          </footer>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.tag-picker-layer { position: fixed; z-index: 2600; inset: 0; display: flex; align-items: flex-end; justify-content: center; padding: 16px 12px max(16px, env(safe-area-inset-bottom)); background: rgb(24 30 27 / 10%); backdrop-filter: blur(6px); -webkit-backdrop-filter: blur(6px); }
.tag-picker { display: flex; width: min(620px, 100%); max-height: min(78dvh, 760px); flex-direction: column; gap: 14px; overflow: hidden; padding: 18px; border: 1px solid color-mix(in srgb, var(--ink) 12%, transparent); border-radius: 28px; background: radial-gradient(130% 180% at 8% 0%, color-mix(in srgb, var(--detail-ambient, var(--accent)) 18%, transparent), transparent 60%), linear-gradient(180deg, rgb(255 255 255 / 72%), rgb(255 251 244 / 88%)), var(--surface-overlay); box-shadow: inset 0 1px 0 rgb(255 255 255 / 75%), 0 24px 70px rgb(35 45 38 / 20%); }
.tag-picker-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.tag-picker-header > div { display: flex; align-items: baseline; gap: 8px; }
.tag-picker-header h2 { margin: 0; color: var(--ink); font-size: 18px; font-weight: 600; }
.tag-picker-header small { color: var(--muted); font-size: 12px; }
.tag-picker-header small.over { color: var(--warning); }
.tag-picker-header > button { display: grid; width: 38px; height: 38px; place-items: center; border: 1px solid color-mix(in srgb, var(--ink) 10%, transparent); border-radius: 50%; background: color-mix(in srgb, var(--surface) 42%, transparent); color: var(--ink-soft); cursor: pointer; }
.tag-picker-tabs { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 4px; padding: 4px; border: 1px solid color-mix(in srgb, var(--ink) 8%, transparent); border-radius: 16px; background: color-mix(in srgb, var(--surface) 38%, transparent); }
.tag-picker-tabs button { min-height: 36px; padding: 4px 6px; border: 0; border-radius: 12px; background: transparent; color: var(--muted); cursor: pointer; font: inherit; font-size: 12px; }
.tag-picker-tabs button.active { background: color-mix(in srgb, var(--surface-overlay) 82%, transparent); box-shadow: inset 0 1px 0 rgb(255 255 255 / 65%), 0 4px 12px rgb(40 54 45 / 7%); color: var(--ink); font-weight: 600; }
.tag-picker-options { display: flex; min-height: 96px; max-height: 190px; flex-wrap: wrap; align-content: flex-start; gap: 8px; overflow-y: auto; padding: 2px; overscroll-behavior: contain; }
.tag-option { display: inline-flex; min-height: 36px; align-items: center; gap: 4px; padding: 7px 13px; border: 1px solid color-mix(in srgb, var(--accent) 16%, var(--line)); border-radius: 999px; background: color-mix(in srgb, #dff1e7 72%, transparent); color: var(--ink-soft); cursor: pointer; font: inherit; font-size: 13px; }
.tag-option.selected { border-color: color-mix(in srgb, var(--accent) 38%, transparent); background: color-mix(in srgb, var(--accent) 15%, var(--surface)); color: var(--accent-hover); }
.tag-option:disabled, .custom-tag-select:disabled { cursor: not-allowed; opacity: .48; }
.tag-picker-options > p { margin: 12px 4px; color: var(--muted); font-size: 13px; }
.custom-tag-section { display: grid; gap: 10px; padding-top: 12px; border-top: 1px solid color-mix(in srgb, var(--ink) 8%, transparent); }
.custom-tag-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.custom-tag-heading strong { color: var(--ink); font-size: 13px; font-weight: 600; }
.custom-tag-heading span { color: var(--muted); font-size: 12px; text-align: right; }
.custom-tag-section :deep(.el-input__wrapper) { border-radius: 14px; background: color-mix(in srgb, var(--surface) 56%, transparent); box-shadow: inset 0 1px 0 rgb(255 255 255 / 62%), 0 0 0 1px color-mix(in srgb, var(--ink) 9%, transparent); }
.custom-tag-list { display: flex; max-height: 126px; flex-wrap: wrap; gap: 7px; overflow-y: auto; }
.custom-tag-pill { display: inline-flex; min-height: 34px; align-items: center; border: 1px solid color-mix(in srgb, var(--ink) 10%, transparent); border-radius: 999px; background: color-mix(in srgb, var(--surface-muted) 58%, transparent); color: var(--ink-soft); overflow: hidden; }
.custom-tag-pill.selected { border-color: color-mix(in srgb, var(--accent) 34%, transparent); background: color-mix(in srgb, var(--accent) 12%, var(--surface)); }
.custom-tag-select { display: inline-flex; min-height: 34px; align-items: center; gap: 4px; padding: 6px 9px; border: 0; background: transparent; color: inherit; cursor: pointer; font: inherit; font-size: 12px; }
.custom-tag-select small { color: var(--muted); font-size: 10px; }
.custom-tag-manage { display: grid; width: 28px; min-height: 34px; place-items: center; padding: 0; border: 0; border-left: 1px solid color-mix(in srgb, var(--ink) 8%, transparent); background: transparent; color: var(--muted); cursor: pointer; }
.custom-tag-manage.danger { color: var(--danger); }
.tag-picker-error { margin: 0; color: var(--danger); font-size: 12px; }
.tag-picker-footer { display: flex; justify-content: flex-end; gap: 8px; }
.tag-picker-footer .el-button { min-width: 82px; min-height: 38px; margin: 0; border-radius: 14px; }
.tag-picker-enter-active, .tag-picker-leave-active { transition: opacity 180ms ease; }
.tag-picker-enter-active .tag-picker, .tag-picker-leave-active .tag-picker { transition: transform 220ms cubic-bezier(.22, 1, .36, 1), opacity 180ms ease; }
.tag-picker-enter-from, .tag-picker-leave-to { opacity: 0; }
.tag-picker-enter-from .tag-picker, .tag-picker-leave-to .tag-picker { opacity: 0; transform: translateY(16px) scale(.98); }
@media (max-width: 560px) { .tag-picker { padding: 14px; border-radius: 24px; } .tag-picker-tabs { grid-template-columns: repeat(2, minmax(0, 1fr)); } .tag-picker-header h2 { font-size: 17px; } .tag-picker-footer .el-button { flex: 1; } }
@media (prefers-reduced-motion: reduce) { .tag-picker, .tag-picker-enter-active, .tag-picker-leave-active { transition: none; } }
</style>
