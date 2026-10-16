<script setup lang="ts">
import { computed } from 'vue'
import type { NoticeConfig } from '../api/types'

/** 仅展示用户选择的 webhook 平台标识；平台名称仍由页面文案提供。 */
const props = defineProps<{
  provider: NoticeConfig['provider']
  size?: 'small' | 'default'
}>()

/** 将渠道类型映射到仓库内保存的官方应用图标，避免依赖第三方运行时图片地址。 */
const logoSource = computed(() => ({
  wecom: '/brand-logos/wecom.jpg',
  dingtalk: '/brand-logos/dingtalk.jpg',
  feishu: '/brand-logos/feishu.jpg',
}[props.provider]))
</script>

<template>
  <img
    class="webhook-provider-logo"
    :class="{ 'webhook-provider-logo-small': size === 'small' }"
    :src="logoSource"
    alt=""
    aria-hidden="true"
    loading="lazy"
    decoding="async"
  >
</template>
