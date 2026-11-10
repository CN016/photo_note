<script setup lang="ts">
import { Camera } from '@element-plus/icons-vue'

/** 用组件库线性相机图标表达取景入口，与 Apple 系统图标的轻量线条保持一致。 */
defineProps<{ size?: 'small' | 'default' }>()
</script>

<template>
  <el-icon class="brand-mark" :class="{ 'brand-mark-small': size === 'small' }" aria-hidden="true">
    <Camera />
  </el-icon>
</template>
