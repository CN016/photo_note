<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChatDotRound, Picture, Plus, Search, Setting, User } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { api } from '../api/client'
import type { ConsoleConfigRecord, ConsoleFeedbackRecord, ConsoleInspirationRecord, ConsoleUserRecord } from '../api/types'
import AppHeader from '../components/AppHeader.vue'
import AuthenticatedImageThumbnail from '../components/AuthenticatedImageThumbnail.vue'
import InspirationThumbnail from '../components/InspirationThumbnail.vue'
import { useAuthStore } from '../stores/auth'

const { t, locale } = useI18n()
const auth = useAuthStore()
const activeTab = ref<'users' | 'configs' | 'inspirations' | 'feedback'>('users')
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const users = ref<ConsoleUserRecord[]>([])
const configs = ref<ConsoleConfigRecord[]>([])
const inspirations = ref<ConsoleInspirationRecord[]>([])
const feedbackItems = ref<ConsoleFeedbackRecord[]>([])
const userSearch = ref('')
const inspirationSearch = ref('')
const inspirationSort = ref<'created_desc' | 'created_asc' | 'updated_desc'>('created_desc')
const feedbackStatus = ref<'' | 'new' | 'processed'>('')
const userPage = ref(1)
const inspirationPage = ref(1)
const feedbackPage = ref(1)
const userTotal = ref(0)
const inspirationTotal = ref(0)
const feedbackTotal = ref(0)
const pageSize = 20
const roleBusy = ref('')
const configDialog = ref(false)
const editingConfig = ref<ConsoleConfigRecord | null>(null)
const configForm = reactive({ key: '', value: '', description: '', is_public: false, is_secret: false, clear_value: false })
const previewDialog = ref(false)
const previewLoading = ref(false)
const previewURL = ref('')
const previewLabel = ref('')
let searchTimer: number | undefined
let previewRequest = 0

const currentPage = computed(() => activeTab.value === 'users' ? userPage.value : activeTab.value === 'inspirations' ? inspirationPage.value : activeTab.value === 'feedback' ? feedbackPage.value : 1)
const currentTotal = computed(() => activeTab.value === 'users' ? userTotal.value : activeTab.value === 'inspirations' ? inspirationTotal.value : activeTab.value === 'feedback' ? feedbackTotal.value : 0)

/** 日期按界面语言格式化，并在无效历史值时保留原始信息。 */
function displayDate(value: string): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return value
  return new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: 'short', day: 'numeric' }).format(date)
}

/** 每个列表在服务端分页；加载失败时保留当前内容并提供可见错误提示。 */
async function loadUsers() {
  loading.value = true
  error.value = ''
  try {
    const result = await api.consoleUsers(userPage.value, pageSize, userSearch.value)
    users.value = result.items
    userTotal.value = result.total
  } catch { error.value = t('console.loadError') }
  finally { loading.value = false }
}

/** 配置数量通常较少，按 key 排序一次读取并由后端隐藏秘密原值。 */
async function loadConfigs() {
  loading.value = true
  error.value = ''
  try { configs.value = (await api.consoleConfigs()).items }
  catch { error.value = t('console.loadError') }
  finally { loading.value = false }
}

/** 全站灵感图只读检索使用后端文件名/昵称查询和白名单排序。 */
async function loadInspirations() {
  loading.value = true
  error.value = ''
  try {
    const result = await api.consoleInspirations(inspirationPage.value, pageSize, inspirationSearch.value, inspirationSort.value)
    inspirations.value = result.items
    inspirationTotal.value = result.total
  } catch { error.value = t('console.loadError') }
  finally { loading.value = false }
}

/** 反馈列表只公开处理所需资料和附件 key，可按新反馈或已处理状态过滤。 */
async function loadFeedback() {
  loading.value = true
  error.value = ''
  try {
    const result = await api.consoleFeedbacks(feedbackPage.value, pageSize, feedbackStatus.value)
    feedbackItems.value = result.items
    feedbackTotal.value = result.total
  } catch { error.value = t('console.loadError') }
  finally { loading.value = false }
}

/** 切换分区时只读取当前需要的数据，避免初次进入同时请求全部后台内容。 */
function loadActiveSection() {
  if (activeTab.value === 'users') return loadUsers()
  if (activeTab.value === 'configs') return loadConfigs()
  if (activeTab.value === 'inspirations') return loadInspirations()
  return loadFeedback()
}

/** 输入检索词时合并短时间内的键入，分页条件由后端执行。 */
watch(userSearch, () => {
  if (activeTab.value !== 'users') return
  if (searchTimer !== undefined) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => { userPage.value = 1; void loadUsers() }, 220)
})

/** 灵感图查询词或排序变化后回到首屏，避免展示旧筛选的后续页。 */
watch([inspirationSearch, inspirationSort], () => {
  if (activeTab.value !== 'inspirations') return
  if (searchTimer !== undefined) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => { inspirationPage.value = 1; void loadInspirations() }, 220)
})

/** 反馈筛选变化时从第一页读取符合状态的记录。 */
watch(feedbackStatus, () => {
  if (activeTab.value !== 'feedback') return
  feedbackPage.value = 1
  void loadFeedback()
})

watch(activeTab, () => { void loadActiveSection() })
onMounted(() => { void loadUsers() })

/** 更新账号角色并同步当前账号的角色副本，后端仍会独立校验每个请求。 */
async function toggleUserRole(user: ConsoleUserRecord) {
  roleBusy.value = user.business_id
  error.value = ''
  try {
    const result = await api.consoleSetUserRole(user.business_id, !user.is_admin)
    users.value = users.value.map(item => item.business_id === user.business_id ? { ...item, is_admin: result.is_admin } : item)
    if (auth.user?.business_id === user.business_id) auth.user = await api.currentUser()
  } catch { error.value = t('console.saveError') }
  finally { roleBusy.value = '' }
}

/** 复用同一对话框新增或编辑配置，秘密值输入框永远不填入现存值。 */
function openConfigEditor(item?: ConsoleConfigRecord) {
  editingConfig.value = item ?? null
  configForm.key = item?.key ?? ''
  configForm.value = item && !item.is_secret ? item.value ?? '' : ''
  configForm.description = item?.description ?? ''
  configForm.is_public = item?.is_public ?? false
  configForm.is_secret = item?.is_secret ?? false
  configForm.clear_value = false
  configDialog.value = true
}

/** 新建配置选择秘密类型后立即取消公开读取，减少无效状态。 */
function handleSecretToggle(value: string | number | boolean) {
  if (value) configForm.is_public = false
}

/** 保存时只对秘密配置发送新输入或显式清除标记，绝不回显旧秘密。 */
async function saveConfig() {
  if (configForm.is_secret && configForm.is_public) {
    error.value = t('console.secretPublicConflict')
    return
  }
  saving.value = true
  error.value = ''
  try {
    if (editingConfig.value) {
      const patch: { value?: string; clear_value?: boolean; description?: string; is_public?: boolean } = {
        description: configForm.description,
        is_public: configForm.is_public,
      }
      if (editingConfig.value.is_secret) {
        if (configForm.value.trim()) patch.value = configForm.value
        if (configForm.clear_value) patch.clear_value = true
      } else patch.value = configForm.value
      await api.consoleUpdateConfig(editingConfig.value.key, patch)
      ElMessage.success(t('console.configSaved'))
    } else {
      await api.consoleCreateConfig({
        key: configForm.key,
        value: configForm.value,
        description: configForm.description,
        is_public: configForm.is_public,
        is_secret: configForm.is_secret,
      })
      ElMessage.success(t('console.configCreated'))
    }
    configDialog.value = false
    await loadConfigs()
  } catch { error.value = t('console.saveError') }
  finally { saving.value = false }
}

/** 标记反馈处理状态后同步本页记录，保持反馈正文和图片为只读。 */
async function setFeedbackStatus(item: ConsoleFeedbackRecord) {
  saving.value = true
  error.value = ''
  const status = item.status === 'new' ? 'processed' : 'new'
  try {
    await api.consoleUpdateFeedbackStatus(item.business_id, status)
    feedbackItems.value = feedbackItems.value.map(record => record.business_id === item.business_id ? { ...record, status } : record)
  } catch { error.value = t('console.saveError') }
  finally { saving.value = false }
}

/** 预览图片通过鉴权接口读取，并在关闭预览或切换图片时释放 blob 地址。 */
async function openPreview(storekey: string, label: string) {
  const version = ++previewRequest
  if (previewURL.value) URL.revokeObjectURL(previewURL.value)
  previewURL.value = ''
  previewLabel.value = label
  previewLoading.value = true
  previewDialog.value = true
  try {
    const blob = await api.download(storekey)
    if (version !== previewRequest) return
    previewURL.value = URL.createObjectURL(blob)
  } catch { error.value = t('console.loadError') }
  finally { if (version === previewRequest) previewLoading.value = false }
}

/** 对话框关闭后撤销本次临时图片地址，避免大图长期驻留内存。 */
function closePreview() {
  previewRequest += 1
  previewDialog.value = false
  previewLoading.value = false
  if (previewURL.value) URL.revokeObjectURL(previewURL.value)
  previewURL.value = ''
}

/** 切换页码并重新读取当前分区。 */
function changePage(delta: number) {
  const next = currentPage.value + delta
  if (next < 1 || (next - 1) * pageSize >= currentTotal.value) return
  if (activeTab.value === 'users') userPage.value = next
  else if (activeTab.value === 'inspirations') inspirationPage.value = next
  else feedbackPage.value = next
  void loadActiveSection()
}

onBeforeUnmount(() => {
  if (searchTimer !== undefined) window.clearTimeout(searchTimer)
  if (previewURL.value) URL.revokeObjectURL(previewURL.value)
})
</script>

<template>
  <div class="workspace-page console-page">
    <AppHeader />
    <main class="console-main">
      <header class="console-heading">
        <div>
          <p class="eyebrow">{{ t('common.product') }}</p>
          <h1>{{ t('console.title') }}</h1>
          <p>{{ t('console.subtitle') }}</p>
        </div>
        <div class="console-heading-mark" aria-hidden="true"><el-icon><Setting /></el-icon></div>
      </header>

      <el-tabs v-model="activeTab" class="console-tabs">
        <el-tab-pane name="users"><template #label><span class="console-tab-label"><el-icon><User /></el-icon>{{ t('console.users') }}</span></template></el-tab-pane>
        <el-tab-pane name="configs"><template #label><span class="console-tab-label"><el-icon><Setting /></el-icon>{{ t('console.configurations') }}</span></template></el-tab-pane>
        <el-tab-pane name="inspirations"><template #label><span class="console-tab-label"><el-icon><Picture /></el-icon>{{ t('console.inspirations') }}</span></template></el-tab-pane>
        <el-tab-pane name="feedback"><template #label><span class="console-tab-label"><el-icon><ChatDotRound /></el-icon>{{ t('console.feedback') }}</span></template></el-tab-pane>
      </el-tabs>

      <div v-if="error" class="inline-error" role="alert"><span>{{ error }}</span><el-button text @click="loadActiveSection">{{ t('console.search') }}</el-button></div>
      <div v-if="loading" class="console-loading" role="status"><span class="loading-dot"></span>{{ t('console.loading') }}</div>

      <section v-if="activeTab === 'users'" class="console-section">
        <div class="console-toolbar">
          <el-input v-model="userSearch" class="console-search" clearable :placeholder="t('console.searchUsers')">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </div>
        <div v-if="users.length" class="console-user-list">
          <article v-for="user in users" :key="user.business_id" class="console-user-card">
            <div class="console-user-identity"><span class="console-avatar">{{ user.nickname.slice(0, 1) }}</span><div><strong>{{ user.nickname }}</strong><span>{{ user.phone }}</span></div></div>
            <div class="console-user-role"><span class="console-role-pill" :class="{ 'is-privileged': user.is_admin }">{{ user.is_admin ? t('console.administrator') : t('console.member') }}</span><small>{{ t('console.joinedAt') }} · {{ displayDate(user.created_at) }}</small></div>
            <el-button :loading="roleBusy === user.business_id" :type="user.is_admin ? 'default' : 'primary'" plain @click="toggleUserRole(user)">{{ user.is_admin ? t('console.revoke') : t('console.grant') }}</el-button>
          </article>
        </div>
        <div v-else-if="!loading" class="console-empty">{{ t('console.noUsers') }}</div>
      </section>

      <section v-else-if="activeTab === 'configs'" class="console-section">
        <div class="console-toolbar console-toolbar-spread"><p>{{ t('console.configsDescription') }}</p><el-button class="primary-button" type="primary" @click="openConfigEditor()"><el-icon><Plus /></el-icon>{{ t('console.addConfig') }}</el-button></div>
        <div v-if="configs.length" class="console-config-grid">
          <article v-for="config in configs" :key="config.business_id" class="console-config-card">
            <div class="console-config-card-heading"><div><code>{{ config.key }}</code><span class="console-config-badge" :class="{ 'is-secret': config.is_secret }">{{ config.is_secret ? t('console.secretConfig') : config.is_public ? t('console.publicConfig') : t('console.member') }}</span></div><el-button text @click="openConfigEditor(config)">{{ t('console.editConfig') }}</el-button></div>
            <p>{{ config.description }}</p>
            <div class="console-config-value"><span v-if="config.is_secret">{{ config.is_configured ? t('console.secretConfigured') : t('console.secretNotConfigured') }}</span><span v-else>{{ config.value || t('common.notSet') }}</span></div>
            <small>{{ t('console.updatedAt', { date: displayDate(config.updated_at) }) }}</small>
          </article>
        </div>
        <div v-else-if="!loading" class="console-empty">{{ t('console.noConfigs') }}</div>
      </section>

      <section v-else-if="activeTab === 'inspirations'" class="console-section">
        <div class="console-toolbar">
          <el-input v-model="inspirationSearch" class="console-search" clearable :placeholder="t('console.searchInspirations')"><template #prefix><el-icon><Search /></el-icon></template></el-input>
          <el-select v-model="inspirationSort" class="console-sort">
            <el-option :label="t('console.sortNewest')" value="created_desc" />
            <el-option :label="t('console.sortOldest')" value="created_asc" />
            <el-option :label="t('console.sortEdited')" value="updated_desc" />
          </el-select>
        </div>
        <div v-if="inspirations.length" class="console-inspiration-grid">
          <article v-for="item in inspirations" :key="item.business_id" class="console-inspiration-card">
            <button class="console-inspiration-image" type="button" :aria-label="t('console.openImage', { name: item.file_name })" @click="openPreview(item.image_key, item.file_name)"><InspirationThumbnail :record="item" /></button>
            <div class="console-inspiration-caption"><strong>{{ item.file_name || t('gallery.untitled') }}</strong><span>{{ t('console.submittedBy', { name: item.owner_nickname }) }}</span><small>{{ displayDate(item.created_at) }}</small></div>
          </article>
        </div>
        <div v-else-if="!loading" class="console-empty">{{ t('console.noInspirations') }}</div>
      </section>

      <section v-else class="console-section">
        <div class="console-toolbar"><el-select v-model="feedbackStatus" class="console-status-filter"><el-option :label="t('console.allStatuses')" value="" /><el-option :label="t('console.newStatus')" value="new" /><el-option :label="t('console.processedStatus')" value="processed" /></el-select></div>
        <div v-if="feedbackItems.length" class="console-feedback-list">
          <article v-for="item in feedbackItems" :key="item.business_id" class="console-feedback-card">
            <header><div><span class="console-role-pill" :class="{ 'is-privileged': item.status === 'processed' }">{{ item.status === 'new' ? t('console.newStatus') : t('console.processedStatus') }}</span><span>{{ t('console.submittedBy', { name: item.nickname }) }}</span></div><time>{{ displayDate(item.created_at) }}</time></header>
            <p class="console-feedback-content">{{ item.content }}</p>
            <div v-if="item.image_keys.length" class="console-feedback-attachments"><small>{{ t('console.attachments') }}</small><div><AuthenticatedImageThumbnail v-for="(key, index) in item.image_keys" :key="`${key}-${index}`" :storekey="key" shape="square" :label="`${t('console.preview')} ${index + 1}`" @preview="openPreview(key, `${t('console.preview')} ${index + 1}`)" /></div></div>
            <footer><el-button :disabled="saving" :type="item.status === 'new' ? 'primary' : 'default'" plain @click="setFeedbackStatus(item)">{{ item.status === 'new' ? t('console.markProcessed') : t('console.markNew') }}</el-button></footer>
          </article>
        </div>
        <div v-else-if="!loading" class="console-empty">{{ t('console.noFeedback') }}</div>
      </section>

      <nav v-if="currentTotal > pageSize" class="console-pagination" :aria-label="t('console.page', { page: currentPage, total: currentTotal })">
        <el-button :disabled="currentPage <= 1 || loading" @click="changePage(-1)">{{ t('console.previous') }}</el-button>
        <span>{{ t('console.page', { page: currentPage, total: currentTotal }) }}</span>
        <el-button :disabled="currentPage * pageSize >= currentTotal || loading" @click="changePage(1)">{{ t('console.next') }}</el-button>
      </nav>
    </main>

    <el-dialog v-model="configDialog" :title="editingConfig ? t('console.editConfig') : t('console.createConfig')" width="min(540px, calc(100vw - 32px))" destroy-on-close>
      <div class="console-config-form">
        <label v-if="!editingConfig"><span>{{ t('console.configKey') }}</span><el-input v-model="configForm.key" :placeholder="t('console.configKeyPlaceholder')" /></label>
        <div v-else class="console-editing-key"><span>{{ t('console.configKey') }}</span><code>{{ editingConfig.key }}</code></div>
        <label><span>{{ t('console.configDescription') }}</span><el-input v-model="configForm.description" type="textarea" :rows="3" :placeholder="t('console.configDescriptionPlaceholder')" /></label>
        <label><span>{{ t('console.configValue') }}</span><el-input v-model="configForm.value" type="textarea" :rows="3" :placeholder="editingConfig?.is_secret ? t('console.secretValuePlaceholder') : t('console.configValuePlaceholder')" /></label>
        <el-checkbox v-if="!editingConfig" v-model="configForm.is_secret" @change="handleSecretToggle">{{ t('console.secretConfig') }}</el-checkbox>
        <el-checkbox v-if="editingConfig?.is_secret" v-model="configForm.clear_value">{{ t('console.clearSecret') }}</el-checkbox>
        <el-checkbox v-if="!configForm.is_secret" v-model="configForm.is_public">{{ t('console.publicConfig') }}</el-checkbox>
        <p v-if="editingConfig?.is_secret" class="console-form-help">{{ editingConfig.is_configured ? t('console.secretConfigured') : t('console.secretNotConfigured') }}</p>
      </div>
      <template #footer><el-button @click="configDialog = false">{{ t('console.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="saveConfig">{{ t('console.save') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="previewDialog" :title="previewLabel" width="min(900px, calc(100vw - 30px))" @closed="closePreview">
      <div class="console-preview-stage">
        <span v-if="previewLoading" class="console-preview-loading"><span class="loading-dot"></span>{{ t('console.imageLoading') }}</span>
        <img v-else-if="previewURL" :src="previewURL" :alt="previewLabel" />
        <span v-else>{{ t('console.imageUnavailable') }}</span>
      </div>
      <template #footer><el-button @click="closePreview">{{ t('console.close') }}</el-button></template>
    </el-dialog>
  </div>
</template>
