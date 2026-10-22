<script setup lang="ts">
import { CameraFilled } from '@element-plus/icons-vue'

/** 用组件库相机图标表达取景入口，配色和留白由全局品牌样式统一维护。 */
defineProps<{ size?: 'small' | 'default' }>()
</script>

<template>
  <el-icon class="brand-mark" :class="{ 'brand-mark-small': size === 'small' }" aria-hidden="true">
    <CameraFilled />
  </el-icon>
</template>
