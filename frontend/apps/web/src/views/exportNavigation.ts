/**
 * 图片导出成功后的历史回退步数。
 *
 * 属性面板会给同一张图片追加一条 `?panel=properties` 历史记录；普通导出成功时
 * 需要跨过该面板记录和图片详情，回到进入图片前的页面。水印模式已经关闭属性
 * 记录，因此只需回退一层。缺少有效历史位置时按一层处理，由调用方在无历史时
 * 回退到灵感库。
 *
 * @param hasOwnedPropertiesEntry 当前是否仍停留在本页追加的属性面板记录
 * @param historyPosition 浏览器当前历史位置；Vue Router 从 0 开始写入
 * @returns 负整数历史步数
 */
export function imageExportBackSteps(hasOwnedPropertiesEntry: boolean, historyPosition?: number): number {
  if (hasOwnedPropertiesEntry && Number.isSafeInteger(historyPosition) && (historyPosition ?? 0) >= 2) return -2
  return -1
}
