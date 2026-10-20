<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Plus, Search, UploadFilled } from '@element-plus/icons-vue'
import { api } from '../../api/client'
import type { InspirationRecord } from '../../api/types'
import { createProjectInspiration } from '../createProjectInspiration'
import { projectUploadLimit } from '../projectUploadLimit'
import InspirationPickerImage from './InspirationPickerImage.vue'

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; select: [item: InspirationRecord] }>()
const { t } = useI18n({ useScope: 'global' })
const query = ref('')
const items = ref<InspirationRecord[]>([])
const page = ref(1)
const pageSize = 24
const total = ref(0)
const loading = ref(false)
const creating = ref(false)
const maxUploadBytes = ref(50 * 1024 * 1024)
const fileInput = ref<HTMLInputElement | null>(null)
let requestVersion = 0
let searchTimer: number | undefined

/** 靈感圖按已有图库 API 搜索分页，不把图片内容复制进项目文档。 */
async function load(reset = false) {
  if (reset) page.value = 1
  const version = ++requestVersion
  loading.value = true
  try {
    const result = await api.inspirations(page.value, pageSize, { q: query.value })
    if (version !== requestVersion) return
    items.value = result.items
    total.value = result.total
  } catch { if (version === requestVersion) ElMessage.error(t('projects.loadError')) }
  finally { if (version === requestVersion) loading.value = false }
}

function choose(item: InspirationRecord) { emit('select', item); emit('update:modelValue', false) }

/** 新图仍按灵感图库的元数据和压缩创建流程保存，再将新记录插入文档。 */
async function createFromFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || creating.value) return
  if (file.size > maxUploadBytes.value) { ElMessage.error(t('projects.fileTooLarge')); return }
  creating.value = true
  try {
    const created = await createProjectInspiration(file)
    choose(created)
  } catch { ElMessage.error(t('common.errors.invalid_image')) }
  finally { creating.value = false }
}

watch(query, () => {
  if (searchTimer) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => { void load(true) }, 220)
})
watch(() => props.modelValue, open => { if (open) { query.value = ''; void load(true); void projectUploadLimit().then(value => { maxUploadBytes.value = value }) } })
watch(page, () => { void load() })
onBeforeUnmount(() => { requestVersion += 1; if (searchTimer) window.clearTimeout(searchTimer) })
</script>

<template>
  <el-dialog :model-value="modelValue" width="min(760px, calc(100vw - 24px))" top="6vh" :title="t('projects.pickerTitle')" destroy-on-close @close="emit('update:modelValue', false)">
    <div class="inspiration-picker-toolbar">
      <el-input v-model="query" clearable :placeholder="t('projects.pickerSearch')"><template #prefix><el-icon><Search /></el-icon></template></el-input>
      <el-button :icon="Plus" :loading="creating" @click="fileInput?.click()">{{ t('projects.createInspiration') }}</el-button>
      <input ref="fileInput" class="picker-file-input" type="file" accept="image/jpeg,image/png,image/webp,image/gif,image/heic,image/heif" @change="createFromFile" />
    </div>
    <p class="picker-create-hint">{{ t('projects.createInspirationHint') }}</p>
    <div v-if="loading" class="picker-state">{{ t('common.loading') }}</div>
    <div v-else-if="items.length" class="inspiration-picker-grid">
      <button v-for="item in items" :key="item.business_id" class="inspiration-picker-card" type="button" @click="choose(item)">
        <InspirationPickerImage :storekey="item.image_key" :label="item.file_name" />
        <span>{{ item.file_name || item.tag_names?.join(' · ') || t('projects.select') }}</span>
      </button>
    </div>
    <div v-else class="picker-state">{{ t('projects.pickerEmpty') }}</div>
    <template #footer><div class="picker-footer"><span>{{ total }} / {{ page }} {{ t('projects.select') }}</span><div><el-button :disabled="page <= 1" @click="page -= 1">‹</el-button><el-button :disabled="page * pageSize >= total" @click="page += 1">›</el-button></div></div></template>
  </el-dialog>
</template>

<style scoped>
.inspiration-picker-toolbar { display: flex; align-items: center; gap: 10px; }
.inspiration-picker-toolbar .el-input { flex: 1; }
.picker-file-input { display: none; }
.picker-create-hint { margin: 8px 0 13px; color: var(--muted); font-size: 11px; }
.inspiration-picker-grid { display: grid; max-height: 54vh; overflow: auto; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 10px; padding: 2px; }
.inspiration-picker-card { min-width: 0; padding: 5px; border: 1px solid var(--line); border-radius: 8px; background: var(--surface); color: var(--ink); text-align: left; cursor: pointer; }
.inspiration-picker-card:hover { border-color: var(--accent); }
.inspiration-picker-card > span:last-child { display: block; overflow: hidden; padding: 6px 2px 2px; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.picker-state { display: grid; min-height: 170px; place-items: center; color: var(--muted); }
.picker-footer { display: flex; justify-content: space-between; align-items: center; color: var(--muted); font-size: 11px; }
@media (max-width: 600px) { .inspiration-picker-toolbar { align-items: stretch; flex-direction: column; } .inspiration-picker-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
</style>
