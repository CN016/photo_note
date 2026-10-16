<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Moon, Sunny } from '@element-plus/icons-vue'
import { currentTheme, toggleTheme } from '../theme'

const { t } = useI18n()
const theme = ref(currentTheme())
/** 更新共享根主题，并同步刷新当前页面的切换按钮状态。 */
function changeTheme() { theme.value = toggleTheme() }
</script>

<template>
  <el-button class="theme-switcher" text :aria-label="theme === 'dark' ? t('common.switchToLight') : t('common.switchToDark')" :title="theme === 'dark' ? t('common.switchToLight') : t('common.switchToDark')" @click="changeTheme">
    <el-icon aria-hidden="true"><component :is="theme === 'dark' ? Sunny : Moon" /></el-icon>
    <span class="theme-switcher-text">{{ theme === 'dark' ? t('common.lightTheme') : t('common.darkTheme') }}</span>
  </el-button>
</template>
