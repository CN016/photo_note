<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PortfolioPhoto, TagRecord } from '../api/types'
import { api } from '../api/client'
import { formatMetadataValue, rawMetadataValue, gpsMapUrl } from '../views/imageMetadataDisplay'
const props = defineProps<{ photos: PortfolioPhoto[]; tags: Pick<TagRecord, 'business_id' | 'content'>[]; namespace: 'portfolio' | 'publicPortfolio'; editable?: boolean; busy?: boolean }>()
const emit = defineEmits<{ save: [photo: PortfolioPhoto, done: () => void]; remove: [photo: PortfolioPhoto] }>()
const { t, te, locale } = useI18n()
const tr = (key: string, args?: Record<string, unknown>) => t(`${props.namespace}.${key}`, args ?? {})
const tag = ref('')
const layout = ref<'grid' | 'masonry'>('grid')
const selected = ref<PortfolioPhoto | null>(null)
const draft = ref<PortfolioPhoto | null>(null)
const filtered = computed(() => tag.value ? props.photos.filter(photo => photo.tag_ids.includes(tag.value)) : props.photos)
const dimensions = [ ['focal_length_mm', 'focal'], ['aperture_f_number', 'aperture'], ['exposure_time_seconds', 'shutter'], ['iso_speed', 'iso'] ] as const
/** 仅统计存在且有效的拍摄参数，缺失项不参与对应分组计数。 */
const counts = computed(() => dimensions.map(([field, label]) => {
  const counts = new Map<string, number>()
  for (const photo of props.photos) {
    const value = photo.image_metadata?.[field]
    if (typeof value !== 'number' || !Number.isFinite(value)) continue
    const formatted = formatMetadataValue(field, value, t, locale.value)
    counts.set(formatted, (counts.get(formatted) ?? 0) + 1)
  }
  return { field, label: tr(label), values: [...counts.entries()].sort((a,b) => b[1] - a[1]) }
}))
/** 使用当前页面语言翻译已知字段，未知字段保留原名。 */
function metadataLabel(key: string) { const path = `${props.namespace}.metadataFields.${key}`; return te(path) ? t(path) : key }
function metadataValue(key: string, value: unknown) { return typeof value === 'object' && value !== null ? rawMetadataValue(value) : formatMetadataValue(key, value, t, locale.value) }
const mapURL = computed(() => gpsMapUrl(selected.value?.image_metadata?.gps_latitude, selected.value?.image_metadata?.gps_longitude))
function closeDialog(value: boolean) { if (!value) selected.value = null }
/** 查看时复制一份编辑草稿，取消不会修改当前列表。 */
function open(photo: PortfolioPhoto) { selected.value = photo; draft.value = { ...photo, tag_ids: [...photo.tag_ids] } }
/** 保存成功才关闭弹窗，失败保留草稿供用户重试。 */
function submit() { if (draft.value) emit('save', draft.value, () => { selected.value = null }) }
</script>
<template>
  <div class="portfolio-toolbar">
    <el-select v-model="tag" :placeholder="tr('all')" :aria-label="tr('filter')"><el-option :label="tr('all')" value="" /><el-option v-for="entry in tags" :key="entry.business_id" :label="entry.content" :value="entry.business_id" /></el-select>
    <el-radio-group v-model="layout" :aria-label="tr('view')"><el-radio-button value="grid">{{ tr('grid') }}</el-radio-button><el-radio-button value="masonry">{{ tr('masonry') }}</el-radio-button></el-radio-group>
  </div>
  <p v-if="!filtered.length">{{ photos.length ? tr('noResults') : tr('noPhotos') }}</p>
  <div v-else :class="['portfolio-photo-grid', { 'portfolio-masonry': layout === 'masonry' }]">
    <button v-for="photo in filtered" :key="photo.business_id" class="portfolio-photo" type="button" @click="open(photo)"><img :src="api.portfolioPhotoURL(photo.image_key)" :alt="photo.title" loading="lazy" /><span>{{ photo.title || tr('preview') }}</span></button>
  </div>
  <details class="portfolio-analysis" :aria-label="tr('statistics')">
    <summary>{{ tr('statistics') }}</summary>
    <div class="portfolio-stats"><section v-for="group in counts" :key="group.field"><h3>{{ group.label }}</h3><ul v-if="group.values.length"><li v-for="[value, count] in group.values" :key="value"><span>{{ value }}</span><b>{{ count }}</b></li></ul><p v-else>{{ tr('unknown') }}</p></section></div>
  </details>
  <el-dialog :model-value="!!selected" @update:model-value="closeDialog" :title="tr('photoDetails')" width="min(760px, 94vw)" :close-on-click-modal="!busy" :close-on-press-escape="!busy" :show-close="!busy" destroy-on-close>
    <template v-if="selected && draft">
      <img class="portfolio-preview" :src="api.portfolioPhotoURL(selected.image_key)" :alt="selected.title" />
      <div v-if="editable" class="portfolio-photo-edit">
        <label>{{ tr('photoTitle') }}<el-input v-model="draft.title" maxlength="255" /></label>
        <label>{{ tr('photoTags') }}<el-select v-model="draft.tag_ids" multiple filterable><el-option v-for="entry in tags" :key="entry.business_id" :label="entry.content" :value="entry.business_id" /></el-select></label>
        <label>{{ tr('order') }}<el-input-number v-model="draft.sort_order" :step="1" /></label>
        <el-button type="primary" :loading="busy" @click="submit">{{ tr('savePhoto') }}</el-button>
        <el-button type="danger" plain :disabled="busy" @click="emit('remove', selected); selected = null">{{ tr('removePhoto') }}</el-button>
      </div>
      <a v-if="mapURL" :href="mapURL" target="_blank" rel="noopener noreferrer">{{ tr('openMap') }}</a>
      <dl class="portfolio-metadata"><div v-for="(value, key) in selected.image_metadata" :key="key"><template v-if="value !== null && value !== ''"><dt>{{ metadataLabel(String(key)) }}</dt><dd><details v-if="typeof value === 'object'"><summary>{{ tr('rawDetails') }}</summary><pre>{{ metadataValue(String(key), value) }}</pre></details><template v-else>{{ metadataValue(String(key), value) }}</template></dd></template></div></dl>
    </template>
  </el-dialog>
</template>
<style scoped>
.portfolio-analysis { margin: 24px 0; } .portfolio-analysis h2 { font-size: 18px; } .portfolio-stats { display: grid; grid-template-columns: repeat(auto-fit,minmax(155px,1fr)); gap: 12px; } .portfolio-stats section { background: var(--el-fill-color-light); border-radius: 10px; padding: 12px; min-width: 0; } .portfolio-stats h3 { margin: 0 0 8px; font-size: 14px; } .portfolio-stats ul { list-style: none; padding: 0; margin: 0; max-height: 130px; overflow:auto; } .portfolio-stats li { display:flex; justify-content:space-between; gap:8px; } .portfolio-toolbar { display:flex; flex-wrap:wrap; gap:12px; margin:20px 0; } .portfolio-toolbar .el-select { width: 190px; } .portfolio-photo-grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(180px,1fr)); gap:14px; } .portfolio-photo { display:block; width:100%; padding:0; overflow:hidden; background:var(--el-fill-color-light); border:1px solid var(--el-border-color); border-radius:10px; text-align:left; cursor:pointer; color:var(--el-text-color-primary); } .portfolio-photo:focus-visible { outline:2px solid var(--el-color-primary); outline-offset:2px; } .portfolio-photo img { display:block; width:100%; aspect-ratio:1; object-fit:cover; } .portfolio-photo span { display:block; padding:10px; overflow-wrap:anywhere; } .portfolio-masonry { display:block; columns:180px; column-gap:14px; } .portfolio-masonry .portfolio-photo { break-inside:avoid; margin-bottom:14px; } .portfolio-masonry img { aspect-ratio:auto; } .portfolio-preview { display:block; max-width:100%; max-height:60vh; margin:auto; object-fit:contain; } .portfolio-photo-edit { display:flex; gap:12px; flex-wrap:wrap; align-items:end; margin:16px 0; } .portfolio-photo-edit label { display:grid; gap:6px; min-width:150px; } .portfolio-metadata { max-height:260px; overflow:auto; } .portfolio-metadata div { display:grid; grid-template-columns:minmax(0,1fr) minmax(0,2fr); gap:10px; padding:5px 0; } .portfolio-metadata dt,.portfolio-metadata dd { overflow-wrap:anywhere; margin:0; } .portfolio-metadata pre { white-space:pre-wrap; font:inherit; margin:0; }

.portfolio-analysis{border-top:1px solid var(--el-border-color-lighter);padding-top:18px}.portfolio-analysis summary{cursor:pointer;width:fit-content;padding:12px 0;font-weight:600}.portfolio-analysis summary:focus-visible{outline:2px solid var(--el-color-primary)}.portfolio-stats{margin-top:12px}.portfolio-toolbar{justify-content:space-between}.portfolio-photo-grid{grid-template-columns:repeat(auto-fill,minmax(220px,1fr));gap:20px}.portfolio-photo-edit{display:grid;grid-template-columns:1fr 1fr}.portfolio-photo-edit label{min-width:0}.portfolio-photo-edit :deep(.el-select){width:100%}.portfolio-photo-edit :deep(.el-button){margin:0;min-height:44px}@media(max-width:600px){.portfolio-photo-grid{grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.portfolio-photo-edit{grid-template-columns:1fr}.portfolio-toolbar{gap:8px}.portfolio-toolbar .el-select{width:100%}}
</style>
