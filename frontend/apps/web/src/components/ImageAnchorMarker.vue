<script setup lang="ts">
/**
 * 图片锚点的固定落点标记。
 *
 * 圆点中心始终位于锚点坐标，黄色短线从圆点中心向右展开。标记不参与布局测量，
 * 因此标题换行、描述出现或编辑条展开都不会改变锚点落点。
 */
defineProps<{ interactive?: boolean }>()
</script>

<template>
  <span class="anchor-marker" :class="{ 'is-interactive': interactive }" aria-hidden="true">
    <span class="anchor-marker-dot"></span>
    <span class="anchor-marker-rule"></span>
  </span>
</template>

<style scoped>
.anchor-marker {
  position: absolute;
  z-index: 2;
  top: 0;
  left: 0;
  width: var(--anchor-marker-rule-width, 42px);
  height: var(--anchor-marker-dot-size, 8px);
  pointer-events: none;
  transform: translateY(-50%);
}

.anchor-marker.is-interactive {
  cursor: grab;
  pointer-events: auto;
  touch-action: none;
}

.anchor-marker-dot,
.anchor-marker-rule {
  position: absolute;
  top: 50%;
  background: #f3e600;
  transform: translateY(-50%);
}

.anchor-marker-dot {
  left: calc(var(--anchor-marker-dot-size, 8px) / -2);
  width: var(--anchor-marker-dot-size, 8px);
  height: var(--anchor-marker-dot-size, 8px);
  border: 1px solid rgb(255 255 255 / 65%);
  border-radius: 50%;
  box-shadow: 0 1px 4px rgb(0 0 0 / 38%);
}

.anchor-marker-rule {
  left: 0;
  width: 100%;
  height: max(2px, calc(var(--anchor-marker-dot-size, 8px) * .22));
  border-radius: 999px;
  box-shadow: 0 1px 3px rgb(0 0 0 / 28%);
}
</style>
