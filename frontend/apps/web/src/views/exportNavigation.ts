/**
 * 图片导出成功后的历史回退步数。
 *
 * 属性面板会给同一张图片追加一条 `?panel=properties` 历史记录；普通导出成功时
 * 需要跨过该面板记录和图片详情，回到进入图片前的页面。水印模式已经关闭属性
 * 记录，因此只需回退一层。不能依赖 `history.state.position`：页面恢复、直接
 * 深链和 WebView 会话可能省略该字段，但仍可能存在由属性面板追加的历史记录。
 *
 * @param hasOwnedPropertiesEntry 当前是否仍停留在本页追加的属性面板记录
 * @returns 负整数历史步数
 */
export function imageExportBackSteps(hasOwnedPropertiesEntry: boolean): number {
  return hasOwnedPropertiesEntry ? -2 : -1
}
