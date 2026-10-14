<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { setLocale } from '../i18n'

const { locale, t } = useI18n()
const selected = computed(() => locale.value)
</script>

<template>
  <label class="language-switcher">
    <span class="sr-only">{{ t('common.language') }}</span>
    <select :value="selected" :aria-label="t('common.language')" @change="setLocale(($event.target as HTMLSelectElement).value as 'zh-CN' | 'en-US')">
      <option value="zh-CN">{{ t('common.chinese') }}</option>
      <option value="en-US">{{ t('common.english') }}</option>
    </select>
  </label>
</template>
