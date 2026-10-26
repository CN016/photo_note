<script setup lang="ts">
import { onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import { api } from '../api/client'
import { useAuthStore } from '../stores/auth'
const auth = useAuthStore()
const { t, locale } = useI18n()
const router = useRouter()
let generation = 0
let busy = false
let ownsModal = false
let timer: number | undefined
const identity = () => `${auth.user?.business_id || ''}:${auth.orgid}:${auth.sessionVersion}`
/** 从服务端原子领取当天提醒；页面重建和多设备不会重复确认，空间切换后丢弃旧响应。 */
async function check() {
  if (!auth.user || busy || document.visibilityState !== 'visible') return
  busy = true
  const version = generation
  const scope = identity()
  try {
    const result = await api.inspirationGoal()
    if (version !== generation || scope !== identity() || !result.reminder_pending) return
    const claimed = await api.acknowledgeInspirationReminder(result.today.day)
    if (!claimed.acknowledged || version !== generation || scope !== identity()) return
    const formatter = new Intl.NumberFormat(locale.value)
    try {
      ownsModal = true
      await ElMessageBox.confirm(t('common.inspirationGoal.body', { count: formatter.format(result.today.saved_count), target: formatter.format(result.goal.target) }), t('common.inspirationGoal.title'), { confirmButtonText: t('common.inspirationGoal.go'), cancelButtonText: t('common.inspirationGoal.later'), closeOnClickModal: false, type: 'info' })
      if (scope === identity()) await router.push('/inspirations/new')
    } catch { /* 稍后、关闭、退出页面均已完成一次提醒，不影响其他工作。 */ } finally { ownsModal = false }
  } catch { /* 全局提醒网络失败保持服务端未确认，下次轮询重试，不打断当前页面。 */ }
  finally { busy = false }
}
watch(() => [auth.user?.business_id, auth.orgid, auth.sessionVersion], () => { generation += 1; if (ownsModal) ElMessageBox.close(); void check() }, { immediate: true })
timer = window.setInterval(() => { void check() }, 30_000)
onBeforeUnmount(() => { generation += 1; if (ownsModal) ElMessageBox.close(); window.clearInterval(timer) })
</script>
<template><span class="inspiration-reminder-anchor" aria-hidden="true" /></template>
<style scoped>.inspiration-reminder-anchor{display:none}</style>
