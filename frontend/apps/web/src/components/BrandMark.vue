<script setup lang="ts">
import { PictureFilled } from '@element-plus/icons-vue'

/** 使用 UI 图标库中的图片标记作为产品入口图形，不自绘品牌路径。 */
defineProps<{ size?: 'small' | 'default' }>()
</script>

<template>
  <el-icon class="brand-mark" :class="{ 'brand-mark-small': size === 'small' }" aria-hidden="true">
    <PictureFilled />
  </el-icon>
</template>
