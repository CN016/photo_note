import type { MenuEntry } from './menu'

/**
 * 构造灵感库图片长按菜单。
 *
 * 菜单只保留当前图片操作，不加载或展示关联策划。复制不可用时仍显示禁用项，
 * 让用户明确看到当前入口，而不是出现空菜单。
 */
export function createGalleryImageMenu(
  copyLabel: string,
  canCopy: boolean,
  copy: () => void | Promise<void>,
): MenuEntry[] {
  return [{ label: copyLabel, disabled: !canCopy, run: copy }]
}
