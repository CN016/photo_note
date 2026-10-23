<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import AppHeader from '../components/AppHeader.vue'
import { api } from '../api/client'
import type { Organization, OrganizationMember } from '../api/types'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const platform = computed(() => Boolean(auth.user?.is_admin))
const organizations = ref<Organization[]>([])
const members = ref<OrganizationMember[]>([])
const selected = ref<Organization | null>(null)
const page = ref(1)
const total = ref(0)
const memberPage = ref(1)
const memberTotal = ref(0)
const search = ref('')
const name = ref('')
const initialAdmin = ref('')
const memberID = ref('')
const memberRole = ref<'admin' | 'member'>('member')
const busy = ref(false)
const error = ref('')
const createVisible = ref(false)
onBeforeRouteLeave(to => to.path !== '/spaces' || (!busy.value && !createVisible.value && !memberID.value.trim()))
const managedOrgID = computed(() => platform.value ? selected.value?.orgid : undefined)

/** 管理调用由服务端重新校验角色；页面不把显示权限作为可信授权边界。 */
async function operation(action: () => Promise<void>) {
  busy.value = true; error.value = ''
  try { await action() } catch { error.value = t('organizations.operationError') }
  finally { busy.value = false }
}
async function load() {
  await operation(async () => {
    if (platform.value) { const result = await api.organizations(page.value, search.value); organizations.value = result.items; total.value = result.total }
    else { selected.value = await api.currentOrganization(); await loadMembers() }
  })
}
async function loadMembers() {
  const result = await api.organizationMembers(managedOrgID.value, memberPage.value)
  members.value = result.items; memberTotal.value = result.total
}
async function select(item: Organization) { selected.value = item; memberPage.value = 1; await operation(loadMembers) }
async function create() {
  await operation(async () => { await api.createOrganization(name.value.trim(), initialAdmin.value.trim()); createVisible.value = false; name.value = ''; initialAdmin.value = '' })
  await load()
}
async function rename(item: Organization) {
  try {
    const result = await ElMessageBox.prompt(t('organizations.name'), t('organizations.rename'), { inputValue: item.name, confirmButtonText: t('common.save'), cancelButtonText: t('common.cancel'), inputValidator: value => Boolean(value?.trim()) })
    await operation(async () => {
      const updated = platform.value ? await api.updateOrganization(item.orgid, { name: result.value.trim() }) : await api.renameCurrentOrganization(result.value.trim())
      if (selected.value?.orgid === item.orgid) selected.value = updated
    })
    await load()
  } catch { /* 用户取消不改变组织资料。 */ }
}
async function toggle(item: Organization) {
  await operation(async () => { await api.updateOrganization(item.orgid, { status: item.status === 'active' ? 'disabled' : 'active' }) })
  await load()
}
async function add() {
  await operation(async () => { await api.addOrganizationMember(memberID.value.trim(), memberRole.value, managedOrgID.value); memberID.value = ''; await loadMembers() })
}
async function role(item: OrganizationMember) {
  await operation(async () => { await api.setOrganizationMemberRole(item.user_id, item.role === 'admin' ? 'member' : 'admin', managedOrgID.value); await loadMembers() })
}
async function remove(item: OrganizationMember) {
  try {
    await ElMessageBox.confirm(t('organizations.removeConfirm'), t('organizations.remove'), { confirmButtonText: t('organizations.remove'), cancelButtonText: t('common.cancel'), type: 'warning' })
    await operation(async () => { await api.removeOrganizationMember(item.user_id, managedOrgID.value); await loadMembers() })
  } catch { /* 取消移除时保留成员。 */ }
}
onMounted(load)
</script>

<template>
  <div class="workspace-page"><AppHeader /><main class="organization-main">
    <header class="organization-toolbar"><div><h1>{{ t('organizations.title') }}</h1><p>{{ t('organizations.description') }}</p></div><el-button v-if="platform" type="primary" :icon="Plus" @click="createVisible = true">{{ t('organizations.create') }}</el-button></header>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <template v-if="platform">
      <el-form inline @submit.prevent="page = 1; load()"><el-form-item><el-input v-model="search" :placeholder="t('organizations.search')" clearable /></el-form-item><el-button :icon="Search" @click="page = 1; load()">{{ t('organizations.search') }}</el-button></el-form>
      <el-table :data="organizations" v-loading="busy" :empty-text="t('organizations.empty')">
        <el-table-column prop="orgid" :label="t('organizations.orgid')" width="100" /><el-table-column prop="name" :label="t('organizations.name')" />
        <el-table-column :label="t('organizations.status')"><template #default="{ row }">{{ t(`organizations.${row.status}`) }}</template></el-table-column>
        <el-table-column :label="t('organizations.actions')" min-width="260"><template #default="{ row }"><el-button text @click="select(row)">{{ t('organizations.members') }}</el-button><el-button text @click="rename(row)">{{ t('organizations.rename') }}</el-button><el-button text @click="toggle(row)">{{ t(row.status === 'active' ? 'organizations.disable' : 'organizations.enable') }}</el-button><RouterLink :to="{ path: '/atelier-7f3c9a2e', query: { tab: 'inspirations', orgid: row.orgid } }">{{ t('organizations.gallery') }}</RouterLink></template></el-table-column>
      </el-table>
      <el-pagination v-if="total > 20" v-model:current-page="page" :total="total" :page-size="20" layout="prev,pager,next" @current-change="load" />
    </template>
    <section v-if="selected" class="organization-members">
      <header class="organization-toolbar"><h2>{{ selected.name }} · {{ t('organizations.members') }}</h2><el-button v-if="!platform" @click="rename(selected)">{{ t('organizations.rename') }}</el-button></header>
      <p>{{ t('organizations.memberHint') }}</p>
      <el-form inline @submit.prevent="add"><el-form-item :label="t('organizations.userID')"><el-input v-model="memberID" :placeholder="t('organizations.userID')" /></el-form-item><el-form-item><el-select v-model="memberRole" style="width: 140px"><el-option value="member" :label="t('organizations.member')" /><el-option value="admin" :label="t('organizations.admin')" /></el-select></el-form-item><el-button type="primary" :disabled="!memberID.trim() || busy" @click="add">{{ t('organizations.addMember') }}</el-button></el-form>
      <el-table :data="members" v-loading="busy" :empty-text="t('organizations.noMembers')"><el-table-column prop="nickname" :label="t('organizations.nickname')" /><el-table-column prop="user_id" :label="t('organizations.userID')" min-width="260" /><el-table-column :label="t('organizations.role')"><template #default="{ row }">{{ t(`organizations.${row.role}`) }}</template></el-table-column><el-table-column :label="t('organizations.actions')" min-width="220"><template #default="{ row }"><el-button text :disabled="busy" @click="role(row)">{{ t(row.role === 'admin' ? 'organizations.demote' : 'organizations.promote') }}</el-button><el-button text type="danger" :disabled="busy" @click="remove(row)">{{ t('organizations.remove') }}</el-button></template></el-table-column></el-table>
      <el-pagination v-if="memberTotal > 20" v-model:current-page="memberPage" :total="memberTotal" :page-size="20" layout="prev,pager,next" @current-change="operation(loadMembers)" />
    </section>
    <el-dialog v-model="createVisible" :title="t('organizations.create')" width="min(520px, 94vw)"><el-form label-position="top"><el-form-item :label="t('organizations.name')"><el-input v-model="name" maxlength="120" /></el-form-item><el-form-item :label="t('organizations.initialAdmin')"><el-input v-model="initialAdmin" /></el-form-item><p>{{ t('organizations.initialAdminHint') }}</p></el-form><template #footer><el-button @click="createVisible = false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="busy" :disabled="!name.trim() || !initialAdmin.trim()" @click="create">{{ t('organizations.create') }}</el-button></template></el-dialog>
  </main></div>
</template>

<style scoped>
.organization-main { width: min(100% - 40px, 1200px); margin: 32px auto; padding-bottom: 100px; }
.organization-toolbar { display: flex; justify-content: space-between; align-items: center; gap: 16px; flex-wrap: wrap; }
.organization-main p { color: var(--el-text-color-secondary); line-height: 1.6; }
.organization-main :deep(.el-alert), .organization-main :deep(.el-pagination) { margin: 20px 0; }
.organization-members { margin-top: 32px; padding-top: 16px; border-top: 1px solid var(--el-border-color); }
.organization-main :deep(.el-table) { border: 1px solid var(--el-border-color); border-radius: 12px; }
</style>
