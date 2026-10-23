<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import type { UploadFile } from 'element-plus'
import AppHeader from '../components/AppHeader.vue'
import { api } from '../api/client'
import { ApiError } from '../api/types'
import { prepareImageUpload, type PreparedImageUpload } from '../imageUpload'
import { useAuthStore } from '../stores/auth'
import { useAppShellStore } from '../stores/appShell'

const { t } = useI18n()
const auth = useAuthStore()
const appShell = useAppShellStore()
const nickname = ref(auth.user?.nickname ?? '')
const avatarUrl = computed(() => {
  const user = auth.user
  return user && appShell.avatarBusinessID === user.business_id && appShell.avatarStoreKey === user.avatar_key ? appShell.avatarSrc : ''
})
const pendingAvatar = ref<PreparedImageUpload | null>(null)
const profileBusy = ref(false)
const passwordBusy = ref(false)
const avatarBusy = ref(false)
const profileError = ref('')
const passwordError = ref('')
const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
onBeforeRouteLeave(to => to.path !== '/spaces' || (!profileBusy.value && !passwordBusy.value && !avatarBusy.value && !pendingAvatar.value && nickname.value === auth.user?.nickname && !currentPassword.value && !newPassword.value && !confirmPassword.value))

async function chooseAvatar(file: UploadFile) {
  if (!file.raw) return
  avatarBusy.value = true
  profileError.value = ''
  try {
    const next = await prepareImageUpload(file.raw)
    if (pendingAvatar.value) URL.revokeObjectURL(pendingAvatar.value.previewUrl)
    pendingAvatar.value = next
  } catch { profileError.value = t('register.avatarInvalid') }
  finally { avatarBusy.value = false }
}

async function saveProfile() {
  profileError.value = ''
  const trimmed = nickname.value.trim()
  if (Array.from(trimmed).length < 1 || Array.from(trimmed).length > 64) { profileError.value = t('register.nicknameInvalid'); return }
  profileBusy.value = true
  try {
    let avatarKey = auth.user?.avatar_key
    if (pendingAvatar.value) avatarKey = (await api.uploadAvatar(pendingAvatar.value.blob, pendingAvatar.value.filename)).storekey
    const user = await api.updateProfile({ nickname: trimmed, ...(avatarKey ? { avatar_key: avatarKey } : {}) })
    auth.user = user
    if (pendingAvatar.value) {
      if (user.avatar_key) appShell.cacheAvatar(user.business_id, user.avatar_key, pendingAvatar.value.blob)
      URL.revokeObjectURL(pendingAvatar.value.previewUrl)
      pendingAvatar.value = null
    }
    ElMessage.success(t('profile.profileSaved'))
  } catch (cause) {
    profileError.value = cause instanceof ApiError && cause.code === 'network' ? t('common.errors.network') : t('profile.profileError')
  } finally { profileBusy.value = false }
}

async function savePassword() {
  passwordError.value = ''
  if (Array.from(newPassword.value).length < 8 || Array.from(newPassword.value).length > 128) { passwordError.value = t('register.passwordInvalid'); return }
  if (newPassword.value !== confirmPassword.value) { passwordError.value = t('profile.passwordMismatch'); return }
  passwordBusy.value = true
  try {
    await api.changePassword(currentPassword.value, newPassword.value)
    currentPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    ElMessage.success(t('profile.passwordChanged'))
  } catch { passwordError.value = t('profile.passwordError') }
  finally { passwordBusy.value = false }
}

onBeforeUnmount(() => {
  if (pendingAvatar.value) URL.revokeObjectURL(pendingAvatar.value.previewUrl)
})
</script>

<template>
  <div class="workspace-page">
    <AppHeader />
    <main class="profile-main">
      <header class="profile-heading"><p class="eyebrow">{{ t('profile.eyebrow') }}</p><h1>{{ t('profile.title') }}</h1><p>{{ t('profile.subtitle') }}</p></header>
      <div class="profile-grid">
        <section class="profile-card">
          <h2>{{ t('profile.eyebrow') }}</h2>
          <div class="profile-avatar-row">
            <el-avatar :size="88" :src="pendingAvatar?.previewUrl || avatarUrl">{{ nickname.slice(0, 1) }}</el-avatar>
            <div><span class="field-label">{{ t('profile.avatar') }}</span><el-upload action="#" :auto-upload="false" :show-file-list="false" accept="image/*,.heic,.heif" :on-change="chooseAvatar"><el-button :loading="avatarBusy">{{ t('profile.avatarChange') }}</el-button></el-upload><small>{{ t('profile.avatarHelp') }}</small></div>
          </div>
          <el-form label-position="top" @submit.prevent="saveProfile">
            <el-form-item :label="t('profile.nickname')"><el-input v-model="nickname" maxlength="64" :placeholder="t('profile.nicknamePlaceholder')" /></el-form-item>
            <el-form-item :label="t('login.phone')"><el-input :model-value="auth.user?.phone || ''" disabled /></el-form-item>
            <p v-if="profileError" class="form-error" role="alert">{{ profileError }}</p>
            <el-button type="primary" native-type="submit" :loading="profileBusy">{{ t('profile.saveProfile') }}</el-button>
          </el-form>
        </section>
        <section class="profile-card">
          <h2>{{ t('profile.passwordTitle') }}</h2>
          <el-form label-position="top" @submit.prevent="savePassword">
            <el-form-item :label="t('profile.currentPassword')"><el-input v-model="currentPassword" type="password" show-password autocomplete="current-password" /></el-form-item>
            <el-form-item :label="t('profile.newPassword')"><el-input v-model="newPassword" type="password" show-password autocomplete="new-password" :placeholder="t('profile.passwordPlaceholder')" /></el-form-item>
            <el-form-item :label="t('profile.confirmPassword')"><el-input v-model="confirmPassword" type="password" show-password autocomplete="new-password" /></el-form-item>
            <p v-if="passwordError" class="form-error" role="alert">{{ passwordError }}</p>
            <el-button type="primary" native-type="submit" :loading="passwordBusy">{{ t('profile.passwordSave') }}</el-button>
          </el-form>
        </section>
      </div>
    </main>
  </div>
</template>
