<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { LIGHT_PALETTES, setLightPalette, useLightPalette, type LightPalette } from '../theme'

const { t } = useI18n()
const palette = useLightPalette()
const options = computed(() => LIGHT_PALETTES.map(value => ({
  value,
  label: t(`common.paletteNames.${value}`),
})))

/** 点击色盘即进入对应浅色主题；按钮保留 44px 触控面积并暴露 pressed 状态。 */
function choose(value: LightPalette): void {
  setLightPalette(value)
}
</script>

<template>
  <div class="palette-switcher" role="group" :aria-label="t('common.lightPalette')">
    <button
      v-for="option in options"
      :key="option.value"
      type="button"
      class="palette-option"
      :class="`palette-${option.value}`"
      :aria-label="option.label"
      :aria-pressed="palette === option.value"
      @click="choose(option.value)"
    >
      <span class="palette-dot" aria-hidden="true" />
    </button>
  </div>
</template>

<style scoped>
.palette-switcher {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 1px;
  padding: 2px;
  border: 1px solid color-mix(in srgb, var(--line) 70%, transparent);
  border-radius: 999px;
  background: color-mix(in srgb, var(--surface-overlay) 28%, transparent);
}

.palette-option {
  display: grid;
  width: 44px;
  height: 44px;
  place-items: center;
  padding: 0;
  border: 0;
  border-radius: 50%;
  background: transparent;
  cursor: pointer;
}

.palette-option:hover {
  background: color-mix(in srgb, var(--surface-overlay) 38%, transparent);
}

.palette-option:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 1px;
}

.palette-dot {
  display: block;
  width: 22px;
  height: 22px;
  border: 1px solid rgb(255 255 255 / 86%);
  border-radius: 50%;
  box-shadow: 0 2px 7px rgb(45 54 48 / 16%), inset 0 1px 0 rgb(255 255 255 / 72%);
}

.palette-option[aria-pressed='true'] .palette-dot {
  box-shadow: 0 0 0 2px var(--surface-overlay), 0 0 0 4px var(--accent), inset 0 1px 0 rgb(255 255 255 / 82%);
}

.palette-green .palette-dot { background: #6f8f7e; }
.palette-aqua .palette-dot { background: #659a91; }
.palette-blue .palette-dot { background: #6e8fa8; }
.palette-gold .palette-dot { background: #b89b63; }

</style>
