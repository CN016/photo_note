<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Files, More, Plus, Search, UploadFilled } from '@element-plus/icons-vue'
import AppHeader from '../components/AppHeader.vue'
import ProjectCoverImage from '../components/ProjectCoverImage.vue'
import { api } from '../api/client'
import type { ProjectSaveInput, ProjectSummary } from '../api/types'
import { projectUploadLimit } from '../projects/projectUploadLimit'

const { t } = useI18n({ useScope: 'global' })
const router = useRouter()
const records = ref<ProjectSummary[]>([])
const query = ref('')
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
const fileInput = ref<HTMLInputElement | null>(null)
const emojiPool = ['✨', '🌿', '🪴', '🌈', '🌙', '🧩', '🪄', '🪷', '🧸', '🌻', '🪐', '📚', '🎨', '🎧', '🧵', '☕']
let searchTimer: number | undefined

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

/** 服务端分页检索项目；输入停顿后再发起请求，标题与正文使用同一个 q。 */
async function loadProjects() {
  loading.value = true
  failed.value = false
  try {
    const result = await api.projects(page.value, pageSize, query.value)
    records.value = result.items
    total.value = result.total
  } catch { failed.value = true }
  finally { loading.value = false }
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
onMounted(() => { void loadProjects(); void projectUploadLimit().then(value => { maxUploadBytes.value = value }) })
onBeforeUnmount(() => {
  if (searchTimer) window.clearTimeout(searchTimer)
  if (coverPreview.value) URL.revokeObjectURL(coverPreview.value)
})
</script>

<template>
  <AppHeader />
  <main class="projects-main gallery-main">
    <header class="projects-heading">
      <div><p class="eyebrow"><el-icon><Files /></el-icon>{{ t('projects.title') }}</p><h1>{{ t('projects.title') }}</h1><p>{{ t('projects.subtitle') }}</p></div>
      <el-button class="projects-new" type="primary" :icon="Plus" @click="openCreate">{{ t('projects.newProject') }}</el-button>
    </header>
    <div class="projects-search"><el-input v-model="query" clearable :placeholder="t('projects.searchPlaceholder')"><template #prefix><el-icon><Search /></el-icon></template></el-input></div>
    <div v-if="loading" class="projects-status" role="status">{{ t('common.loading') }}</div>
    <div v-else-if="failed" class="projects-status projects-error"><p>{{ t('projects.loadError') }}</p><el-button @click="loadProjects">{{ t('common.retry') }}</el-button></div>
    <section v-else-if="records.length" class="projects-grid" :aria-label="t('projects.title')">
      <article v-for="record in records" :key="record.business_id" class="project-card">
        <RouterLink class="project-card-link" :to="`/projects/${record.business_id}`">
          <ProjectCoverImage :storekey="record.cover_image_key" :emoji="record.cover_emoji" :label="record.title" />
          <span class="project-card-copy"><strong>{{ record.title }}</strong><small>{{ new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date(record.updated_at)) }}</small></span>
        </RouterLink>
        <el-dropdown trigger="click" @command="removeProject(record)">
          <el-button text :loading="deletingID === record.business_id" :aria-label="t('projects.deleteProject')"><el-icon><More /></el-icon></el-button>
          <template #dropdown><el-dropdown-menu><el-dropdown-item command="delete"><el-icon><Delete /></el-icon>{{ t('projects.deleteProject') }}</el-dropdown-item></el-dropdown-menu></template>
        </el-dropdown>
      </article>
    </section>
    <section v-else class="projects-empty"><el-icon><Files /></el-icon><h2>{{ t('projects.emptyTitle') }}</h2><p>{{ t('projects.emptyDescription') }}</p><el-button type="primary" :icon="Plus" @click="openCreate">{{ t('projects.newProject') }}</el-button></section>
    <div v-if="total > pageSize" class="projects-pagination">
      <el-button :disabled="page <= 1" @click="page -= 1">‹</el-button><span>{{ page }} / {{ Math.ceil(total / pageSize) }}</span><el-button :disabled="page >= Math.ceil(total / pageSize)" @click="page += 1">›</el-button>
    </div>
  </main>

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
.projects-main { min-height: 100svh; padding-top: 48px; padding-bottom: 90px; }
.projects-heading { display: flex; align-items: flex-end; justify-content: space-between; gap: 20px; margin-bottom: 25px; }
.projects-heading h1 { margin: 7px 0 8px; color: var(--ink); font-size: clamp(31px, 4vw, 44px); letter-spacing: -.045em; }
.projects-heading > div > p:last-child { margin: 0; color: var(--muted); font-size: 13px; }
.projects-heading .eyebrow { display: flex; align-items: center; gap: 7px; margin: 0; color: var(--accent-hover); font-size: 11px; font-weight: 700; }
.projects-new { min-height: 42px; border-radius: 7px; }
.projects-search { max-width: 520px; margin: 20px 0 22px; }
.projects-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.project-card { display: flex; min-width: 0; align-items: center; gap: 8px; padding: 7px 10px 7px 14px; border: 1px solid var(--line); border-radius: 9px; background: var(--surface); transition: border-color .15s, transform .15s; }
.project-card:hover { transform: translateY(-1px); border-color: var(--accent); }
.project-card-link { display: flex; min-width: 0; flex: 1; align-items: center; gap: 14px; padding: 7px 0; color: inherit; text-decoration: none; }
.project-card-copy { display: grid; min-width: 0; gap: 5px; }
.project-card-copy strong { overflow: hidden; color: var(--ink); font-size: 14px; text-overflow: ellipsis; white-space: nowrap; }
.project-card-copy small { color: var(--muted); font-size: 11px; }
.project-card-arrow { margin-left: auto; color: var(--muted); }
.projects-status, .projects-empty { display: grid; min-height: 220px; justify-items: center; align-content: center; gap: 10px; color: var(--muted); text-align: center; }
.projects-error p, .projects-empty p { margin: 0; }
.projects-empty > .el-icon { font-size: 36px; color: var(--accent); }
.projects-empty h2 { margin: 0; color: var(--ink); font-size: 18px; }
.projects-pagination { display: flex; justify-content: center; align-items: center; gap: 15px; margin-top: 24px; color: var(--muted); }
.project-cover-optional { display: block; margin-top: 3px; color: var(--muted); font-size: 11px; font-weight: 400; }
.project-cover-select { display: flex; min-height: 92px; align-items: center; gap: 16px; }
.project-cover-select > img, .project-cover-emoji { display: grid; width: 74px; height: 74px; place-items: center; border-radius: 9px; background: var(--surface-muted); object-fit: cover; font-size: 36px; }
.project-cover-select > div { display: flex; flex-wrap: wrap; gap: 8px; }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0,0,0,0); }
@media (max-width: 768px) { .projects-main { padding-top: 27px; padding-bottom: 105px; } .projects-heading { align-items: center; } .projects-heading h1 { font-size: 34px; } .projects-heading > div > p:last-child { max-width: 235px; font-size: 11px; line-height: 1.5; } .projects-new { width: 43px; min-width: 43px; height: 43px; padding: 0; font-size: 0; } .projects-new .el-icon { font-size: 19px; } .projects-grid { grid-template-columns: 1fr; gap: 8px; } .project-card { gap: 5px; padding: 6px 8px 6px 10px; } .project-card-link { gap: 11px; } }
</style>
