import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

const ANCHOR_DISPLAY_SIZE_KEY = 'photo-nini.anchor-display-size'
export const MIN_ANCHOR_DISPLAY_SIZE = 70
export const MAX_ANCHOR_DISPLAY_SIZE = 150
export const DEFAULT_ANCHOR_DISPLAY_SIZE = 100

/** 将本地锚点大小限制在界面滑杆支持的范围，并按五个百分点对齐。 */
function normalizeAnchorDisplaySize(value: number): number {
  const clamped = Math.max(MIN_ANCHOR_DISPLAY_SIZE, Math.min(MAX_ANCHOR_DISPLAY_SIZE, value))
  return Math.round(clamped / 5) * 5
}

/** 读取当前浏览器保存的展示偏好；存储不可用或内容损坏时使用默认值。 */
function readAnchorDisplaySize(): number {
  try {
    const value = Number(window.localStorage.getItem(ANCHOR_DISPLAY_SIZE_KEY))
    return Number.isFinite(value) && value > 0 ? normalizeAnchorDisplaySize(value) : DEFAULT_ANCHOR_DISPLAY_SIZE
  } catch { return DEFAULT_ANCHOR_DISPLAY_SIZE }
}

/** 为详情与编辑画布共享本地锚点展示比例，偏好不写入图片文档或服务端。 */
export const useAnchorDisplayStore = defineStore('anchor-display', () => {
  const size = ref(readAnchorDisplaySize())

  watch(size, value => {
    try { window.localStorage.setItem(ANCHOR_DISPLAY_SIZE_KEY, String(value)) }
    catch { /* 本地存储被禁用时仍允许本次页面使用内存偏好。 */ }
  }, { flush: 'sync' })

  /** 更新锚点展示比例并同步保存，避免各页面分别维护显示尺寸。 */
  function setSize(value: number): void {
    if (Number.isFinite(value)) size.value = normalizeAnchorDisplaySize(value)
  }

  return { size, setSize }
})
