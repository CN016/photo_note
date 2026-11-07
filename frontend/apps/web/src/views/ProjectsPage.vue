<script setup lang="ts">
import ContentZoom from '../components/ContentZoom.vue'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { onBeforeRouteLeave, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, ArrowRight, Delete, Files, More, Search, UploadFilled } from '@element-plus/icons-vue'
import AppHeader from '../components/AppHeader.vue'
import CreateFab from '../components/CreateFab.vue'
import ProjectCoverImage from '../components/ProjectCoverImage.vue'
import { api } from '../api/client'
import type { PageResult, ProjectSaveInput, ProjectSummary, ProjectStatus, ProjectStatusCounts } from '../api/types'
import { projectUploadLimit } from '../projects/projectUploadLimit'

const { t, locale } = useI18n({ useScope: 'global' })
const router = useRouter()
const records = ref<ProjectSummary[]>([])
const query = ref('')
const activeStatus = ref<ProjectStatus | ''>('')
const statuses: ProjectStatus[] = ['unfinished', 'executed', 'completed']
const statusCounts = ref<ProjectStatusCounts>({ unfinished: 0, executed: 0, completed: 0 })
const page = ref(1)
const pageSize = 20
const total = ref(0)
const loading = ref(true)
const failed = ref(false)
const dialogOpen = ref(false)
const saving = ref(false)
const title = ref('')
const coverEmoji = ref('')
const coverKey = ref('')
const coverFileName = ref('')
const coverPreview = ref('')
const coverLoading = ref(false)
const maxUploadBytes = ref(50 * 1024 * 1024)
const deletingID = ref('')
onBeforeRouteLeave(to => to.path !== '/spaces' || (!dialogOpen.value && !saving.value && !coverLoading.value && !deletingID.value))
const fileInput = ref<HTMLInputElement | null>(null)
const emojiPool = ['✨', '🌿', '🪴', '🌈', '🌙', '🧩', '🪄', '🪷', '🧸', '🌻', '🪐', '📚', '🎨', '🎧', '🧵', '☕']
let searchTimer: number | undefined
let loadGeneration = 0

const emptyDocument = { type: 'doc', content: [{ type: 'paragraph' }] }

/** 每次新建自动从精选 emoji 中选一项，用户上传封面后才清空该字段。 */
function openCreate() {
  title.value = ''
  coverEmoji.value = emojiPool[Math.floor(Math.random() * emojiPool.length)]
  coverKey.value = ''
  coverFileName.value = ''
  if (coverPreview.value) URL.revokeObjectURL(coverPreview.value)
  coverPreview.value = ''
  dialogOpen.value = true
}

/** 以服务端压缩接口上传可选封面，页面仅保留 storekey 和短期预览 URL。 */
async function selectCover(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if ((event.target as HTMLInputElement).value) (event.target as HTMLInputElement).value = ''
  if (!file) return
  if (file.size > maxUploadBytes.value) { ElMessage.error(t('projects.fileTooLarge')); return }
  coverLoading.value = true
  try {
    const result = await api.uploadProjectCover(file)
    if (coverPreview.value) URL.revokeObjectURL(coverPreview.value)
    coverPreview.value = URL.createObjectURL(file)
    coverKey.value = result.storekey
    coverFileName.value = result.file_name
    coverEmoji.value = ''
  } catch { ElMessage.error(t('projects.saveError')) }
  finally { coverLoading.value = false }
}

function removeCover() {
  if (coverPreview.value) URL.revokeObjectURL(coverPreview.value)
  coverPreview.value = ''
  coverKey.value = ''
  coverFileName.value = ''
  coverEmoji.value = emojiPool[Math.floor(Math.random() * emojiPool.length)]
}

/** 新建操作先保存一个版本化空文档，再进入自动保存编辑器。 */
async function createProject() {
  if (!title.value.trim() || saving.value) return
  saving.value = true
  try {
    const input: ProjectSaveInput = {
      title: title.value.trim(), cover_emoji: coverEmoji.value, cover_image_key: coverKey.value,
      cover_file_name: coverFileName.value, document_schema_version: 1, document: emptyDocument, revision: 1,
    }
    const project = await api.createProject(input)
    dialogOpen.value = false
    await router.push(`/projects/${project.business_id}`)
  } catch { ElMessage.error(t('projects.saveError')) }
  finally { saving.value = false }
}

/** 服务端分页检索项目；兼容旧版 Go 导出的首字母大写字段，异常响应进入可重试错误态。 */
async function loadProjects() {
  const generation = ++loadGeneration
  loading.value = true
  failed.value = false
  try {
    const result = await api.projects(page.value, pageSize, query.value, activeStatus.value) as PageResult<ProjectSummary> & { Items?: ProjectSummary[]; Total?: number; status_counts?: ProjectStatusCounts }
    if (generation !== loadGeneration) return
    const items = Array.isArray(result?.items) ? result.items : result?.Items
    const resultTotal = typeof result?.total === 'number' ? result.total : result?.Total
    if (!Array.isArray(items) || typeof resultTotal !== 'number' || !Number.isFinite(resultTotal)) throw new Error('invalid_projects_response')
    records.value = items
    total.value = resultTotal
    statusCounts.value = result.status_counts ?? { unfinished: 0, executed: 0, completed: 0 }
  } catch { if (generation === loadGeneration) { records.value = []; total.value = 0; failed.value = true } }
  finally { if (generation === loadGeneration) loading.value = false }
}

/** 列表删除经确认后调用项目软删除接口；服务端保留历史灵感图和附件引用关系。 */
async function removeProject(item: ProjectSummary) {
  try {
    await ElMessageBox.confirm(t('projects.deleteConfirm'), t('projects.deleteConfirmTitle'), {
      confirmButtonText: t('projects.deleteProject'), cancelButtonText: t('projects.cancel'), type: 'warning',
    })
  } catch { return }
  deletingID.value = item.business_id
  try { await api.deleteProject(item.business_id); ElMessage.success(t('projects.deleteConfirmed')); await loadProjects() }
  catch { ElMessage.error(t('projects.saveError')) }
  finally { deletingID.value = '' }
}

watch(query, () => {
  page.value = 1
  if (searchTimer) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => { void loadProjects() }, 250)
})
watch(page, () => { void loadProjects() })
watch(activeStatus, () => { if (page.value !== 1) page.value = 1; else void loadProjects() })
onMounted(() => { void loadProjects(); void projectUploadLimit().then(value => { maxUploadBytes.value = value }) })
onBeforeUnmount(() => {
  loadGeneration++
  if (searchTimer) window.clearTimeout(searchTimer)
  if (coverPreview.value) URL.revokeObjectURL(coverPreview.value)
})
</script>

<template>
  <AppHeader />
  <ContentZoom><main id="main-content" class="projects-main gallery-main">
    <header class="projects-heading">
      <h1>{{ t('projects.title') }}</h1>
      <div class="projects-heading-actions"><RouterLink class="el-button" to="/project-reviews">{{ t('projects.workflow.allReviews') }}</RouterLink></div>
    </header>
    <div class="projects-search"><el-input v-model="query" clearable :placeholder="t('projects.searchPlaceholder')"><template #prefix><el-icon><Search /></el-icon></template></el-input></div>
    <div class="project-status-filters" role="group" :aria-label="t('projects.filterStatus')"><button type="button" :aria-pressed="activeStatus === ''" @click="activeStatus = ''">{{ t('projects.allStatuses') }}</button><button v-for="status in statuses" :key="status" type="button" :class="`status-${status}`" :aria-pressed="activeStatus === status" @click="activeStatus = status"><span class="status-dot" aria-hidden="true" />{{ t(`projects.status.${status}`) }}<span class="status-count">{{ new Intl.NumberFormat(locale).format(statusCounts[status]) }}</span></button></div>
    <div v-if="loading" class="projects-status" role="status">{{ t('common.loading') }}</div>
    <div v-else-if="failed" class="projects-status projects-error"><p>{{ t('projects.loadError') }}</p><el-button @click="loadProjects">{{ t('common.retry') }}</el-button></div>
    <section v-else-if="records.length" class="projects-grid" :aria-label="t('projects.title')">
      <article v-for="record in records" :key="record.business_id" class="project-card">
        <RouterLink class="project-card-link" :to="`/projects/${record.business_id}`">
          <ProjectCoverImage :storekey="record.cover_image_key" :emoji="record.cover_emoji" :label="record.title" />
          <span class="project-card-copy"><strong>{{ record.title }}</strong><small>{{ new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(new Date(record.updated_at)) }}</small><small class="execution-label" :class="`status-${record.status}`"><span class="status-dot" aria-hidden="true" />{{ t(`projects.status.${record.status}`) }}</small></span>
        </RouterLink>
        <el-dropdown trigger="click" @command="removeProject(record)">
          <el-button text :loading="deletingID === record.business_id" :aria-label="t('projects.deleteProject')"><el-icon><More /></el-icon></el-button>
          <template #dropdown><el-dropdown-menu><el-dropdown-item command="delete"><el-icon><Delete /></el-icon>{{ t('projects.deleteProject') }}</el-dropdown-item></el-dropdown-menu></template>
        </el-dropdown>
      </article>
    </section>
    <section v-else class="projects-empty"><el-icon><Files /></el-icon><h2>{{ t('projects.emptyTitle') }}</h2></section>
    <div v-if="total > pageSize" class="projects-pagination">
      <el-button :icon="ArrowLeft" :title="t('projects.viewer.previous')" :aria-label="t('projects.viewer.previous')" :disabled="loading || page <= 1" @click="page -= 1" /><span>{{ page }} / {{ Math.ceil(total / pageSize) }}</span><el-button :icon="ArrowRight" :title="t('projects.viewer.next')" :aria-label="t('projects.viewer.next')" :disabled="loading || page >= Math.ceil(total / pageSize)" @click="page += 1" />
    </div>
  </main></ContentZoom>
  <CreateFab :label="t('projects.newProject')" @select="openCreate" />

  <el-dialog v-model="dialogOpen" class="project-create-dialog" width="min(480px, calc(100vw - 30px))" :title="t('projects.createTitle')" destroy-on-close>
    <el-form label-position="top" @submit.prevent="createProject">
      <el-form-item :label="t('projects.titleLabel')" required><el-input v-model="title" maxlength="120" show-word-limit autofocus :placeholder="t('projects.titlePlaceholder')" @keyup.enter="createProject" /></el-form-item>
      <el-form-item><template #label><span>{{ t('projects.coverLabel') }}</span><small class="project-cover-optional">{{ t('projects.coverOptional') }}</small></template>
        <div class="project-cover-select">
          <img v-if="coverPreview" :src="coverPreview" alt="" />
          <span v-else class="project-cover-emoji">{{ coverEmoji }}</span>
          <div><el-button :icon="UploadFilled" :loading="coverLoading" @click="fileInput?.click()">{{ coverKey ? t('projects.changeCover') : t('projects.chooseCover') }}</el-button><el-button v-if="coverKey" text @click="removeCover">{{ t('common.remove') }}</el-button></div>
        </div>
        <input ref="fileInput" class="sr-only" type="file" accept="image/jpeg,image/png,image/webp,image/gif" @change="selectCover" />
      </el-form-item>
    </el-form>
    <template #footer><el-button @click="dialogOpen = false">{{ t('projects.cancel') }}</el-button><el-button type="primary" :loading="saving" :disabled="!title.trim() || coverLoading" @click="createProject">{{ t('projects.create') }}</el-button></template>
  </el-dialog>
</template>

<style scoped>
/* 界面表面与控件使用全局主题令牌；图片、文档和用户自选封面颜色保持原数据。 */
.projects-main { min-height: 100svh; padding-top: 48px; padding-bottom: 92px; }
.projects-heading-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.project-status-filters { display: flex; flex-wrap: wrap; gap: 8px; margin: 16px 0 24px; }
.project-status-filters button { display: inline-flex; align-items: center; gap: 8px; min-height: 44px; padding: 8px 12px; font: inherit; font-size: 14px; background: transparent; color: var(--ink); border: 1px solid transparent; border-radius: 8px; cursor: pointer; }
.project-status-filters button:hover { background: var(--surface-muted); }
.project-status-filters button[aria-pressed=true] { background: var(--surface); border-color: var(--line); }
.project-status-filters button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.status-dot { display: inline-block; width: 6px; height: 6px; flex-shrink: 0; border-radius: 50%; background: currentColor; }
.status-count { color: var(--muted); font-variant-numeric: tabular-nums; }
.execution-label { display: inline-flex; align-items: center; gap: 6px; }
.status-unfinished { color: var(--muted) !important; }
.status-executed { color: var(--danger) !important; }
.status-completed { color: var(--success) !important; }
.projects-heading { display: flex; flex-wrap: wrap; align-items: flex-end; justify-content: space-between; gap: 20px; margin-bottom: 24px; }
.projects-heading h1 { margin: 8px 0; color: var(--ink); font-size: clamp(28px, 3vw, 36px); font-weight: 500; letter-spacing: 0; }
.projects-heading > div > p:last-child { margin: 0; color: var(--muted); font-size: 13px; }
.projects-heading .eyebrow { display: flex; align-items: center; gap: 8px; margin: 0; color: var(--accent-hover); font-size: 12px; font-weight: 600; }
.projects-new { min-height: 42px; border-radius: var(--radius-control); }
.projects-search { max-width: 520px; margin: 20px 0 24px; }
.projects-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.project-card { display: flex; min-width: 0; align-items: center; gap: 8px; padding: 8px 12px 8px 16px; border: 1px solid var(--line); border-radius: var(--radius-card); background: var(--surface); box-shadow: var(--soft-shadow); transition: border-color .18s, box-shadow .18s; }
.project-card:hover { border-color: var(--accent); }
.project-card-link { display: flex; min-width: 0; flex: 1; align-items: center; gap: 16px; padding: 8px 0; color: inherit; text-decoration: none; }
.project-card-copy { display: grid; min-width: 0; gap: 4px; }
.project-card-copy strong { overflow: hidden; color: var(--ink); font-size: 14px; text-overflow: ellipsis; white-space: nowrap; }
.project-card-copy small { color: var(--muted); font-size: 12px; }
.project-card-arrow { margin-left: auto; color: var(--muted); }
.projects-status, .projects-empty { display: grid; min-height: 220px; justify-items: center; align-content: center; gap: 12px; color: var(--muted); text-align: center; }
.projects-error p, .projects-empty p { margin: 0; }
.projects-empty > .el-icon { font-size: 36px; color: var(--accent); }
.projects-empty h2 { margin: 0; color: var(--ink); font-size: 18px; }
.projects-pagination { display: flex; justify-content: center; align-items: center; gap: 16px; margin-top: 24px; color: var(--muted); }
.project-cover-optional { display: block; margin-top: 3px; color: var(--muted); font-size: 12px; font-weight: 400; }
.project-cover-select { display: flex; min-height: 92px; align-items: center; gap: 16px; }
.project-cover-select > img, .project-cover-emoji { display: grid; width: 74px; height: 74px; place-items: center; border-radius: var(--radius-control); background: var(--surface-muted); object-fit: cover; font-size: 36px; }
.project-cover-select > div { display: flex; flex-wrap: wrap; gap: 8px; }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0,0,0,0); }
@media (max-width: 768px) { .projects-main { padding-top: 20px; padding-bottom: 104px; } .projects-heading { align-items: center; margin-bottom: 14px; } .projects-heading h1 { font-size: 32px; } .projects-heading > div > p:last-child { max-width: 235px; font-size: 12px; line-height: 1.5; } .projects-new { width: 44px; min-width: 44px; height: 44px; padding: 0; font-size: 0; } .projects-new :deep(.el-icon) { font-size: 20px; } .projects-search { margin: 12px 0; } .project-status-filters { flex-wrap: nowrap; gap: 4px; margin: 0 0 16px; overflow-x: auto; padding-bottom: 2px; scrollbar-width: none; } .project-status-filters button { flex: 0 0 auto; padding: 8px 10px; } .projects-grid { grid-template-columns: 1fr; gap: 8px; } .project-card { gap: 4px; padding: 8px 8px 8px 12px; } .project-card-link { gap: 12px; } }
@media (max-width: 768px) { .project-card :deep(.el-button), .projects-pagination :deep(.el-button) { min-width: 44px; min-height: 44px; } }
</style>
