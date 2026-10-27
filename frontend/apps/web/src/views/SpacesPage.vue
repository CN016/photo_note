<script setup lang="ts">
import ContentZoom from '../components/ContentZoom.vue'
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { OfficeBuilding, User } from '@element-plus/icons-vue'
import AppHeader from '../components/AppHeader.vue'
import { api } from '../api/client'
import type { Environment } from '../api/types'
import { useAuthStore } from '../stores/auth'
import { supportsSynchronizedSpaces, switchSpace } from '../spaces/coordination'

const { t } = useI18n()
const auth = useAuthStore()
const items = ref<Environment[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const supported = supportsSynchronizedSpaces()
async function load() {
  loading.value = true
  try { items.value = (await api.environments()).items } catch { error.value = t('spaces.loadError') }
  finally { loading.value = false }
}
async function select(orgid: number) {
  busy.value = true
  error.value = ''
  try { await switchSpace(orgid) } catch { error.value = t('spaces.switchError') }
  finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <div class="workspace-page"><AppHeader /><ContentZoom><main class="space-main" data-space-ready>
    <h1>{{ t('spaces.title') }}</h1><p>{{ t('spaces.description') }}</p>
    <el-alert v-if="!supported" type="warning" :closable="false" :title="t('spaces.unsupported')" />
    <el-alert v-if="error" type="error" :closable="false" :title="error" />
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <div class="space-grid">
      <el-card v-for="item in items" :key="item.orgid" shadow="never">
        <h2><el-icon><User v-if="item.orgid === -1" /><OfficeBuilding v-else /></el-icon> {{ item.orgid === -1 ? t('spaces.personal') : item.name }}</h2>
        <p>{{ t(`spaces.role.${item.role}`) }}</p>
        <el-button :type="item.orgid === auth.orgid ? 'default' : 'primary'" :disabled="busy || !supported || item.orgid === auth.orgid || item.status !== 'active'" :loading="busy" @click="select(item.orgid)">{{ item.orgid === auth.orgid ? t('spaces.current') : t('spaces.enter') }}</el-button>
      </el-card>
    </div>
  </main></ContentZoom></div>
</template>

<style scoped>
.space-main { width: min(100% - 40px, 1000px); margin: 32px auto; padding-bottom: 100px; }
.space-main > p { color: var(--el-text-color-secondary); line-height: 1.6; }
.space-main :deep(.el-alert) { margin: 16px 0; }
.space-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 20px; margin-top: 24px; }
.space-grid h2 { font-size: 18px; display: flex; gap: 8px; align-items: center; }
</style>
