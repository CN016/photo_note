<script setup lang="ts">
import ContentZoom from '../components/ContentZoom.vue'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Calendar, Check, Clock, Delete, Edit, Plus, Refresh, UploadFilled } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import AppHeader from '../components/AppHeader.vue'
import PlanCoverImage from '../components/PlanCoverImage.vue'
import { api } from '../api/client'
import type { PlanFields, PlanRecord } from '../api/types'
import { addDateKey, chinaDateKey, chinaDateTimeInput, formatChinaTime, mondayOf, toChinaTimestamp } from '../schedule/scheduleUtils'

type PlanTab = 'today' | 'plans' | 'completed'
type PlanGroup = { key: string; items: PlanRecord[] }

const { t: translate, locale } = useI18n()
function t(key: string, parameters?: Record<string, string | number>): string {
  return translate(key.startsWith('common.') ? key : `plans.${key}`, parameters ?? {})
}

// 页面只持有当前令牌用户返回的计划，状态转换和提醒取消由服务端原子完成。
const loading = ref(true)
const loadFailed = ref(false)
const plans = ref<PlanRecord[]>([])
const activeTab = ref<PlanTab>('today')
const tabKeys: PlanTab[] = ['today', 'plans', 'completed']
const today = ref(chinaDateKey())
const dialogOpen = ref(false)
const saving = ref(false)
const uploadingCover = ref(false)
const editing = ref<PlanRecord | null>(null)
const coverEmojis = ref<string[]>([])
const emojiLoadFailed = ref(false)
const formStart = ref('')
const localCoverURL = ref('')
const fileInput = ref<HTMLInputElement | null>(null)
const busyPlanID = ref('')
const form = reactive<PlanFields>(emptyFields())
onBeforeRouteLeave(to => to.path !== '/spaces' || (!dialogOpen.value && !saving.value && !uploadingCover.value && !busyPlanID.value))
let dateTimer: number | undefined

const activePlans = computed(() => plans.value.filter(item => item.status === 'active'))
const completedPlans = computed(() => plans.value.filter(item => item.status === 'completed'))
const todayItems = computed(() => activePlans.value.filter(item => planDate(item) <= today.value).sort(sortByStart))
const futureItems = computed(() => activePlans.value.filter(item => planDate(item) > today.value).sort(sortByStart))
const currentWeekEnd = computed(() => addDateKey(mondayOf(today.value), 7))
const yearText = computed(() => today.value.slice(0, 4))
const groups = computed<PlanGroup[]>(() => {
  if (activeTab.value === 'today') {
    return [
      { key: 'overdue', items: todayItems.value.filter(item => planDate(item) < today.value) },
      { key: 'today', items: todayItems.value.filter(item => planDate(item) === today.value) },
    ].filter(group => group.items.length > 0)
  }
  if (activeTab.value === 'plans') {
    return [
      { key: 'week', items: futureItems.value.filter(item => planDate(item) < currentWeekEnd.value) },
      { key: 'year', items: futureItems.value.filter(item => planDate(item) >= currentWeekEnd.value && planDate(item).startsWith(yearText.value)) },
      { key: 'other', items: futureItems.value.filter(item => !planDate(item).startsWith(yearText.value)) },
    ].filter(group => group.items.length > 0)
  }
  return completedPlans.value.length ? [{ key: 'completed', items: [...completedPlans.value].sort(sortByStart) }] : []
})
const visibleCount = computed(() => groups.value.reduce((sum, group) => sum + group.items.length, 0))

function emptyFields(): PlanFields {
  return { title: '', description: '', starts_at: '', cover_emoji: '', cover_image_key: '', reminder_offsets: [60] }
}

function sortByStart(left: PlanRecord, right: PlanRecord): number {
  return left.starts_at.localeCompare(right.starts_at)
}

function planDate(item: PlanRecord): string { return chinaDateKey(new Date(item.starts_at)) }

function dateLabel(item: PlanRecord): string {
  const date = new Intl.DateTimeFormat(locale.value, { timeZone: 'Asia/Shanghai', dateStyle: 'medium' }).format(new Date(item.starts_at))
  const time = formatChinaTime(item.starts_at, locale.value)
  if (planDate(item) === today.value) return t('todayAt', { time })
  return t('dateAt', { date, time })
}

function overdueDays(item: PlanRecord): number {
  const start = Date.parse(`${planDate(item)}T00:00:00Z`)
  const current = Date.parse(`${today.value}T00:00:00Z`)
  return Math.max(1, Math.floor((current - start) / 86400000))
}

function currentChinaInput(offsetMinutes = 60): string {
  const value = new Date(Date.now() + offsetMinutes * 60000)
  const parts = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(value)
  const part = (name: string) => parts.find(item => item.type === name)?.value ?? '00'
  return `${part('year')}-${part('month')}-${part('day')}T${part('hour')}:${part('minute')}`
}

/** 读取当前账户所有未删除计划；服务端按访问令牌隔离，并在 SQL 中装载提醒配置。 */
async function load(): Promise<void> {
  loading.value = true
  loadFailed.value = false
  try {
    const result = await api.plans()
    plans.value = Array.isArray(result?.items) ? result.items : []
  } catch {
    loadFailed.value = true
  } finally {
    loading.value = false
  }
}

/** 表单每次打开都绕过 HTTP 缓存读取公开配置，管理员更新后下一次新增即可生效。 */
async function loadCoverEmojis(): Promise<void> {
  emojiLoadFailed.value = false
  coverEmojis.value = []
  try {
    const result = await api.publicConfigs(['plans.cover_emojis'])
    const raw = result.items.find(item => item.key === 'plans.cover_emojis')?.value
    const decoded: unknown = raw ? JSON.parse(raw) : []
    coverEmojis.value = Array.isArray(decoded)
      ? [...new Set(decoded.filter((value): value is string => typeof value === 'string' && value.trim().length > 0))]
      : []
  } catch {
    emojiLoadFailed.value = true
  }
}

/** 打开新建或编辑表单，并在每次打开时刷新系统配置中的 emoji 列表。 */
async function openForm(item?: PlanRecord): Promise<void> {
  if (localCoverURL.value) URL.revokeObjectURL(localCoverURL.value)
  localCoverURL.value = ''
  editing.value = item ?? null
  Object.assign(form, item ? {
    title: item.title,
    description: item.description,
    starts_at: item.starts_at,
    cover_emoji: item.cover_emoji,
    cover_image_key: item.cover_image_key,
    reminder_offsets: [...item.reminder_offsets],
  } : emptyFields())
  formStart.value = item ? chinaDateTimeInput(item.starts_at) : currentChinaInput()
  dialogOpen.value = true
  await loadCoverEmojis()
  if (!item && !form.cover_image_key && coverEmojis.value.length) form.cover_emoji = coverEmojis.value[0]
}

/** 选择 emoji 后释放可能已上传的图片封面引用；底层文件按文件存储约束保留。 */
function chooseEmoji(emoji: string): void {
  form.cover_emoji = emoji
  form.cover_image_key = ''
  if (localCoverURL.value) URL.revokeObjectURL(localCoverURL.value)
  localCoverURL.value = ''
}

/** 上传接口使用 Access Token；服务端保存前还会验证 storekey 文件实际可解码为栅格图片。 */
async function uploadCover(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  const supportedTypes = ['image/jpeg', 'image/png', 'image/gif', 'image/webp']
  if (file.type && !supportedTypes.includes(file.type)) {
    ElMessage.error(t('form.fileType'))
    return
  }
  if (localCoverURL.value) URL.revokeObjectURL(localCoverURL.value)
  localCoverURL.value = URL.createObjectURL(file)
  uploadingCover.value = true
  try {
    const result = await api.upload(file, file.name)
    form.cover_image_key = result.storekey
    form.cover_emoji = ''
  } catch {
    form.cover_image_key = ''
    localCoverURL.value = ''
    ElMessage.error(t('form.coverLoadFailed'))
  } finally {
    uploadingCover.value = false
  }
}

/** 增加一条未重复的常用提前时间，最多三条；用户仍可修改为任意分钟数。 */
function addReminder(): void {
  if (form.reminder_offsets.length >= 3) {
    ElMessage.warning(t('form.maxReminders'))
    return
  }
  const suggestion = [60, 30, 15, 120, 1440, 10, 5].find(value => !form.reminder_offsets.includes(value)) ?? 1
  form.reminder_offsets.push(suggestion)
}

function validateForm(): boolean {
  const offsets = form.reminder_offsets
  if (!form.title.trim() || !formStart.value || (!form.cover_emoji && !form.cover_image_key) || (form.cover_emoji && form.cover_image_key)) return false
  if (offsets.length > 3 || offsets.some(value => !Number.isInteger(value) || value < 1 || value > 525600) || new Set(offsets).size !== offsets.length) return false
  return true
}

/** 保存完整编辑快照；时间显式按 +08:00 提交，提醒空集合交由服务端应用一小时默认值。 */
async function save(): Promise<void> {
  // 回车提交与按钮点击共用同一入口，处理中不重复创建计划或保存尚未完成的封面。
  if (saving.value || uploadingCover.value) return
  if (!validateForm()) {
    ElMessage.warning(t('form.validation'))
    return
  }
  saving.value = true
  try {
    const fields: PlanFields = {
      title: form.title.trim(), description: form.description.trim(),
      starts_at: toChinaTimestamp(formStart.value), cover_emoji: form.cover_emoji,
      cover_image_key: form.cover_image_key, reminder_offsets: [...form.reminder_offsets],
    }
    if (editing.value) await api.updatePlan(editing.value.business_id, fields)
    else await api.createPlan(fields)
    dialogOpen.value = false
    ElMessage.success(t('form.saved'))
    await load()
  } catch {
    ElMessage.error(t('common.errors.requestFailed'))
  } finally {
    saving.value = false
  }
}

/** 只有确认后才完成计划；服务端在事务中取消它的所有未发送提醒。 */
async function complete(item: PlanRecord): Promise<void> {
  try {
    await ElMessageBox.confirm(t('form.completeConfirm', { title: item.title }), t('actions.complete'), {
      confirmButtonText: t('actions.complete'), cancelButtonText: t('actions.cancel'), type: 'success',
    })
    busyPlanID.value = item.business_id
    await api.completePlan(item.business_id)
    ElMessage.success(t('form.completed'))
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(t('common.errors.requestFailed'))
  } finally {
    busyPlanID.value = ''
  }
}

/** 删除采用服务端软删除，通知历史保留，未发送提醒在同一事务内取消。 */
async function remove(item: PlanRecord): Promise<void> {
  try {
    await ElMessageBox.confirm(t('form.deleteConfirm', { title: item.title }), t('actions.delete'), {
      confirmButtonText: t('actions.delete'), cancelButtonText: t('actions.cancel'), type: 'warning',
    })
    busyPlanID.value = item.business_id
    await api.deletePlan(item.business_id)
    ElMessage.success(t('form.deleted'))
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(t('common.errors.requestFailed'))
  } finally {
    busyPlanID.value = ''
  }
}

function closeDialog(): void {
  dialogOpen.value = false
  if (localCoverURL.value) URL.revokeObjectURL(localCoverURL.value)
  localCoverURL.value = ''
}

onMounted(() => {
  void load()
  dateTimer = window.setInterval(() => { today.value = chinaDateKey() }, 60000)
})

onBeforeUnmount(() => {
  if (dateTimer !== undefined) window.clearInterval(dateTimer)
  if (localCoverURL.value) URL.revokeObjectURL(localCoverURL.value)
})
</script>

<template>
  <AppHeader />
  <ContentZoom><main class="plans-main">
    <header class="plans-heading">
      <div>
        <p class="plans-eyebrow">{{ t('title') }}</p>
        <h1>{{ t('title') }}</h1>
        <p class="plans-subtitle">{{ t('subtitle') }}</p>
      </div>
      <el-button type="primary" class="add-plan-button" @click="openForm()">
        <el-icon aria-hidden="true"><Plus /></el-icon>{{ t('actions.add') }}
      </el-button>
    </header>

    <nav class="plan-tabs" :aria-label="t('title')" role="tablist">
      <button v-for="tab in tabKeys" :key="tab" type="button" role="tab" :aria-selected="activeTab === tab" :class="{ active: activeTab === tab }" @click="activeTab = tab">
        {{ t(`tabs.${tab}`) }}
        <span>{{ tab === 'today' ? todayItems.length : tab === 'plans' ? futureItems.length : completedPlans.length }}</span>
      </button>
    </nav>

    <div v-if="loading" class="plans-state" role="status"><el-icon class="is-loading" aria-hidden="true"><Refresh /></el-icon>{{ t('loading') }}</div>
    <div v-else-if="loadFailed" class="plans-state is-error" role="alert">
      <p>{{ t('loadError') }}</p><el-button @click="load">{{ t('retry') }}</el-button>
    </div>
    <template v-else>
      <section v-if="visibleCount" class="plan-groups" :aria-label="t(`tabs.${activeTab}`)">
        <section v-for="group in groups" :key="group.key" class="plan-group">
          <h2><span class="group-marker"></span>{{ group.key === 'year' ? `${t(`groups.${group.key}`)} · ${yearText}` : t(`groups.${group.key}`) }}<small>{{ group.items.length }}</small></h2>
          <article v-for="item in group.items" :key="item.business_id" class="plan-card" :class="{ 'is-completed': item.status === 'completed', 'is-overdue': planDate(item) < today && item.status === 'active' }">
            <div class="plan-cover" aria-hidden="true">
              <PlanCoverImage v-if="item.cover_image_key" :storekey="item.cover_image_key" :label="t('cover.imageAlt')" :loading-label="t('cover.loading')" :unavailable-label="t('cover.unavailable')" />
              <span v-else class="plan-emoji">{{ item.cover_emoji }}</span>
            </div>
            <div class="plan-copy">
              <div class="plan-title-row">
                <h3>{{ item.title }}</h3>
                <el-tag v-if="item.status === 'completed'" type="success" effect="light" round>{{ t('tabs.completed') }}</el-tag>
                <el-tag v-else-if="planDate(item) < today" type="danger" effect="light" round>{{ t('overdueBy', { days: overdueDays(item) }) }}</el-tag>
              </div>
              <p v-if="item.description" class="plan-description">{{ item.description }}</p>
              <div class="plan-meta"><span><el-icon aria-hidden="true"><Calendar /></el-icon>{{ dateLabel(item) }}</span><span v-if="item.reminder_offsets.length"><el-icon aria-hidden="true"><Clock /></el-icon>{{ item.reminder_offsets.map(value => t('reminderLabel', { minutes: value })).join(' · ') }}</span></div>
            </div>
            <div class="plan-actions">
              <el-button v-if="item.status === 'active'" circle type="success" plain :loading="busyPlanID === item.business_id" :title="t('actions.complete')" :aria-label="t('actions.complete')" @click="complete(item)"><el-icon aria-hidden="true"><Check /></el-icon></el-button>
              <el-button v-if="item.status === 'active'" circle plain :disabled="busyPlanID === item.business_id" :title="t('actions.edit')" :aria-label="t('actions.edit')" @click="openForm(item)"><el-icon aria-hidden="true"><Edit /></el-icon></el-button>
              <el-button circle type="danger" plain :loading="busyPlanID === item.business_id" :title="t('actions.delete')" :aria-label="t('actions.delete')" @click="remove(item)"><el-icon aria-hidden="true"><Delete /></el-icon></el-button>
            </div>
          </article>
        </section>
      </section>
      <section v-else class="plans-empty">
        <div class="empty-icon"><el-icon aria-hidden="true"><Calendar /></el-icon></div>
        <h2>{{ t(`empty.${activeTab}`) }}</h2>
        <p>{{ t('empty.hint') }}</p>
        <el-button v-if="activeTab !== 'completed'" type="primary" plain @click="openForm()"><el-icon aria-hidden="true"><Plus /></el-icon>{{ t('actions.add') }}</el-button>
      </section>
    </template>
  </main></ContentZoom>

  <el-dialog v-model="dialogOpen" class="plans-dialog" :title="editing ? t('form.editTitle') : t('form.newTitle')" width="min(620px, calc(100vw - 28px))" :close-on-click-modal="!saving && !uploadingCover" :close-on-press-escape="!saving && !uploadingCover" :show-close="!saving && !uploadingCover" destroy-on-close @closed="closeDialog">
    <el-form label-position="top" :disabled="saving || uploadingCover" @submit.prevent="save">
      <el-form-item :label="t('form.title')" required>
        <el-input v-model="form.title" maxlength="120" show-word-limit :placeholder="t('form.titlePlaceholder')" />
      </el-form-item>
      <el-form-item :label="t('form.description')">
        <el-input v-model="form.description" type="textarea" maxlength="4000" show-word-limit :rows="3" :placeholder="t('form.descriptionPlaceholder')" />
      </el-form-item>
      <el-form-item :label="t('form.startsAt')" required>
        <el-input v-model="formStart" type="datetime-local" />
        <small class="plan-form-help">{{ t('form.timezone') }}</small>
      </el-form-item>
      <el-form-item :label="t('form.cover')" required>
        <div class="cover-editor">
          <div class="cover-preview">
            <img v-if="localCoverURL" :src="localCoverURL" alt="" />
            <PlanCoverImage v-else-if="form.cover_image_key" :storekey="form.cover_image_key" :label="t('cover.imageAlt')" :loading-label="t('cover.loading')" :unavailable-label="t('cover.unavailable')" />
            <span v-else-if="form.cover_emoji" class="plan-emoji">{{ form.cover_emoji }}</span>
            <el-icon v-else class="cover-add-placeholder" aria-hidden="true"><Plus /></el-icon>
          </div>
          <div class="cover-options">
            <div class="cover-options-heading"><strong>{{ t('form.emoji') }}</strong><el-button text :disabled="emojiLoadFailed" @click="loadCoverEmojis">{{ t('actions.chooseEmoji') }}</el-button></div>
            <div v-if="coverEmojis.length" class="emoji-grid" :aria-label="t('actions.chooseEmoji')">
              <button v-for="emoji in coverEmojis" :key="emoji" type="button" :disabled="saving || uploadingCover" :aria-label="emoji" :aria-pressed="form.cover_emoji === emoji" :class="{ selected: form.cover_emoji === emoji }" @click="chooseEmoji(emoji)">{{ emoji }}</button>
            </div>
            <p v-if="emojiLoadFailed" class="plan-form-error" role="alert">{{ t('form.emojiLoadFailed') }}</p>
            <div class="cover-upload-row">
              <input ref="fileInput" class="visually-hidden" type="file" accept="image/jpeg,image/png,image/gif,image/webp" @change="uploadCover" />
              <el-button :loading="uploadingCover" @click="fileInput?.click()"><el-icon aria-hidden="true"><UploadFilled /></el-icon>{{ form.cover_image_key ? t('actions.replaceCover') : t('actions.uploadCover') }}</el-button>
              <small v-if="form.cover_image_key">{{ t('form.imageSelected') }}</small>
            </div>
          </div>
        </div>
      </el-form-item>
      <el-form-item :label="t('form.reminders')">
        <div class="reminder-editor">
          <div v-for="(offset, index) in form.reminder_offsets" :key="index" class="reminder-row">
            <el-input-number v-model="form.reminder_offsets[index]" :min="1" :max="525600" :step="5" controls-position="right" :aria-label="t('form.minutesBefore')" />
            <span>{{ t('form.minutesBefore') }}</span>
            <el-button circle text type="danger" :aria-label="t('actions.removeReminder')" @click="form.reminder_offsets.splice(index, 1)"><el-icon aria-hidden="true"><Delete /></el-icon></el-button>
          </div>
          <el-button v-if="form.reminder_offsets.length < 3" text type="primary" @click="addReminder"><el-icon aria-hidden="true"><Plus /></el-icon>{{ t('actions.addReminder') }}</el-button>
          <small class="plan-form-help">{{ form.reminder_offsets.length ? t('form.reminderHelp') : t('form.defaultReminder') }}</small>
        </div>
      </el-form-item>
    </el-form>
    <template #footer>
      <div class="plan-dialog-actions"><el-button :disabled="saving || uploadingCover" @click="closeDialog">{{ t('actions.cancel') }}</el-button><el-button type="primary" :loading="saving || uploadingCover" :disabled="uploadingCover" @click="save">{{ saving ? t('actions.saving') : t('actions.save') }}</el-button></div>
    </template>
  </el-dialog>
</template>

<style scoped>
/* 界面表面与控件使用全局主题令牌；图片、文档和用户自选封面颜色保持原数据。 */
.plans-main { width: min(960px, calc(100vw - 310px)); min-height: 100svh; margin: 0 42px 0 calc(226px + max(0px, (100vw - 1708px) / 2)); padding: 46px 0 88px; color: var(--ink); }
.plans-heading { display: flex; align-items: center; justify-content: space-between; gap: 18px; }
.plans-eyebrow { margin: 0 0 4px; color: var(--accent); font-size: 11px; font-weight: 750; letter-spacing: .12em; text-transform: uppercase; }
.plans-heading h1 { margin: 0; font-size: clamp(27px, 3vw, 35px); letter-spacing: -.035em; }
.plans-subtitle { margin: 8px 0 0; color: var(--muted); font-size: 13px; line-height: 1.55; }
.add-plan-button { min-height: 44px; border-radius: var(--radius-control); font-weight: 700; }
.plan-tabs { display: flex; gap: 7px; margin: 30px 0 22px; padding: 5px; border: 1px solid var(--line); border-radius: var(--radius-control); background: var(--surface-soft); }
.plan-tabs button { display: flex; min-width: 0; min-height: 42px; flex: 1; align-items: center; justify-content: center; gap: 8px; border: 0; border-radius: var(--radius-control); background: transparent; color: var(--muted); cursor: pointer; font: inherit; font-size: 12px; font-weight: 650; }
.plan-tabs button span { display: grid; min-width: 20px; height: 20px; place-items: center; border-radius: 10px; background: var(--surface); color: var(--muted); font-size: 10px; }
.plan-tabs button.active { background: var(--accent-soft); color: var(--accent-hover); box-shadow: 0 1px 4px rgb(20 30 40 / 7%); }
.plan-tabs button.active span { background: var(--accent-soft); color: var(--accent-hover); }
.plans-state { display: flex; min-height: 230px; align-items: center; justify-content: center; gap: 10px; flex-direction: column; color: var(--muted); font-size: 13px; text-align: center; }
.plans-state.is-error { color: var(--danger); }
.plans-state p { margin: 0; }
.plan-groups { display: grid; gap: 24px; }
.plan-group h2 { display: flex; align-items: center; gap: 9px; margin: 0 0 10px; color: var(--ink); font-size: 14px; font-weight: 750; }
.group-marker { width: 3px; height: 16px; border-radius: var(--radius-control); background: var(--accent); }
.plan-group h2 small { display: grid; min-width: 20px; height: 20px; place-items: center; border-radius: 10px; background: var(--surface-soft); color: var(--muted); font-size: 10px; }
.plan-card { display: flex; min-width: 0; align-items: center; gap: 15px; margin-bottom: 8px; padding: 14px; border: 1px solid var(--line); border-radius: var(--radius-card); background: var(--surface); box-shadow: var(--soft-shadow); transition: border-color .18s ease, box-shadow .18s ease; }
.plan-card:hover { border-color: color-mix(in srgb, var(--accent) 30%, var(--line)); box-shadow: var(--soft-shadow); }
.plan-card.is-completed { opacity: .72; }
.plan-card.is-overdue { border-left: 3px solid var(--danger); }
.plan-cover { display: grid; width: 68px; height: 68px; flex: 0 0 68px; place-items: center; overflow: hidden; border-radius: var(--radius-card); background: var(--accent-soft); }
.plan-emoji { font-size: 30px; line-height: 1; }
.plan-copy { min-width: 0; flex: 1; }
.plan-title-row { display: flex; min-width: 0; align-items: center; flex-wrap: wrap; gap: 8px; }
.plan-title-row h3 { min-width: 0; margin: 0; color: var(--ink); font-size: 14px; line-height: 1.4; overflow-wrap: anywhere; }
.plan-description { margin: 5px 0 0; color: var(--ink-soft); font-size: 12px; line-height: 1.5; white-space: pre-line; overflow-wrap: anywhere; }
.plan-meta { display: flex; flex-wrap: wrap; gap: 8px 16px; margin-top: 8px; color: var(--muted); font-size: 10px; }
.plan-meta span { display: inline-flex; min-width: 0; align-items: center; gap: 5px; overflow-wrap: anywhere; }
.plan-meta :deep(.el-icon) { color: var(--accent); font-size: 13px; }
.plan-actions { display: flex; flex: 0 0 auto; align-items: center; gap: 5px; }
.plan-actions :deep(.el-button) { width: 36px; height: 36px; margin: 0; }
.plans-empty { display: grid; min-height: 310px; justify-items: center; align-content: center; padding: 30px 16px; border: 1px dashed var(--line-strong); border-radius: var(--radius-card); background: color-mix(in srgb, var(--surface) 75%, var(--surface-soft)); text-align: center; }
.empty-icon { display: grid; width: 54px; height: 54px; place-items: center; border-radius: 50%; background: var(--accent-soft); color: var(--accent); font-size: 24px; }
.plans-empty h2 { margin: 15px 0 0; color: var(--ink); font-size: 15px; }
.plans-empty p { margin: 7px 0 17px; color: var(--muted); font-size: 12px; }
.cover-editor { display: flex; width: 100%; align-items: flex-start; gap: 14px; }
.cover-preview { display: grid; width: 80px; height: 80px; flex: 0 0 80px; place-items: center; overflow: hidden; border-radius: var(--radius-card); background: var(--accent-soft); }
.cover-add-placeholder { color: var(--accent); font-size: 24px; }
.cover-preview img { width: 100%; height: 100%; object-fit: cover; }
.cover-preview :deep(.plan-cover-image) { width: 80px; height: 80px; flex-basis: 80px; }
.cover-options { min-width: 0; flex: 1; }
.cover-options-heading { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 7px; color: var(--ink-soft); font-size: 11px; }
.cover-options-heading :deep(.el-button) { min-height: 30px; padding: 0 4px; font-size: 10px; }
.emoji-grid { display: flex; flex-wrap: wrap; gap: 5px; }
.emoji-grid button { display: grid; width: 40px; height: 40px; place-items: center; border: 1px solid var(--line); border-radius: var(--radius-control); background: var(--surface); cursor: pointer; font-size: 20px; }
.emoji-grid button.selected { border-color: var(--accent); background: var(--accent-soft); box-shadow: 0 0 0 1px var(--accent); }
.cover-upload-row { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 9px; }
.cover-upload-row :deep(.el-button) { min-height: 36px; }
.cover-upload-row small, .plan-form-help { color: var(--muted); font-size: 10px; line-height: 1.45; }
.plan-form-help { display: block; margin-top: 5px; }
.plan-form-error { margin: 7px 0; color: var(--danger); font-size: 11px; line-height: 1.45; }
.reminder-editor { display: grid; width: 100%; gap: 7px; }
.reminder-row { display: flex; align-items: center; gap: 9px; }
.reminder-row :deep(.el-input-number) { width: 160px; }
.reminder-row > span { color: var(--muted); font-size: 11px; }
.plan-dialog-actions { display: flex; justify-content: flex-end; gap: 8px; }
.visually-hidden { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; clip-path: inset(50%); }
@media (max-width: 768px) { .plans-main { width: calc(100% - 34px); margin: 0 auto; padding: 30px 0 calc(105px + env(safe-area-inset-bottom)); } }
@media (max-width: 768px) {
  .plan-actions :deep(.el-button), .emoji-grid button { width: 44px; height: 44px; min-height: 44px; }
  .plan-tabs button, .cover-options-heading :deep(.el-button), .cover-upload-row :deep(.el-button) { min-height: 44px; }
}
@media (max-width: 560px) {
  .plans-main { width: calc(100% - 24px); padding-top: 23px; }
  .plans-heading { align-items: flex-start; }
  .plans-heading h1 { font-size: 27px; }
  .plans-subtitle { max-width: 250px; font-size: 11px; }
  .add-plan-button { min-height: 44px; padding: 0 10px; font-size: 11px; }
  .plan-tabs { margin: 22px 0 17px; gap: 3px; padding: 4px; }
  .plan-tabs button { min-height: 44px; gap: 5px; font-size: 11px; }
  .plan-card { display: grid; grid-template-columns: 50px minmax(0, 1fr); align-items: start; gap: 10px; padding: 12px; }
  .plan-cover { width: 50px; height: 50px; flex-basis: 50px; }
  .plan-cover :deep(.plan-cover-image) { width: 50px; height: 50px; flex-basis: 50px; }
  .plan-emoji { font-size: 24px; }
  .plan-title-row h3 { font-size: 12px; }
  .plan-title-row :deep(.el-tag) { height: 20px; padding: 0 6px; font-size: 9px; }
  .plan-description { font-size: 11px; }
  .plan-meta { display: grid; gap: 5px; font-size: 9px; }
  /* 操作移到正文下方，44px 按钮不会压缩标题与描述。 */
  .plan-actions { grid-column: 2; justify-self: end; gap: 4px; margin-left: 0; }
  .plan-actions :deep(.el-button) { width: 44px; height: 44px; min-height: 44px; }
  .plan-actions :deep(.el-button + .el-button) { margin-left: 0; }
  .cover-editor { gap: 10px; }
  .cover-preview { width: 62px; height: 62px; flex-basis: 62px; }
  .cover-preview :deep(.plan-cover-image) { width: 62px; height: 62px; flex-basis: 62px; }
  .emoji-grid { gap: 4px; }
  .emoji-grid button { width: 44px; height: 44px; font-size: 20px; }
  .reminder-row :deep(.el-input-number) { width: 138px; }
  .reminder-row > span { font-size: 11px; }
  .cover-options-heading :deep(.el-button), .cover-upload-row :deep(.el-button) { min-height: 44px; }
}
</style>

<style>
@media (max-width: 560px) {
  .el-dialog.plans-dialog { display: flex; max-height: calc(100dvh - 20px); flex-direction: column; margin: 10px auto !important; }
  .el-dialog.plans-dialog .el-dialog__body { min-height: 0; flex: 1 1 auto; overflow-y: auto; }
}
</style>
