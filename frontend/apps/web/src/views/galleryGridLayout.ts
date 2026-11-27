/** 图库进入时的默认列数；双指缩放和窗口变化都以该列数为基准。 */
export const DEFAULT_GALLERY_COLUMNS = 4
/** 图库缩略图默认圆角，使用小圆角保留照片面积而不是形成大卡片轮廓。 */
export const GALLERY_THUMBNAIL_RADIUS = 3
/** 桌面五列网格的缩略图间距。 */
export const GALLERY_GRID_GAP = 6
/** 手机五列网格的缩略图间距；按设备像素比放大后仍保持紧凑。 */
export const GALLERY_MOBILE_GRID_GAP = 4
/** 双指缩放允许吸附的列数档位。 */
export const GALLERY_COLUMN_STEPS = [1, 2, 3, 4, 5, 10, 15, 20, 25, 30] as const

/**
 * 将连续缩放结果吸附到最近的产品列数档位。
 *
 * @param desired 根据缩放手势得到的理想列数
 * @returns 最接近的允许列数；非法值回退到默认四列
 */
export function nearestGalleryColumnCount(desired: number): number {
  if (!Number.isFinite(desired) || desired <= 0) return DEFAULT_GALLERY_COLUMNS
  return GALLERY_COLUMN_STEPS.reduce(
    (best, candidate) => Math.abs(candidate - desired) < Math.abs(best - desired) ? candidate : best,
    GALLERY_COLUMN_STEPS[0],
  )
}
