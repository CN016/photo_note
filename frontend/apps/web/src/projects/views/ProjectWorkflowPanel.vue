<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Calendar, Document, Picture, Refresh } from '@element-plus/icons-vue'
import { api } from '../../api/client'
import { ApiError, type PortfolioRecord, type ProjectRecord, type ProjectReview, type ProjectScheduleLink, type ScheduleOccurrence } from '../../api/types'

const props = defineProps<{
  source: ProjectRecord
  summary: string
  analysis: string
  readOnly: boolean
  saving: boolean
  flush: () => Promise<boolean>
}>()
const emit = defineEmits<{
  'update:summary': [value: string]
  'update:analysis': [value: string]
  busy: [value: boolean]
}>()
const { t, locale } = useI18n()
const link = ref<ProjectScheduleLink>({ schedule_id: props.source.schedule_id, owned: props.source.schedule_owned })
const review = ref<ProjectReview | null>(null)
const linkedPortfolios = ref<PortfolioRecord[]>([])
const sideError = ref(false)
const scheduleBusy = ref(false)
const scheduleError = ref('')
const reviewBusy = ref(false)
const portfolioBusy = ref(false)
const generating = ref(false)
const preview = ref('')
const aiError = ref('')
const candidates = ref<ScheduleOccurrence[]>([])
const candidatesOpen = ref(false)
const selectedSchedule = ref('')
const portfolioDialog = ref(false)
const portfolioOptions = ref<PortfolioRecord[]>([])
const selectedPortfolios = ref<string[]>([])
const portfolioLoading = ref(false)
const portfolioReady = ref(false)
let active = true
let attemptedShooting = ''
let autoTimer: ReturnType<typeof setTimeout> | undefined
let generationController: AbortController | undefined
const busy = computed(() => scheduleBusy.value || reviewBusy.value || portfolioBusy.value)
watch(busy, value => emit('busy', value), { flush: 'sync' })
watch(() => props.source.revision, () => {
  if (!scheduleBusy.value) link.value = { schedule_id: props.source.schedule_id, owned: props.source.schedule_owned }
})

/** 只读取独立关系数据，失败保留重试入口；卸载后丢弃迟到响应。 */
async function loadRelations() {
  sideError.value = false
  const result = await Promise.allSettled([api.projectReview(props.source.business_id), api.projectPortfolios(props.source.business_id)])
  if (!active) return
  if (result[0].status === 'fulfilled') review.value = result[0].value
  else sideError.value = true
  if (result[1].status === 'fulfilled') linkedPortfolios.value = result[1].value.items
  else sideError.value = true
}

/** 完整的已保存拍摄信息才允许同步；失败后同一份信息不反复后台重试。 */
const shootingComplete = computed(() => {
  const s = props.source.shooting
  return Boolean(s?.start_at && s.end_at && s.location.trim() && s.person.trim() && s.kind)
})
watch(() => [props.source.shooting, props.saving, props.readOnly, busy.value] as const, () => {
  if (autoTimer) clearTimeout(autoTimer)
  if (props.readOnly || props.saving || busy.value || !shootingComplete.value || link.value.schedule_id && !link.value.owned) return
  const fingerprint = JSON.stringify(props.source.shooting)
  if (fingerprint === attemptedShooting) return
  if (props.source.schedule_sync_state === 'failed') { scheduleError.value = t('projects.workflow.scheduleFailed'); attemptedShooting = fingerprint; return }
  if (props.source.schedule_sync_state === 'linked') { attemptedShooting = fingerprint; return }
  autoTimer = setTimeout(() => { if (active) void syncSchedule() }, 400)
}, { immediate: true })

/** 自动关联前刷新候选；用户选择另建或关联后，服务端仍在事务中重新验证。 */
async function syncSchedule(selection?: string, createNew = false) {
  if (busy.value || props.readOnly) return
  scheduleBusy.value = true
  scheduleError.value = ''
  attemptedShooting = JSON.stringify(props.source.shooting)
  try {
    const beforeFlush = props.source.revision
    if (!await props.flush() || !active) return
    if (props.source.revision !== beforeFlush) link.value = { schedule_id: props.source.schedule_id, owned: props.source.schedule_owned }
    attemptedShooting = JSON.stringify(props.source.shooting)
    if (!link.value.schedule_id && !selection && !createNew) {
      const result = await api.projectScheduleCandidates(props.source.business_id)
      if (!active) return
      if (result.items.length) { candidates.value = result.items; selectedSchedule.value = ''; candidatesOpen.value = true; return }
    }
    const result = await api.syncProjectSchedule(props.source.business_id, { revision: props.source.revision, ...(selection ? { schedule_id: selection } : {}), ...(createNew ? { create_new: true } : {}) })
    if (!active) return
    link.value = result
    candidatesOpen.value = false
  } catch (error) {
    if (!active) return
    scheduleError.value = t('projects.workflow.scheduleFailed')
    if (error instanceof ApiError && error.code === 'project_schedule_candidates') attemptedShooting = ''
  } finally { if (active) scheduleBusy.value = false }
}

/** 解除关系只更改策划关联，不删除档期；之后由用户明确重新同步。 */
async function detachSchedule() {
  if (busy.value || props.readOnly) return
  try { await ElMessageBox.confirm(t('projects.workflow.detachConfirm'), t('projects.workflow.detach'), { confirmButtonText: t('projects.workflow.detach'), cancelButtonText: t('projects.cancel') }) } catch { return }
  scheduleBusy.value = true
  try {
    if (!await props.flush() || !active) return
    const result = await api.syncProjectSchedule(props.source.business_id, { revision: props.source.revision, detach: true })
    if (!active) return
    link.value = result; scheduleError.value = ''; attemptedShooting = JSON.stringify(props.source.shooting)
  } catch { if (active) scheduleError.value = t('projects.workflow.scheduleFailed') }
  finally { if (active) scheduleBusy.value = false }
}

/** 先排空自动保存，再复制已确认的人工总结；重复点击由服务端返回同一份独立文档。 */
async function completeReview() {
  if (busy.value || props.readOnly || !props.summary.trim()) return
  reviewBusy.value = true
  try {
    if (!await props.flush() || !active) return
    const result = await api.completeProjectReview(props.source.business_id, props.source.revision)
    if (active) { review.value = result; ElMessage.success(t('projects.workflow.reviewCreated')) }
  } catch { if (active) ElMessage.error(t('projects.saveError')) }
  finally { if (active) reviewBusy.value = false }
}

/** 外部 AI 仅返回预览；用户采用前不会替换已有分析，也不会影响人工总结。 */
async function generateAnalysis() {
  if (generating.value || busy.value || props.readOnly) return
  generating.value = true; aiError.value = ''
  generationController = new AbortController()
  try {
    if (!await props.flush() || !active) return
    const result = await api.generateProjectAnalysis(props.source.business_id, props.source.revision, locale.value, generationController.signal)
    if (active) preview.value = result.analysis
  } catch (error) {
    if (!active) return
    const code = error instanceof ApiError ? error.code : ''
    aiError.value = t(code === 'project_ai_unconfigured' ? 'projects.workflow.aiUnconfigured' : code === 'project_ai_invalid_config' ? 'projects.workflow.aiInvalidConfig' : code === 'project_ai_busy' ? 'projects.workflow.aiBusy' : code === 'project_ai_timeout' ? 'projects.workflow.aiTimeout' : 'projects.workflow.aiFailed')
  } finally { if (active) generating.value = false }
}
async function adoptAnalysis() {
  if (!preview.value || props.readOnly) return
  if (props.analysis.trim()) {
    try { await ElMessageBox.confirm(t('projects.workflow.replaceAnalysis'), t('projects.workflow.adopt'), { confirmButtonText: t('projects.workflow.adopt'), cancelButtonText: t('projects.cancel') }) } catch { return }
  }
  emit('update:analysis', preview.value); preview.value = ''
}

/** 每次打开关联选择器都从持久化关系恢复，取消草稿不会影响当前关联。 */
async function openPortfolioPicker() {
  portfolioDialog.value = true; portfolioLoading.value = true; portfolioReady.value = false
  try {
    const [options, links] = await Promise.all([api.portfolios(), api.projectPortfolios(props.source.business_id)])
    if (active) { portfolioOptions.value = options.items; linkedPortfolios.value = links.items; selectedPortfolios.value = links.items.map(item => item.business_id); portfolioReady.value = true }
  }
  catch { if (active) ElMessage.error(t('projects.loadError')) }
  finally { if (active) portfolioLoading.value = false }
}
async function savePortfolios() {
  if (busy.value || !portfolioReady.value) return
  portfolioBusy.value = true
  try {
    const result = await api.setProjectPortfolios(props.source.business_id, selectedPortfolios.value)
    if (active) { linkedPortfolios.value = result.items; portfolioDialog.value = false }
  } catch { if (active) ElMessage.error(t('projects.saveError')) }
  finally { if (active) portfolioBusy.value = false }
}
function candidateTime(item: ScheduleOccurrence) {
  const format = new Intl.DateTimeFormat(locale.value, { timeZone: 'Asia/Shanghai', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  return `${format.format(new Date(item.start_at))} – ${format.format(new Date(item.end_at))}`
}
onMounted(() => { void loadRelations() })
onBeforeUnmount(() => { active = false; if (autoTimer) clearTimeout(autoTimer); generationController?.abort() })
</script>

<template>
  <div class="workflow-panel">
    <section class="workflow-section schedule-section" :aria-label="t('projects.workflow.schedule')">
      <div class="section-heading"><h2><el-icon><Calendar /></el-icon>{{ t('projects.workflow.schedule') }}</h2><RouterLink to="/schedule">{{ t('projects.workflow.openSchedule') }}</RouterLink></div>
      <div class="section-actions">
        <span v-if="link.schedule_id" class="relation-label">{{ t('projects.workflow.linked') }}</span>
        <el-button v-if="!readOnly && shootingComplete && (!link.schedule_id || link.owned)" :loading="scheduleBusy" :disabled="busy && !scheduleBusy" @click="syncSchedule()">{{ t(link.schedule_id ? 'projects.workflow.syncSchedule' : 'projects.workflow.linkSchedule') }}</el-button>
        <el-button v-if="!readOnly && link.schedule_id" text :disabled="busy" @click="detachSchedule">{{ t('projects.workflow.detach') }}</el-button>
      </div>
      <p v-if="scheduleError" class="operation-error" role="alert">{{ scheduleError }}</p>
    </section>

    <section class="workflow-section" :aria-label="t('projects.manualSummary')">
      <div class="section-heading"><h2><el-icon><Document /></el-icon>{{ t('projects.manualSummary') }}</h2><RouterLink to="/project-reviews">{{ t('projects.workflow.allReviews') }}</RouterLink></div>
      <el-input :model-value="summary" type="textarea" :rows="6" maxlength="100000" :disabled="readOnly" :aria-label="t('projects.manualSummary')" @update:model-value="emit('update:summary', $event)" />
      <div class="section-actions">
        <RouterLink v-if="review" class="el-button" :to="`/project-reviews/${review.business_id}`">{{ t('projects.workflow.openReview') }}</RouterLink>
        <el-button v-else-if="!readOnly" :loading="reviewBusy" :disabled="!summary.trim() || busy && !reviewBusy" @click="completeReview">{{ t('projects.workflow.completeReview') }}</el-button>
      </div>
    </section>

    <section class="workflow-section" :aria-label="t('projects.workflow.aiTitle')">
      <div class="section-heading"><h2>{{ t('projects.workflow.aiTitle') }}</h2><el-tooltip :content="t('projects.workflow.aiSendHint')"><el-button v-if="!readOnly" :loading="generating" :disabled="busy" :icon="Refresh" @click="generateAnalysis">{{ t('projects.workflow.generate') }}</el-button></el-tooltip></div>
      <el-input :model-value="analysis" type="textarea" :rows="5" maxlength="100000" :disabled="readOnly" :aria-label="t('projects.workflow.aiTitle')" @update:model-value="emit('update:analysis', $event)" />
      <p v-if="aiError" class="operation-error" role="alert">{{ aiError }}</p>
      <div v-if="preview" class="analysis-preview"><h3>{{ t('projects.workflow.preview') }}</h3><div class="preview-text">{{ preview }}</div><div class="section-actions"><el-button type="primary" :disabled="readOnly" @click="adoptAnalysis">{{ t('projects.workflow.adopt') }}</el-button><el-button @click="preview = ''">{{ t('projects.cancel') }}</el-button></div></div>
    </section>

    <section class="workflow-section" :aria-label="t('projects.workflow.portfolios')">
      <div class="section-heading"><h2><el-icon><Picture /></el-icon>{{ t('projects.workflow.portfolios') }}</h2><el-button v-if="!readOnly" :disabled="busy" @click="openPortfolioPicker">{{ t('projects.workflow.managePortfolios') }}</el-button></div>
      <div class="portfolio-links"><RouterLink v-for="item in linkedPortfolios" :key="item.business_id" :to="`/portfolios/${item.business_id}`">{{ item.title }}</RouterLink></div>
    </section>
    <div v-if="sideError" class="section-actions operation-error" role="alert">{{ t('projects.workflow.relationsFailed') }}<el-button @click="loadRelations">{{ t('common.retry') }}</el-button></div>
  </div>

  <el-dialog v-model="candidatesOpen" :title="t('projects.workflow.chooseSchedule')" width="min(640px, 94vw)" :close-on-click-modal="!scheduleBusy" :show-close="!scheduleBusy" :close-on-press-escape="!scheduleBusy">
    <el-radio-group v-model="selectedSchedule" class="schedule-candidates" :disabled="scheduleBusy"><el-radio v-for="item in candidates" :key="`${item.business_id}:${item.start_at}`" :value="item.business_id"><span class="candidate-copy"><strong>{{ item.title }}</strong><small>{{ candidateTime(item) }}</small><small v-if="item.content">{{ item.content }}</small></span></el-radio></el-radio-group>
    <template #footer><el-button :disabled="scheduleBusy" @click="candidatesOpen = false">{{ t('projects.cancel') }}</el-button><el-button :loading="scheduleBusy" @click="syncSchedule(undefined, true)">{{ t('projects.workflow.createSeparate') }}</el-button><el-button type="primary" :loading="scheduleBusy" :disabled="!selectedSchedule" @click="syncSchedule(selectedSchedule)">{{ t('projects.workflow.confirmLink') }}</el-button></template>
  </el-dialog>
  <el-dialog v-model="portfolioDialog" :title="t('projects.workflow.managePortfolios')" width="min(560px, 94vw)" :close-on-click-modal="!portfolioBusy" :close-on-press-escape="!portfolioBusy" :show-close="!portfolioBusy">
    <el-select v-model="selectedPortfolios" multiple filterable :loading="portfolioLoading" :disabled="portfolioBusy || portfolioLoading" :aria-label="t('projects.workflow.portfolios')" style="width: 100%"><el-option v-for="item in portfolioOptions" :key="item.business_id" :label="item.title" :value="item.business_id" /></el-select>
    <template #footer><el-button :disabled="portfolioBusy" @click="portfolioDialog = false">{{ t('projects.cancel') }}</el-button><el-button type="primary" :disabled="portfolioLoading || !portfolioReady" :loading="portfolioBusy" @click="savePortfolios">{{ t('projects.workflow.saveLinks') }}</el-button></template>
  </el-dialog>
</template>

<style scoped>
.workflow-panel { margin-top: 32px; }
.workflow-section { padding: 24px 0; border-top: 1px solid var(--line); }
.section-heading { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
h2 { display: flex; align-items: center; gap: 8px; margin: 0; font-size: 18px; font-weight: 500; }
h3 { margin: 0 0 12px; font-size: 15px; font-weight: 500; }
.section-heading a, .portfolio-links a { color: var(--accent); font-size: 14px; }
.section-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 12px; }
.section-actions :deep(.el-button) { margin-left: 0; }
.relation-label { font-size: 14px; color: var(--muted); }
.operation-error { color: var(--danger); font-size: 14px; margin-bottom: 0; }
.analysis-preview { padding: 20px; margin-top: 16px; background: var(--surface-muted); border: 1px solid var(--line); border-radius: 12px; }
.preview-text { white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.7; max-height: 480px; overflow-y: auto; }
.portfolio-links { display: flex; flex-wrap: wrap; gap: 8px 20px; }
.portfolio-links a { padding: 12px 0; }
.schedule-candidates { display: flex; align-items: stretch; flex-direction: column; gap: 8px; width: 100%; max-height: 50vh; overflow-y: auto; }
.schedule-candidates :deep(.el-radio) { height: auto; min-height: 64px; margin: 0; padding: 12px; border: 1px solid var(--line); border-radius: 8px; }
.candidate-copy { display: flex; flex-direction: column; gap: 4px; white-space: normal; overflow-wrap: anywhere; }
.candidate-copy strong { font-weight: 500; }
.candidate-copy small { color: var(--muted); font-size: 13px; white-space: pre-wrap; }
@media (max-width: 768px) { .workflow-section { padding: 24px 0; } .workflow-panel :deep(.el-button) { min-height: 44px; } .workflow-panel :deep(textarea) { font-size: 16px; } }
</style>
