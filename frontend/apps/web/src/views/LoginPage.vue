<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '../api/types'
import LanguageSwitcher from '../components/LanguageSwitcher.vue'
import ThemeSwitcher from '../components/ThemeSwitcher.vue'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
const phone = ref('')
const password = ref('')
const showPassword = ref(false)
const busy = ref(false)
const error = ref('')
const phoneValid = computed(() => /^\+[1-9]\d{1,14}$/.test(phone.value.trim()))

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

/** 校验手机号和密码长度后调用后端登录入口。 */
async function submit() {
  error.value = ''
  if (!phoneValid.value) { error.value = t('login.phoneInvalid'); return }
  if (Array.from(password.value).length < 8 || Array.from(password.value).length > 128) { error.value = t('common.errors.validation_failed'); return }
  busy.value = true
  try { await auth.signIn(phone.value.trim(), password.value); await router.replace('/inspirations') }
  catch (cause) { error.value = errorText(cause) }
  finally { busy.value = false }
}
</script>

<template>
  <main class="auth-page">
    <section class="auth-art" aria-hidden="true">
      <div class="art-brand"><span class="brand-mark">n.</span><span>{{ t('common.brand') }}</span></div>
      <div class="art-copy"><p class="eyebrow">{{ t('login.eyebrow') }}</p><h1>{{ t('login.title') }}</h1><p>{{ t('login.subtitle') }}</p></div>
      <div class="moodboard">
        <div class="mood-card mood-card-one"><div class="sun-disc"></div><div class="sand-hill"></div><span>{{ t('login.moodOne') }}</span></div>
        <div class="mood-card mood-card-two"><div class="leaf-shape"></div><span>{{ t('login.moodTwo') }}</span></div>
        <div class="mood-card mood-card-three"><span>{{ t('login.moodThree') }}</span><i>✳</i></div>
      </div>
      <p class="art-caption">{{ t('login.artCaption') }}</p>
    </section>
    <section class="auth-panel">
      <div class="auth-topbar"><RouterLink class="mobile-brand" to="/login"><span class="brand-mark">n.</span>{{ t('common.brand') }}</RouterLink><div class="auth-top-actions"><ThemeSwitcher /><LanguageSwitcher /></div></div>
      <div class="auth-form-wrap">
        <div class="auth-heading"><p class="eyebrow">{{ t('login.formHint') }}</p><h2>{{ t('login.formTitle') }}</h2></div>
        <form class="auth-form" @submit.prevent="submit" novalidate>
          <label class="field"><span>{{ t('login.phone') }}</span><input v-model="phone" name="phone" type="tel" inputmode="tel" autocomplete="tel" :placeholder="t('login.phonePlaceholder')" required aria-describedby="phone-help" /></label>
          <p id="phone-help" class="field-help">{{ t('login.phoneInvalid') }}</p>
          <label class="field"><span>{{ t('login.password') }}</span><span class="password-input"><input v-model="password" name="password" :type="showPassword ? 'text' : 'password'" autocomplete="current-password" :placeholder="t('login.passwordPlaceholder')" required /><button class="password-toggle" type="button" :aria-label="showPassword ? t('login.hidePassword') : t('login.showPassword')" @click="showPassword = !showPassword">{{ showPassword ? '◉' : '◎' }}</button></span></label>
          <p v-if="error" class="form-error" role="alert">{{ error }}</p>
          <button class="primary-button full-button" type="submit" :disabled="busy">{{ busy ? t('login.submitting') : t('login.submit') }}<span aria-hidden="true">↗</span></button>
        </form>
        <p class="auth-switch">{{ t('login.newHere') }} <RouterLink to="/register">{{ t('login.createAccount') }}</RouterLink></p>
      </div>
      <p class="auth-footnote">{{ t('common.copyright') }}</p>
    </section>
  </main>
</template>
