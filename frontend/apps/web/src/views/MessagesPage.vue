<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, ArrowRight, Delete, Edit, Plus, TopRight } from '@element-plus/icons-vue'
import AppHeader from '../components/AppHeader.vue'
import WebhookProviderLogo from '../components/WebhookProviderLogo.vue'
import { api } from '../api/client'
import type { NoticeConfig, NoticeDelivery, NoticeMessage } from '../api/types'
import { useAuthStore } from '../stores/auth'
import { useAppShellStore } from '../stores/appShell'

const { t: translate, locale } = useI18n()
const router = useRouter()
const auth = useAuthStore()
const appShell = useAppShellStore()

/** 页面固定使用 messages 命名空间，公共通用提示仍从 common 中读取。 */
function t(key: string, parameters?: Record<string, string | number>): string {
  return translate(key.startsWith('common.') ? key : `messages.${key}`, parameters ?? {})
}

const activeTab = ref<'inbox' | 'channels'>('inbox')
const filter = ref<'all' | 'unread'>('all')
const messages = ref<NoticeMessage[]>([])
const unreadCount = ref(0)
const messageTotal = ref(0)
const currentPage = ref(1)
const loadingMessages = ref(false)
const messageError = ref(false)
const configs = ref<NoticeConfig[]>([])
const maximumConfigs = ref(20)
const loadingConfigs = ref(false)
const configError = ref(false)
const editorOpen = ref(false)
const editorBusy = ref(false)
const editingID = ref('')
const providerOptions: NoticeConfig['provider'][] = ['wecom', 'dingtalk', 'feishu']
const form = reactive({
  provider: 'wecom' as NoticeConfig['provider'],
  name: '',
  webhook_url: '',
  signing_secret: '',
  clear_signing_secret: false,
  enabled: true,
})
const testResults = ref<Record<string, NoticeDelivery>>({})
let refreshTimer: number | undefined
let mounted = false
const pageSize = 20

const hasPrevious = computed(() => currentPage.value > 1)
const hasNext = computed(() => currentPage.value * pageSize < messageTotal.value)
const configCountText = computed(() => t('webhooks.limit', { count: configs.value.length, maximum: maximumConfigs.value }))

/** 读取站内消息及未读角标；轮询刷新期间保留当前列表，避免闪烁。 */
async function loadMessages(quiet = false) {
  if (!quiet) loadingMessages.value = true
  messageError.value = false
  const businessID = auth.user?.business_id ?? ''
  const unreadCountRevision = appShell.unreadCountRevision
  try {
    const result = await api.notifications(currentPage.value, pageSize, filter.value === 'unread')
    // 旧版本 API 或异常响应可能把无投递记录序列化为 null；归一化避免单条通知导致整页崩溃。
    messages.value = (Array.isArray(result?.items) ? result.items : []).map(item => ({
      ...item,
      deliveries: Array.isArray(item?.deliveries) ? item.deliveries : [],
      payload: item?.payload && typeof item.payload === 'object' && !Array.isArray(item.payload) ? item.payload : {},
    }))
    messageTotal.value = result.total
    if (businessID) {
      const countUpdated = appShell.setUnreadCount(businessID, result.unread_count, unreadCountRevision)
      unreadCount.value = countUpdated ? result.unread_count : appShell.unreadCount
    } else {
      unreadCount.value = result.unread_count
    }
  } catch {
    messageError.value = true
  } finally {
    if (!quiet) loadingMessages.value = false
  }
}

/** 读取当前用户配置列表和系统动态上限。 */
async function loadConfigs(quiet = false) {
  if (!quiet) loadingConfigs.value = true
  configError.value = false
  try {
    const result = await api.noticeConfigs()
    configs.value = Array.isArray(result?.items) ? result.items : []
    maximumConfigs.value = result.maximum
  } catch {
    configError.value = true
  } finally {
    if (!quiet) loadingConfigs.value = false
  }
}

function selectFilter(next: 'all' | 'unread') {
  filter.value = next
  currentPage.value = 1
  void loadMessages()
}

function selectTab(next: 'inbox' | 'channels') {
  activeTab.value = next
  if (next === 'channels' && configs.value.length === 0 && !configError.value) void loadConfigs()
}

function messageTitle(item: NoticeMessage): string {
  if (item.event_type === 'inspiration.compression.succeeded') return t('compressionSucceeded')
  if (item.event_type === 'inspiration.compression.failed') return t('compressionFailed')
  if (item.event_type === 'system.error_log.created') return t('errorLogCreated')
  if (item.event_type === 'feedback.submitted') return t('feedbackSubmitted')
  if (item.event_type === 'plan.reminder.due') return t('planReminderDue')
  return t('title')
}

function messageBody(item: NoticeMessage): string {
  if (item.event_type === 'inspiration.compression.succeeded') {
    return t('version', { version: item.payload.async_version ?? 0 })
  }
  if (item.event_type === 'inspiration.compression.failed') {
    return t('version', { version: item.payload.async_version ?? 0 })
  }
  if (item.event_type === 'system.error_log.created') {
    const source = item.payload.source_file || t('errorLogUnknownSource')
    return t('errorLogBody', {
      code: item.payload.error_code || 'internal_error',
      source,
      line: item.payload.source_line ?? 0,
    })
  }
  if (item.event_type === 'feedback.submitted') return item.payload.text || t('feedbackImagesOnly')
  if (item.event_type === 'plan.reminder.due') {
    return t('planReminderBody', {
      title: item.payload.title || t('planReminderDue'),
      time: item.payload.starts_at ? new Intl.DateTimeFormat(locale.value, { timeZone: 'Asia/Shanghai', dateStyle: 'medium', timeStyle: 'short' }).format(new Date(item.payload.starts_at)) : '',
    })
  }
  return t('description')
}

/** 只展示反馈通知中已脱敏范围内的昵称、手机号或游客身份。 */
function feedbackReporterText(item: NoticeMessage): string {
  if (item.event_type !== 'feedback.submitted' || !item.payload.reporter) return ''
  const reporter = item.payload.reporter
  if (reporter.guest) return t('feedbackReporterGuest')
  const nickname = reporter.nickname?.trim()
  if (!nickname) return ''
  if (!reporter.phone) return t('feedbackReporterName', { nickname })
  return t('feedbackReporterDetails', { nickname, phone: reporter.phone })
}

/** 只渲染后端生成的 HTTPS 反馈图片链接，正文始终使用纯文本插值。 */
function feedbackImages(item: NoticeMessage): string[] {
  if (item.event_type !== 'feedback.submitted' || !Array.isArray(item.payload.image_urls)) return []
  return item.payload.image_urls.filter(url => typeof url === 'string' && url.startsWith('https://'))
}

function formatDate(value: string): string {
  return new Date(value).toLocaleString(locale.value, { dateStyle: 'medium', timeStyle: 'short' })
}

function providerName(provider: string): string {
  return t(`providers.${provider}`)
}

function deliveryStatus(delivery: NoticeDelivery): string {
  const key = `delivery.${delivery.status}`
  const translated = t(key)
  return translated === key ? delivery.status : translated
}

function deliveryError(delivery: NoticeDelivery): string {
  if (delivery.last_error_code) {
    if (delivery.last_error_code.startsWith('http_')) {
      return t('delivery.http_error', { status: delivery.last_error_code.slice('http_'.length) })
    }
    const errorKey = `delivery.${delivery.last_error_code}`
    const translated = translate(errorKey)
    if (translated !== errorKey) return translated
    return delivery.last_error_code
  }
  return ''
}

function replaceDelivery(updated: NoticeDelivery) {
  messages.value = messages.value.map(item => ({
    ...item,
    deliveries: (Array.isArray(item.deliveries) ? item.deliveries : []).map(delivery => delivery.business_id === updated.business_id ? updated : delivery),
  }))
}

/** 追踪异步测试或人工重试，轮询到终态后停止。 */
async function watchDelivery(deliveryID: string, configID?: string) {
  for (let attempt = 0; attempt < 90 && mounted; attempt++) {
    await new Promise(resolve => window.setTimeout(resolve, 1500))
    if (!mounted) return
    try {
      const latest = await api.noticeDelivery(deliveryID)
      if (configID) testResults.value = { ...testResults.value, [configID]: latest }
      replaceDelivery(latest)
      if (['delivered', 'failed', 'cancelled'].includes(latest.status)) return
    } catch {
      // 临时网络错误不清除当前状态；后续轮询仍会尝试恢复。
    }
  }
}

/** 点击站内消息时先确认已读，再按后端标记跳转到灵感图或计划资源。 */
async function openMessage(item: NoticeMessage) {
  if (!item.read_at) {
    const businessID = auth.user?.business_id ?? ''
    try {
      await api.markNotificationRead(item.business_id)
      const now = new Date().toISOString()
      messages.value = messages.value.map(row => row.business_id === item.business_id ? { ...row, read_at: now } : row)
      if (businessID) {
        appShell.decrementUnreadCount(businessID)
        unreadCount.value = appShell.unreadCount
      } else {
        unreadCount.value = Math.max(0, unreadCount.value - 1)
      }
    } catch {
      ElMessage.error(t('common.errors.requestFailed'))
    }
  }
  if (item.resource_type === 'inspiration' && item.resource_business_id) {
    await router.push(`/inspirations/${encodeURIComponent(item.resource_business_id)}`)
  } else if (item.resource_type === 'plan' && item.resource_business_id) {
    await router.push('/plans')
  }
}

async function markAllRead() {
  const businessID = auth.user?.business_id ?? ''
  try {
    await api.markAllNotificationsRead()
    const now = new Date().toISOString()
    messages.value = messages.value.map(item => item.read_at ? item : { ...item, read_at: now })
    unreadCount.value = 0
    if (businessID) appShell.clearUnreadCount(businessID)
    if (filter.value === 'unread') await loadMessages()
  } catch {
    ElMessage.error(t('common.errors.requestFailed'))
  }
}

function resetForm() {
  form.provider = 'wecom'
  form.name = ''
  form.webhook_url = ''
  form.signing_secret = ''
  form.clear_signing_secret = false
  form.enabled = true
}

function openCreateConfig() {
  editingID.value = ''
  resetForm()
  editorOpen.value = true
}

function openEditConfig(item: NoticeConfig) {
  editingID.value = item.business_id
  form.provider = item.provider
  form.name = item.name
  form.webhook_url = ''
  form.signing_secret = ''
  form.clear_signing_secret = false
  form.enabled = item.enabled
  editorOpen.value = true
}

async function saveConfig() {
  if (!form.name.trim() || Array.from(form.name.trim()).length > 80) {
    ElMessage.warning(t('webhooks.nameRequired'))
    return
  }
  if (!editingID.value && !form.webhook_url.trim()) {
    ElMessage.warning(t('webhooks.saveFailed'))
    return
  }
  editorBusy.value = true
  try {
    if (editingID.value) {
      const updated = await api.updateNoticeConfig(editingID.value, {
        provider: form.provider,
        name: form.name.trim(),
        webhook_url: form.webhook_url.trim(),
        signing_secret: form.signing_secret,
        clear_signing_secret: form.clear_signing_secret,
        enabled: form.enabled,
      })
      configs.value = configs.value.map(item => item.business_id === updated.business_id ? updated : item)
    } else {
      const created = await api.createNoticeConfig({
        provider: form.provider,
        name: form.name.trim(),
        webhook_url: form.webhook_url.trim(),
        signing_secret: form.signing_secret,
      })
      configs.value = [created, ...configs.value]
    }
    editorOpen.value = false
    ElMessage.success(t('webhooks.saved'))
    await loadConfigs()
  } catch {
    ElMessage.error(t('webhooks.saveFailed'))
  } finally {
    editorBusy.value = false
  }
}

async function toggleConfig(item: NoticeConfig, enabled: unknown) {
  try {
    const updated = await api.updateNoticeConfig(item.business_id, { enabled: Boolean(enabled) })
    configs.value = configs.value.map(row => row.business_id === updated.business_id ? updated : row)
  } catch {
    ElMessage.error(t('webhooks.saveFailed'))
  }
}

async function deleteConfig(item: NoticeConfig) {
  try {
    await ElMessageBox.confirm(t('webhooks.deleteConfirm'), t('webhooks.delete'), { type: 'warning', confirmButtonText: t('webhooks.delete'), cancelButtonText: t('common.cancel') })
    await api.deleteNoticeConfig(item.business_id)
    configs.value = configs.value.filter(row => row.business_id !== item.business_id)
    ElMessage.success(t('webhooks.deleted'))
    await loadConfigs()
  } catch (cause) {
    if (cause !== 'cancel' && cause !== 'close') ElMessage.error(t('webhooks.deleteFailed'))
  }
}

async function testConfig(item: NoticeConfig) {
  try {
    const delivery = await api.testNoticeConfig(item.business_id)
    testResults.value = { ...testResults.value, [item.business_id]: delivery }
    ElMessage.success(t('webhooks.testQueued'))
    void watchDelivery(delivery.business_id, item.business_id)
  } catch {
    ElMessage.error(t('webhooks.testFailed'))
  }
}

async function retryDelivery(delivery: NoticeDelivery) {
  try {
    const queued = await api.retryNoticeDelivery(delivery.business_id)
    replaceDelivery(queued)
    ElMessage.success(t('retryQueued'))
    void watchDelivery(queued.business_id)
  } catch {
    ElMessage.error(t('retryFailed'))
  }
}

onMounted(() => {
  mounted = true
  void loadMessages()
  refreshTimer = window.setInterval(() => {
    if (activeTab.value === 'inbox') void loadMessages(true)
    if (activeTab.value === 'channels') void loadConfigs(true)
  }, 15_000)
})

onBeforeUnmount(() => {
  mounted = false
  if (refreshTimer !== undefined) window.clearInterval(refreshTimer)
})
</script>

<template>
  <AppHeader />
  <main class="messages-main">
    <header class="messages-heading">
      <div>
        <p class="eyebrow">PHOTO NINI</p>
        <h1>{{ t('title') }}</h1>
        <p>{{ t('description') }}</p>
      </div>
      <span v-if="unreadCount" class="messages-unread-total">{{ t('unreadLabel') }} · {{ unreadCount }}</span>
    </header>

    <div class="messages-tabs" role="tablist" :aria-label="t('title')">
      <button type="button" role="tab" :aria-selected="activeTab === 'inbox'" :class="{ active: activeTab === 'inbox' }" @click="selectTab('inbox')">
        {{ t('inbox') }}<span v-if="unreadCount" class="tab-count">{{ unreadCount > 99 ? '99+' : unreadCount }}</span>
      </button>
      <button type="button" role="tab" :aria-selected="activeTab === 'channels'" :class="{ active: activeTab === 'channels' }" @click="selectTab('channels')">{{ t('channels') }}</button>
    </div>

    <section v-if="activeTab === 'inbox'" class="messages-panel" role="tabpanel" aria-live="polite">
      <div class="messages-toolbar">
        <div class="messages-filter" role="group" :aria-label="t('inbox')">
          <button type="button" :class="{ active: filter === 'all' }" @click="selectFilter('all')">{{ t('all') }}</button>
          <button type="button" :class="{ active: filter === 'unread' }" @click="selectFilter('unread')">{{ t('unread') }}</button>
        </div>
        <button v-if="unreadCount" class="text-action" type="button" @click="markAllRead">{{ t('markAllRead') }}</button>
      </div>

      <div v-if="loadingMessages" class="messages-empty">{{ t('loading') }}</div>
      <div v-else-if="messageError" class="messages-empty error-panel">
        <p>{{ t('loadFailed') }}</p>
        <button class="outline-action" type="button" @click="loadMessages()">{{ t('reload') }}</button>
      </div>
      <div v-else-if="messages.length === 0" class="messages-empty">{{ t('empty') }}</div>

      <div v-else class="notification-list">
        <article v-for="item in messages" :key="item.business_id" class="notification-card" :class="{ unread: !item.read_at }" role="button" tabindex="0" :aria-label="`${messageTitle(item)}. ${messageBody(item)}`" @click="openMessage(item)" @keydown.enter.self.prevent="openMessage(item)" @keydown.space.self.prevent="openMessage(item)">
          <div class="notification-marker" aria-hidden="true"></div>
          <div class="notification-content">
            <div class="notification-title-row">
              <h2>{{ messageTitle(item) }}</h2>
              <span class="read-state">{{ item.read_at ? t('readLabel') : t('unreadLabel') }}</span>
            </div>
            <p v-if="feedbackReporterText(item)" class="notification-reporter">{{ feedbackReporterText(item) }}</p>
            <p class="notification-body" :class="{ 'feedback-message': item.event_type === 'feedback.submitted' }">{{ messageBody(item) }}</p>
            <div v-if="feedbackImages(item).length" class="feedback-image-list">
              <img v-for="(imageURL, index) in feedbackImages(item)" :key="imageURL" :src="imageURL" :alt="t('feedbackImage', { index: index + 1 })" loading="lazy" referrerpolicy="no-referrer" />
            </div>
            <div class="notification-meta"><span>{{ t('createdAt') }} · {{ formatDate(item.created_at) }}</span></div>

            <div v-if="item.deliveries?.length" class="delivery-list" @click.stop>
              <div v-for="delivery in item.deliveries" :key="delivery.business_id" class="delivery-row">
                <span class="provider-chip"><WebhookProviderLogo :provider="delivery.provider" size="small" /><span>{{ providerName(delivery.provider) }} · {{ delivery.config_name }}</span></span>
                <span v-if="delivery.part_count > 1" class="delivery-part">{{ t('part', { index: delivery.part_index, count: delivery.part_count }) }}</span>
                <span class="delivery-state" :class="`state-${delivery.status}`">{{ deliveryStatus(delivery) }}</span>
                <span v-if="deliveryError(delivery)" class="delivery-error">{{ deliveryError(delivery) }}</span>
                <span v-if="['retry', 'failed'].includes(delivery.status) && delivery.attempt_count > 0" class="delivery-attempts">{{ t('delivery.attemptCount', { count: delivery.attempt_count }) }}</span>
                <button v-if="delivery.can_retry" class="inline-action" type="button" @click="retryDelivery(delivery)">{{ t('retry') }}</button>
              </div>
            </div>
            <p v-else class="no-delivery-status">{{ item.event_type === 'system.error_log.created' ? t('errorLogInternalOnly') : t('noWebhookStatus') }}</p>

            <button v-if="item.resource_type === 'inspiration' && item.resource_business_id" class="open-resource" type="button" @click.stop="openMessage(item)">{{ t('goToInspiration') }} <el-icon aria-hidden="true"><TopRight /></el-icon></button>
            <button v-if="item.resource_type === 'plan' && item.resource_business_id" class="open-resource" type="button" @click.stop="openMessage(item)">{{ t('goToPlan') }} <el-icon aria-hidden="true"><TopRight /></el-icon></button>
          </div>
        </article>
      </div>

      <div v-if="messageTotal > pageSize" class="messages-pagination">
        <button class="outline-action" type="button" :disabled="!hasPrevious" @click="currentPage--; loadMessages()"><el-icon aria-hidden="true"><ArrowLeft /></el-icon></button>
        <span>{{ currentPage }} / {{ Math.ceil(messageTotal / pageSize) }}</span>
        <button class="outline-action" type="button" :disabled="!hasNext" @click="currentPage++; loadMessages()"><el-icon aria-hidden="true"><ArrowRight /></el-icon></button>
      </div>
    </section>

    <section v-else class="messages-panel channels-panel" role="tabpanel">
      <div class="messages-toolbar channels-toolbar">
        <div>
          <h2>{{ t('webhooks.title') }}</h2>
          <p>{{ t('webhooks.description') }}</p>
          <small>{{ configCountText }}</small>
        </div>
        <button class="primary-action" type="button" :disabled="configs.length >= maximumConfigs" @click="openCreateConfig"><el-icon aria-hidden="true"><Plus /></el-icon>{{ t('webhooks.add') }}</button>
      </div>

      <div v-if="loadingConfigs" class="messages-empty">{{ t('common.loading') }}</div>
      <div v-else-if="configError" class="messages-empty error-panel">
        <p>{{ t('webhooks.saveFailed') }}</p>
        <button class="outline-action" type="button" @click="loadConfigs()">{{ t('reload') }}</button>
      </div>
      <div v-else-if="configs.length === 0" class="messages-empty">{{ t('webhooks.empty') }}</div>

      <div v-else class="webhook-list">
        <article v-for="item in configs" :key="item.business_id" class="webhook-card">
          <div class="webhook-card-copy">
            <div class="webhook-card-title"><WebhookProviderLogo :provider="item.provider" /><div><h3>{{ item.name }}</h3><span>{{ providerName(item.provider) }} · {{ item.webhook_masked }}</span></div></div>
            <small>{{ item.secret_configured ? t('webhooks.secretConfigured') : t('webhooks.noSecret') }}</small>
          </div>
          <div class="webhook-card-actions">
            <el-switch :model-value="item.enabled" :aria-label="item.enabled ? t('webhooks.enabled') : t('webhooks.disabled')" @change="toggleConfig(item, $event)" />
            <span v-if="testResults[item.business_id]" class="test-status">{{ deliveryStatus(testResults[item.business_id]) }}</span>
            <button class="outline-action" type="button" :disabled="['pending', 'sending', 'retry'].includes(testResults[item.business_id]?.status ?? '')" @click="testConfig(item)">{{ t('webhooks.test') }}</button>
            <button class="icon-action" type="button" :aria-label="t('webhooks.edit')" @click="openEditConfig(item)"><el-icon aria-hidden="true"><Edit /></el-icon></button>
            <button class="icon-action danger-action" type="button" :aria-label="t('webhooks.delete')" @click="deleteConfig(item)"><el-icon aria-hidden="true"><Delete /></el-icon></button>
          </div>
        </article>
      </div>
    </section>

    <el-dialog v-model="editorOpen" :title="editingID ? t('webhooks.editTitle') : t('webhooks.addTitle')" width="min(560px, calc(100vw - 30px))" align-center>
      <form class="webhook-form" @submit.prevent="saveConfig">
        <label class="form-label">{{ t('webhooks.name') }}<el-input v-model="form.name" maxlength="80" show-word-limit /></label>
        <label class="form-label">{{ t('webhooks.provider') }}<el-select v-model="form.provider" class="provider-select">
          <template #prefix><WebhookProviderLogo :provider="form.provider" size="small" /></template>
          <el-option v-for="provider in providerOptions" :key="provider" :value="provider" :label="providerName(provider)">
            <template #default><div class="provider-select-option"><WebhookProviderLogo :provider="provider" size="small" /><span>{{ providerName(provider) }}</span></div></template>
          </el-option>
        </el-select></label>
        <p class="form-help">{{ t('webhooks.providerHelp') }}</p>
        <label class="form-label">{{ t('webhooks.address') }}<el-input v-model="form.webhook_url" autocomplete="off" placeholder="https://…" /></label>
        <p class="form-help">{{ t('webhooks.addressHint') }}</p>
        <label class="form-label">{{ t('webhooks.secret') }}<el-input v-model="form.signing_secret" type="password" show-password autocomplete="new-password" /></label>
        <p class="form-help">{{ form.provider === 'wecom' ? t('webhooks.signatureHelp') : t('webhooks.secretHint') }}</p>
        <el-checkbox v-if="editingID && configs.find(item => item.business_id === editingID)?.secret_configured" v-model="form.clear_signing_secret">{{ t('webhooks.clearSecret') }}</el-checkbox>
        <el-checkbox v-model="form.enabled">{{ t('webhooks.enabled') }}</el-checkbox>
      </form>
      <template #footer>
        <div class="dialog-actions">
          <el-button @click="editorOpen = false">{{ t('common.cancel') }}</el-button>
          <el-button type="primary" :loading="editorBusy" @click="saveConfig">{{ t('webhooks.save') }}</el-button>
        </div>
      </template>
    </el-dialog>
  </main>
</template>
