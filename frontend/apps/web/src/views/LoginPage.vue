<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '../api/types'
import { ArrowRight, StarFilled } from '@element-plus/icons-vue'
import BrandMark from '../components/BrandMark.vue'
import LanguageSwitcher from '../components/LanguageSwitcher.vue'
import ThemeSwitcher from '../components/ThemeSwitcher.vue'
import { useAuthStore } from '../stores/auth'
import { defaultAuthenticatedRoute } from '../router'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
const phone = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')
const phoneValid = computed(() => /^\d{11}$/.test(phone.value.trim()))

onMounted(() => { if (route.query.logout === 'failed') error.value = t('common.logoutFailed') })

/** 把认证错误转换成登录页语言资源，不展示未经翻译的服务端原文。 */
function errorText(value: unknown): string {
  if (value instanceof ApiError) {
    if (value.code === 'network') return t('common.errors.network')
    if (value.code === 'invalid_credentials' || value.code === 'unauthorized') return t('login.loginFailed')
    const path = `common.errors.${value.code}`
    const translated = t(path)
    return translated === path ? t('common.errors.requestFailed') : translated
  }
  return t('common.errors.requestFailed')
}

/** 页面重载后网络短暂失败时保留凭据并重建内存store，只有明确退出才回到账号表单。 */
async function retrySession() {
  if (auth.busy || !auth.recoveryPending) return
  await auth.initialize()
  if (auth.user) await router.replace(defaultAuthenticatedRoute())
}
/** 用户明确放弃恢复时才退出；离线撤销失败也清理本机状态，并显示已翻译提示。 */
async function resetSession() {
  try { await auth.signOut() } catch { error.value = t('common.logoutFailed') }
}
onMounted(() => window.addEventListener('focus', retrySession))
onUnmounted(() => window.removeEventListener('focus', retrySession))

/** 校验手机号和密码长度后调用后端登录入口。 */
async function submit() {
  error.value = ''
  if (!phoneValid.value) { error.value = t('login.phoneInvalid'); return }
  if (Array.from(password.value).length < 8 || Array.from(password.value).length > 128) { error.value = t('common.errors.validation_failed'); return }
  busy.value = true
  try { await auth.signIn(phone.value.trim(), password.value); await router.replace(defaultAuthenticatedRoute()) }
  catch (cause) { error.value = errorText(cause) }
  finally { busy.value = false }
}
</script>

<template>
  <main class="auth-page">
    <section class="auth-art" aria-hidden="true">
      <div class="art-brand"><BrandMark /><span>{{ t('common.brand') }}</span></div>
      <div class="art-copy"><p class="eyebrow">{{ t('login.eyebrow') }}</p><h1>{{ t('login.title') }}</h1><p>{{ t('login.subtitle') }}</p></div>
      <div class="moodboard">
        <div class="mood-card mood-card-one"><div class="sun-disc"></div><div class="sand-hill"></div><span>{{ t('login.moodOne') }}</span></div>
        <div class="mood-card mood-card-two"><div class="leaf-shape"></div><span>{{ t('login.moodTwo') }}</span></div>
        <div class="mood-card mood-card-three"><span>{{ t('login.moodThree') }}</span><el-icon class="mood-decoration-icon"><StarFilled /></el-icon></div>
      </div>
      <p class="art-caption">{{ t('login.artCaption') }}</p>
    </section>
    <section class="auth-panel">
      <div class="auth-topbar"><RouterLink class="mobile-brand" to="/login"><BrandMark />{{ t('common.brand') }}</RouterLink><div class="auth-top-actions"><ThemeSwitcher /><LanguageSwitcher /></div></div>
      <div class="auth-form-wrap">
        <div class="auth-heading"><p class="eyebrow">{{ t('login.formHint') }}</p><h2>{{ t('login.formTitle') }}</h2></div>
        <section v-if="auth.recoveryPending" class="auth-form" role="status" aria-live="polite">
          <h3>{{ t('login.sessionRecoveryTitle') }}</h3>
          <p>{{ t('login.sessionRecoveryMessage') }}</p>
          <el-button class="primary-button full-button" type="primary" :loading="auth.busy" @click="retrySession">{{ t('login.sessionRecoveryRetry') }}</el-button>
          <el-button text :disabled="auth.busy" @click="resetSession">{{ t('login.sessionRecoverySignIn') }}</el-button>
        </section>
        <form v-else class="auth-form" @submit.prevent="submit" novalidate>
          <el-form-item :label="t('login.phone')"><el-input v-model="phone" name="phone" type="tel" inputmode="numeric" autocomplete="tel-national" maxlength="11" :placeholder="t('login.phonePlaceholder')" aria-describedby="phone-help" @input="phone = phone.replace(/\D/g, '').slice(0, 11)" /></el-form-item>
          <p id="phone-help" class="field-help">{{ t('login.phoneInvalid') }}</p>
          <el-form-item :label="t('login.password')"><el-input v-model="password" name="password" type="password" show-password autocomplete="current-password" :placeholder="t('login.passwordPlaceholder')" /></el-form-item>
          <p v-if="error" class="form-error" role="alert">{{ error }}</p>
          <el-button class="primary-button full-button" type="primary" native-type="submit" :loading="busy">{{ busy ? t('login.submitting') : t('login.submit') }}<el-icon aria-hidden="true"><ArrowRight /></el-icon></el-button>
        </form>
        <p v-if="!auth.recoveryPending" class="auth-switch">{{ t('login.newHere') }} <RouterLink to="/register">{{ t('login.createAccount') }}</RouterLink></p>
      </div>
      <p class="auth-footnote">{{ t('common.copyright') }}</p>
    </section>
  </main>
</template>
