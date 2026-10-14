<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ApiError } from '../api/types'
import LanguageSwitcher from '../components/LanguageSwitcher.vue'
import ThemeSwitcher from '../components/ThemeSwitcher.vue'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const phone = ref('')
const password = ref('')
const nickname = ref('')
const busy = ref(false)
const error = ref('')
const phoneValid = computed(() => /^\+[1-9]\d{1,14}$/.test(phone.value.trim()))

/** 把注册错误转换成本地化提示，避免直接显示未翻译的服务端文本。 */
function errorText(value: unknown): string {
  if (value instanceof ApiError) {
    if (value.code === 'network') return t('common.errors.network')
    const path = `common.errors.${value.code}`
    const translated = t(path)
    return translated === path ? t('common.errors.requestFailed') : translated
  }
  return t('common.errors.requestFailed')
}

/** 按后端长度与 E.164 规则校验表单后创建账号。 */
async function submit() {
  error.value = ''
  if (!phoneValid.value) { error.value = t('register.phoneInvalid'); return }
  if (Array.from(password.value).length < 8 || Array.from(password.value).length > 128) { error.value = t('register.passwordInvalid'); return }
  if (Array.from(nickname.value.trim()).length < 1 || Array.from(nickname.value.trim()).length > 64) { error.value = t('register.nicknameInvalid'); return }
  busy.value = true
  try { await auth.signUp(phone.value.trim(), password.value, nickname.value.trim()); await router.replace('/inspirations') }
  catch (cause) { error.value = errorText(cause) }
  finally { busy.value = false }
}
</script>

<template>
  <main class="auth-page">
    <section class="auth-art register-art" aria-hidden="true">
      <div class="art-brand"><span class="brand-mark">n.</span><span>{{ t('common.brand') }}</span></div>
      <div class="art-copy"><p class="eyebrow">{{ t('register.eyebrow') }}</p><h1>{{ t('register.title') }}</h1><p>{{ t('register.subtitle') }}</p></div>
      <div class="moodboard register-moodboard"><div class="mood-card mood-card-four"><span>{{ t('register.moodOne') }}</span><i>✳</i></div><div class="mood-card mood-card-five"><div class="blue-arch"></div><span>{{ t('register.moodTwo') }}</span></div><div class="mood-card mood-card-six"><div class="flower-dots">✿</div><span>{{ t('register.moodThree') }}</span></div></div>
      <p class="art-caption">{{ t('register.artCaption') }}</p>
    </section>
    <section class="auth-panel">
      <div class="auth-topbar"><RouterLink class="mobile-brand" to="/login"><span class="brand-mark">n.</span>{{ t('common.brand') }}</RouterLink><div class="auth-top-actions"><ThemeSwitcher /><LanguageSwitcher /></div></div>
      <div class="auth-form-wrap register-form-wrap">
        <div class="auth-heading"><p class="eyebrow">{{ t('register.formHint') }}</p><h2>{{ t('register.formTitle') }}</h2></div>
        <form class="auth-form" @submit.prevent="submit" novalidate>
          <label class="field"><span>{{ t('register.phone') }}</span><input v-model="phone" name="phone" type="tel" inputmode="tel" autocomplete="tel" :placeholder="t('register.phonePlaceholder')" required /></label>
          <label class="field"><span>{{ t('register.password') }}</span><input v-model="password" name="password" type="password" autocomplete="new-password" :placeholder="t('register.passwordPlaceholder')" required /></label>
          <label class="field"><span>{{ t('register.nickname') }}</span><input v-model="nickname" name="nickname" type="text" autocomplete="nickname" :placeholder="t('register.nicknamePlaceholder')" required /></label>
          <p v-if="error" class="form-error" role="alert">{{ error }}</p>
          <button class="primary-button full-button" type="submit" :disabled="busy">{{ busy ? t('register.submitting') : t('register.submit') }}<span aria-hidden="true">↗</span></button>
        </form>
        <p class="auth-switch">{{ t('register.existing') }} <RouterLink to="/login">{{ t('register.signIn') }}</RouterLink></p>
      </div>
      <p class="auth-footnote">{{ t('common.copyright') }}</p>
    </section>
  </main>
</template>
