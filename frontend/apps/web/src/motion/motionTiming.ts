/** 开合与像素交接共180ms；不限制GSAP刷新频率，随设备的requestAnimationFrame更新。 */
export const IMAGE_EXPAND_SECONDS = 0.16
export const IMAGE_HANDOFF_SECONDS = 0.02
/** 用户要求匀速开合，窗口和图片边界共用线性插值，不施加缓动或回弹。 */
export const IMAGE_EXPAND_EASE = 'none'
