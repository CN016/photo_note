<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import LanguageSwitcher from '../components/LanguageSwitcher.vue'
import { api } from '../api/client'
import type { ScheduleBookingReceipt, SchedulePublicSlot } from '../api/types'
import { addDateKey, chinaDateKey, formatChinaDate, formatChinaTime } from '../schedule/scheduleUtils'

const route = useRoute()
const { t: translate, locale } = useI18n()
function t(key: string, parameters?: Record<string, string | number>): string {
  return translate(key.startsWith('common.') ? key : `schedule.${key}`, parameters ?? {})
}
// 分享令牌只从路由读取；名额、链接状态和客户资料校验都由服务端完成。
const loading = ref(true)
const unavailable = ref(false)
const slots = ref<SchedulePublicSlot[]>([])
const selected = ref<SchedulePublicSlot | null>(null)
const submitting = ref(false)
const receipt = ref<ScheduleBookingReceipt | null>(null)
const fields = reactive({ name: '', phone: '', note: '' })
const token = computed(() => String(route.params.shareToken ?? ''))
const requestDialogOpen = computed({ get: () => selected.value !== null, set: (open: boolean) => { if (!open) selected.value = null } })
const receiptDialogOpen = computed({ get: () => receipt.value !== null, set: (open: boolean) => { if (!open) receipt.value = null } })
const dateGroups = computed(() => {
  const groups = new Map<string, SchedulePublicSlot[]>()
  for (const slot of slots.value) {
    const actualDate = chinaDateKey(new Date(slot.start_at))
    groups.set(actualDate, [...(groups.get(actualDate) ?? []), slot])
  }
  return [...groups.entries()].sort(([a], [b]) => a.localeCompare(b))
})

function rangeLabel(date: string): string {
  return formatChinaDate(date, locale.value, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' })
}

/** 公开页面最多查询未来 90 天，只接收剩余名额大于零的开放时段。 */
async function load(): Promise<void> {
  loading.value = true
  unavailable.value = false
  const from = chinaDateKey()
  try {
    const result = await api.publicScheduleSlots(token.value, from, addDateKey(from, 89))
    slots.value = result.items ?? []
  } catch {
    unavailable.value = true
  } finally {
    loading.value = false
  }
}

/** 选中时段时清空前次输入，避免把旧联系方式提交到另一份申请。 */
function openRequest(slot: SchedulePublicSlot): void {
  selected.value = slot
  receipt.value = null
  fields.name = ''
  fields.phone = ''
  fields.note = ''
}

/** 提交后只显示申请编号；访客的联系信息仅返回给档期拥有者。 */
async function submit(): Promise<void> {
  if (!selected.value || !fields.name.trim() || !fields.phone.trim()) {
    ElMessage.warning(t('public.required'))
    return
  }
  submitting.value = true
  try {
    receipt.value = await api.requestScheduleBooking(token.value, {
      schedule_id: selected.value.business_id,
      start_at: selected.value.start_at,
      name: fields.name,
      phone: fields.phone,
      note: fields.note,
    })
    selected.value = null
    await load()
  } catch {
    ElMessage.error(t('public.submitFailed'))
    await load()
  } finally {
    submitting.value = false
  }
}

onMounted(() => { void load() })
</script>

<template>
  <main class="public-booking-page">
    <section class="public-booking-shell">
      <header class="public-booking-header">
        <RouterLink class="public-brand" to="/inspirations"><span>n.</span><strong>{{ t('common.brand') }}</strong></RouterLink>
        <LanguageSwitcher />
      </header>
      <section class="public-booking-intro"><p>{{ t('common.schedule') }}</p><h1>{{ t('public.title') }}</h1><span>{{ t('public.subtitle') }}</span></section>

      <div v-if="loading" class="public-booking-state" role="status">{{ t('public.loading') }}</div>
      <section v-else-if="unavailable" class="public-booking-state is-error"><span aria-hidden="true">⌁</span><p>{{ t('public.unavailable') }}</p></section>
      <section v-else-if="!slots.length" class="public-booking-state"><span aria-hidden="true">◷</span><p>{{ t('public.empty') }}</p></section>
      <section v-else class="public-slot-list">
        <div v-for="[date, dailySlots] in dateGroups" :key="date" class="public-slot-day">
          <h2>{{ rangeLabel(date) }}</h2>
          <article v-for="slot in dailySlots" :key="`${slot.business_id}-${slot.start_at}`" class="public-slot-card">
            <div class="public-slot-time"><strong>{{ formatChinaTime(slot.start_at, locale) }}–{{ formatChinaTime(slot.end_at, locale) }}</strong><span>{{ t('public.remaining', { count: slot.remaining }) }}</span></div>
            <h3>{{ slot.title }}</h3>
            <p v-if="slot.content">{{ slot.content }}</p>
            <el-button type="primary" @click="openRequest(slot)">{{ t('actions.book') }}</el-button>
          </article>
        </div>
      </section>
      <footer class="public-booking-footer"><RouterLink to="/login">{{ t('common.login') }}</RouterLink><span>·</span><span>{{ t('common.product') }}</span></footer>
    </section>

    <el-dialog v-model="requestDialogOpen" :title="t('actions.book')" width="min(490px, calc(100vw - 28px))" destroy-on-close>
      <div v-if="selected" class="selected-slot-summary"><small>{{ rangeLabel(selected.series_date) }}</small><strong>{{ formatChinaTime(selected.start_at, locale) }}–{{ formatChinaTime(selected.end_at, locale) }}</strong><b>{{ selected.title }}</b><p v-if="selected.content">{{ selected.content }}</p></div>
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item :label="t('public.name')" required><el-input v-model="fields.name" maxlength="80" autocomplete="name" /></el-form-item>
        <el-form-item :label="t('public.phone')" required><el-input v-model="fields.phone" maxlength="32" type="tel" autocomplete="tel" /></el-form-item>
        <el-form-item :label="t('public.note')"><el-input v-model="fields.note" type="textarea" maxlength="1000" :rows="3" :placeholder="t('public.notePlaceholder')" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="selected = null">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="submitting" @click="submit">{{ t('public.submit') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="receiptDialogOpen" :title="t('public.success')" width="min(440px, calc(100vw - 28px))" :show-close="false">
      <div v-if="receipt" class="booking-receipt"><span aria-hidden="true">✓</span><p>{{ t('public.receipt', { code: receipt.request_code }) }}</p><small>{{ t('public.pendingHelp') }}</small><strong>{{ receipt.request_code }}</strong></div>
      <template #footer><el-button type="primary" @click="receipt = null">{{ t('actions.close') }}</el-button></template>
    </el-dialog>
  </main>
</template>

<style scoped>
.public-booking-page{min-height:100svh;padding:30px 18px 70px;background:var(--page);color:var(--ink)}.public-booking-shell{width:min(820px,100%);margin:0 auto}.public-booking-header{display:flex;align-items:center;justify-content:space-between;padding:0 0 18px;border-bottom:1px solid var(--line)}.public-brand{display:flex;align-items:center;gap:9px;color:var(--ink);text-decoration:none}.public-brand>span{display:grid;width:34px;height:34px;place-items:center;border-radius:4px;background:var(--accent);color:white;font-weight:800}.public-brand strong{font-size:13px}.public-booking-intro{padding:38px 0 21px}.public-booking-intro>p{margin:0;color:var(--accent);font-size:11px;font-weight:750}.public-booking-intro h1{margin:8px 0;font-size:clamp(27px,6vw,37px);letter-spacing:-.04em}.public-booking-intro>span{color:var(--muted);font-size:13px;line-height:1.6}.public-booking-state{display:grid;min-height:260px;align-content:center;justify-items:center;gap:12px;border:1px solid var(--line);border-radius:8px;background:var(--surface);color:var(--muted);text-align:center}.public-booking-state>span{color:var(--accent);font-size:30px}.public-booking-state p{margin:0;font-size:13px}.public-booking-state.is-error{color:var(--danger)}.public-slot-list{display:grid;gap:22px}.public-slot-day h2{margin:0 0 10px;font-size:15px}.public-slot-card{display:grid;gap:9px;margin-bottom:10px;padding:17px;border:1px solid var(--line);border-radius:6px;background:var(--surface);box-shadow:var(--soft-shadow)}.public-slot-time{display:flex;align-items:center;justify-content:space-between;gap:10px}.public-slot-time strong{font-size:14px}.public-slot-time span{color:var(--accent);font-size:11px;font-weight:700}.public-slot-card h3{margin:0;font-size:14px}.public-slot-card p{margin:0;color:var(--ink-soft);font-size:12px;line-height:1.6;white-space:pre-wrap}.public-slot-card :deep(.el-button){justify-self:end;min-height:44px}.public-booking-footer{display:flex;justify-content:center;gap:9px;margin-top:28px;color:var(--muted);font-size:10px}.public-booking-footer a{color:var(--muted)}.selected-slot-summary{display:grid;gap:5px;padding:12px;margin-bottom:13px;border-radius:4px;background:var(--surface-soft)}.selected-slot-summary small{color:var(--muted);font-size:10px}.selected-slot-summary strong{font-size:14px}.selected-slot-summary b{font-size:12px}.selected-slot-summary p{margin:0;color:var(--ink-soft);font-size:11px;white-space:pre-wrap}.booking-receipt{display:grid;justify-items:center;gap:9px;text-align:center}.booking-receipt>span{display:grid;width:43px;height:43px;place-items:center;border-radius:50%;background:var(--accent-soft);color:var(--accent);font-size:24px}.booking-receipt p{margin:0;font-size:13px}.booking-receipt small{color:var(--muted);font-size:11px;line-height:1.5}.booking-receipt strong{padding:10px 18px;border:1px dashed var(--line-strong);border-radius:4px;font-size:20px;letter-spacing:.12em}
@media(max-width:560px){.public-booking-page{padding:17px 13px calc(80px + env(safe-area-inset-bottom))}.public-booking-intro{padding-top:30px}.public-slot-card{padding:14px}.public-slot-card :deep(.el-button){width:100%}}
</style>
