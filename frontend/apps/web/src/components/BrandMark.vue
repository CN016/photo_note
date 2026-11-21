<script setup lang="ts">
/** 品牌入口使用同一份 LightNote 矢量图标，避免各平台出现不同标识。 */
defineProps<{ size?: 'small' | 'default' }>()
</script>

<template>
  <img
    class="brand-mark"
    :class="{ 'brand-mark-small': size === 'small' }"
    src="/lightnote-icon.svg"
    alt=""
    width="40"
    height="40"
    draggable="false"
  />
</template>
