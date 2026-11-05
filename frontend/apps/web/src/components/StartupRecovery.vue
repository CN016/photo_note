<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Loading } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'

/** 首个导航仍在等健康检查或登录恢复时提供真实状态；不挂载受保护页面，不触碰登录凭据。 */
const route = useRoute()
const auth = useAuthStore()
const { t } = useI18n()
const slow = ref(false)
const starting = computed(() => route.matched.length === 0)
let timer: ReturnType<typeof setTimeout> | undefined
onMounted(() => { timer = setTimeout(() => { slow.value = true }, 12_000) })
onBeforeUnmount(() => clearTimeout(timer))
/** 首屏没有已打开的编辑文档；用户可重新加载恢复，原生刷新成功的迟到结果仍由保管库保存。 */
function retry() { window.location.reload() }
</script>

<template>
  <main v-if="starting" class="startup-recovery" aria-live="polite" :aria-busy="true">
    <el-icon class="is-loading" aria-hidden="true"><Loading /></el-icon>
    <p role="status">{{ t(auth.busy ? 'startup.restoring' : 'startup.opening') }}</p>
    <el-button v-if="slow" @click="retry">{{ t('startup.retry') }}</el-button>
  </main>
</template>

<style scoped>
.startup-recovery { min-height: 100dvh; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 16px; padding: 24px; color: var(--el-text-color-regular); }
.startup-recovery p { margin: 0; }
.startup-recovery .el-icon { font-size: 24px; color: var(--el-color-primary); }
@media (prefers-reduced-motion: reduce) { .is-loading { animation: none; } }
</style>
