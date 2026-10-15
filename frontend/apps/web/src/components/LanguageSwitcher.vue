<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { setLocale } from '../i18n'

const { locale, t } = useI18n()
const selected = computed(() => locale.value)
</script>

<template>
  <el-select :model-value="selected" class="language-switcher" :aria-label="t('common.language')" @change="setLocale($event as 'zh-CN' | 'en-US')">
    <el-option value="zh-CN" :label="t('common.chinese')" />
    <el-option value="en-US" :label="t('common.english')" />
  </el-select>
</template>
