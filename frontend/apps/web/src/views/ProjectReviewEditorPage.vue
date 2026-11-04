<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import AppHeader from '../components/AppHeader.vue'
import ContentZoom from '../components/ContentZoom.vue'
import { api } from '../api/client'
import { ApiError, type ProjectReview } from '../api/types'

const { t } = useI18n()
const route = useRoute()
const id = computed(() => String(route.params.id || ''))
const record = ref<ProjectReview | null>(null)
const title = ref('')
const content = ref('')
const loading = ref(true)
const failed = ref(false)
const state = ref<'saved' | 'unsaved' | 'saving' | 'error'>('saved')
const errorMessage = ref('')
let hydrating = true
let active = true
let change = 0
let timer: ReturnType<typeof setTimeout> | undefined
let flight: Promise<boolean> | null = null

/** 独立文档只按自身 revision 保存，修改原策划不会覆盖此草稿。 */
async function load() {
  loading.value = true; failed.value = false; hydrating = true
  try {
    const result = await api.review(id.value)
    if (!active) return
    record.value = result; title.value = result.title; content.value = result.content ?? ''; state.value = 'saved'; errorMessage.value = ''
  } catch { if (active) failed.value = true }
  finally { if (active) { loading.value = false; hydrating = false } }
}
watch([title, content], () => {
  if (hydrating || !record.value) return
  change++; state.value = 'unsaved'
  if (timer) clearTimeout(timer)
  timer = setTimeout(() => { void save() }, 750)
}, { flush: 'sync' })
async function save(): Promise<boolean> {
  if (flight) return flight
  if (!record.value) return false
  if (timer) { clearTimeout(timer); timer = undefined }
  const revision = record.value.revision; const version = change
  state.value = 'saving'; errorMessage.value = ''
  flight = (async () => {
    try {
      const result = await api.saveReview(id.value, { title: title.value, content: content.value, revision })
      if (!active) return false
      record.value = result; state.value = version === change ? 'saved' : 'unsaved'
      if (version !== change) { if (timer) clearTimeout(timer); timer = setTimeout(() => { void save() }, 100) }
      return true
    } catch (error) {
      if (active) { state.value = 'error'; errorMessage.value = t(error instanceof ApiError && error.status === 409 ? 'reviewEditor.conflict' : 'reviewEditor.saveError') }
      return false
    } finally { flight = null }
  })()
  return flight
}
async function reload() {
  if (state.value !== 'saved') { try { await ElMessageBox.confirm(t('reviewEditor.discard'), t('reviewEditor.reload'), { confirmButtonText: t('reviewEditor.reload'), cancelButtonText: t('common.cancel') }) } catch { return } }
  if (timer) { clearTimeout(timer); timer = undefined }
  await load()
}
function beforeUnload(event: BeforeUnloadEvent) { if (state.value !== 'saved') { event.preventDefault(); event.returnValue = '' } }
onMounted(() => { window.addEventListener('beforeunload', beforeUnload); void load() })
onBeforeUnmount(() => { active = false; if (timer) clearTimeout(timer); window.removeEventListener('beforeunload', beforeUnload) })
onBeforeRouteLeave(async () => {
  if (loading.value || failed.value) return true
  while (state.value !== 'saved' || timer) { if (flight) await flight; else if (!await save()) return false; if (state.value === 'error') return false }
  return true
})
</script>

<template>
  <AppHeader />
  <ContentZoom><main class="review-editor-main gallery-main">
    <header><RouterLink class="el-button" to="/project-reviews"><el-icon><ArrowLeft /></el-icon>{{ t('reviewEditor.back') }}</RouterLink><RouterLink v-if="record" :to="`/projects/${record.project_id}`">{{ t('reviewEditor.source') }}</RouterLink></header>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <div v-else-if="failed" role="alert"><p>{{ t('reviewEditor.loadError') }}</p><el-button @click="load">{{ t('common.retry') }}</el-button></div>
    <template v-else-if="record">
      <label for="review-title">{{ t('reviewEditor.title') }}</label><el-input id="review-title" v-model="title" maxlength="600" />
      <label for="review-content">{{ t('reviewEditor.content') }}</label><el-input id="review-content" v-model="content" type="textarea" :rows="18" maxlength="100000" resize="vertical" />
      <footer><span role="status">{{ t(`reviewEditor.state.${state}`) }}</span><el-button :loading="state === 'saving'" @click="save">{{ t('reviewEditor.save') }}</el-button><el-button :disabled="state === 'saving'" @click="reload">{{ t('reviewEditor.reload') }}</el-button></footer>
      <p v-if="errorMessage" class="operation-error" role="alert">{{ errorMessage }}</p>
    </template>
  </main></ContentZoom>
</template>

<style scoped>
.review-editor-main { padding-top: 32px; padding-bottom: 104px; min-height: 100svh; }
header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; margin-bottom: 32px; }
header > a:last-child { color: var(--accent); }
label { display: block; margin: 24px 0 12px; color: var(--muted); font-size: 14px; }
.review-editor-main :deep(textarea), .review-editor-main :deep(input) { font-size: 16px; line-height: 1.7; }
footer { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-top: 24px; }
footer span { margin-right: auto; color: var(--muted); font-size: 13px; }
.review-editor-main :deep(.el-button) { min-height: 44px; }
.operation-error { color: var(--danger); font-size: 14px; }
</style>
