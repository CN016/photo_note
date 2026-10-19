<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import type { UploadFile } from 'element-plus'
import { ApiError } from '../api/types'
import BrandMark from '../components/BrandMark.vue'
import LanguageSwitcher from '../components/LanguageSwitcher.vue'
import ThemeSwitcher from '../components/ThemeSwitcher.vue'
import { prepareImageUpload, type PreparedImageUpload } from '../imageUpload'
import { useAuthStore } from '../stores/auth'
import { defaultAuthenticatedRoute } from '../router'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const phone = ref('')
const password = ref('')
const nickname = ref('')
const avatar = ref<PreparedImageUpload | null>(null)
const step = ref(1)
const busy = ref(false)
const error = ref('')

function errorText(value: unknown): string {
  if (value instanceof ApiError) {
    if (value.code === 'network') return t('common.errors.network')
    const path = `common.errors.${value.code}`
    const translated = t(path)
    return translated === path ? t('common.errors.requestFailed') : translated
  }
  return t('common.errors.requestFailed')
}

function validDetails(): boolean {
  if (!/^\d{11}$/.test(phone.value)) { error.value = t('register.phoneInvalid'); return false }
  if (Array.from(password.value).length < 8 || Array.from(password.value).length > 128) { error.value = t('register.passwordInvalid'); return false }
  if (Array.from(nickname.value.trim()).length < 1 || Array.from(nickname.value.trim()).length > 64) { error.value = t('register.nicknameInvalid'); return false }
  return true
}

function continueToAvatar() {
  error.value = ''
  if (validDetails()) step.value = 2
}

async function chooseAvatar(file: UploadFile) {
  error.value = ''
  if (!file.raw) return
  busy.value = true
  try {
    const prepared = await prepareImageUpload(file.raw)
    if (avatar.value) URL.revokeObjectURL(avatar.value.previewUrl)
    avatar.value = prepared
  } catch { error.value = t('register.avatarInvalid') }
  finally { busy.value = false }
}

async function submit() {
  error.value = ''
  if (!validDetails()) { step.value = 1; return }
  if (!avatar.value) { error.value = t('register.avatarRequired'); return }
  busy.value = true
  try {
    await auth.signUp(phone.value, password.value, nickname.value.trim(), avatar.value.blob, avatar.value.filename)
    await router.replace(defaultAuthenticatedRoute())
  } catch (cause) { error.value = errorText(cause) }
  finally { busy.value = false }
}

onBeforeUnmount(() => { if (avatar.value) URL.revokeObjectURL(avatar.value.previewUrl) })
</script>

<template>
  <main class="auth-page register-page">
    <section class="auth-art register-art" aria-hidden="true">
      <div class="art-brand"><BrandMark /><span>{{ t('common.brand') }}</span></div>
      <div class="art-copy"><p class="eyebrow">{{ t('register.eyebrow') }}</p><h1>{{ t('register.title') }}</h1><p>{{ t('register.subtitle') }}</p></div>
      <div class="register-art-note">{{ t('register.artCaption') }}</div>
      <p class="art-caption">{{ t('common.footer') }}</p>
    </section>
    <section class="auth-panel">
      <div class="auth-topbar"><RouterLink class="mobile-brand" to="/login"><BrandMark />{{ t('common.brand') }}</RouterLink><div class="auth-top-actions"><ThemeSwitcher /><LanguageSwitcher /></div></div>
      <div class="auth-form-wrap register-form-wrap">
        <div class="auth-heading"><p class="eyebrow">{{ t('register.formHint') }} · {{ step }}/2</p><h2>{{ step === 1 ? t('register.formTitle') : t('register.avatarTitle') }}</h2><p class="register-step-help">{{ step === 1 ? t('register.detailsHelp') : t('register.avatarHelp') }}</p></div>
        <el-form class="auth-form" label-position="top" @submit.prevent="step === 1 ? continueToAvatar() : submit()">
          <template v-if="step === 1">
            <el-form-item :label="t('register.phone')"><el-input v-model="phone" type="tel" inputmode="numeric" autocomplete="tel-national" maxlength="11" :placeholder="t('register.phonePlaceholder')" @input="phone = phone.replace(/\D/g, '').slice(0, 11)" /></el-form-item>
            <el-form-item :label="t('register.password')"><el-input v-model="password" type="password" show-password autocomplete="new-password" :placeholder="t('register.passwordPlaceholder')" /></el-form-item>
            <el-form-item :label="t('register.nickname')"><el-input v-model="nickname" maxlength="64" autocomplete="nickname" :placeholder="t('register.nicknamePlaceholder')" /></el-form-item>
          </template>
          <template v-else>
            <div class="avatar-upload-stage">
              <el-avatar :size="112" :src="avatar?.previewUrl">{{ nickname.slice(0, 1) }}</el-avatar>
              <el-upload action="#" :auto-upload="false" :show-file-list="false" accept="image/*,.heic,.heif" :on-change="chooseAvatar">
                <el-button type="primary" plain :loading="busy">{{ avatar ? t('register.avatarReplace') : t('register.avatarChoose') }}</el-button>
              </el-upload>
              <span>{{ t('register.avatarFormats') }}</span>
            </div>
          </template>
          <p v-if="error" class="form-error" role="alert">{{ error }}</p>
          <div class="register-actions">
            <el-button v-if="step === 2" @click="step = 1">{{ t('common.back') }}</el-button>
            <el-button class="register-submit" type="primary" native-type="submit" :loading="busy" :disabled="step === 2 && !avatar">
              {{ step === 1 ? t('register.continue') : (busy ? t('register.submitting') : t('register.submit')) }}
            </el-button>
          </div>
        </el-form>
        <p class="auth-switch">{{ t('register.existing') }} <RouterLink to="/login">{{ t('register.signIn') }}</RouterLink></p>
      </div>
      <p class="auth-footnote">{{ t('common.copyright') }}</p>
    </section>
  </main>
</template>
