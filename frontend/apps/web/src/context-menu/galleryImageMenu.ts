import type { MenuEntry } from './menu'

export interface GalleryMenuProject {
  business_id: string
  title: string
}

export interface GalleryImageMenuOptions {
  manageLabel: string
  manage: () => void | Promise<void>
  copyLabel: string
  canCopy: boolean
  copy: () => void | Promise<void>
  addLabel: string
  addProjects: GalleryMenuProject[]
  addStatus?: string
  addDisabled?: boolean
  onAdd: (project: GalleryMenuProject) => void | Promise<void>
  relatedProjects: GalleryMenuProject[]
  relatedStatus?: string
  onOpenRelated: (project: GalleryMenuProject) => void | Promise<void>
}

/**
 * 构造灵感库图片长按菜单。
 *
 * 行顺序固定为：管理、复制图片、添加到策划、已关联策划。加载或出错时用同一位置的
 * 禁用状态行反馈，避免关联项目迟到后改变前两行的位置。
 */
export function createGalleryImageMenu(options: GalleryImageMenuOptions): MenuEntry[] {
  const addChildren: MenuEntry[] = options.addStatus
    ? [{ label: options.addStatus, disabled: true }]
    : options.addProjects.length
      ? options.addProjects.map(project => ({
        label: project.title,
        run: () => options.onAdd(project),
      }))
      : [{ label: options.addLabel, disabled: true }]
  const entries: MenuEntry[] = [
    { label: options.manageLabel, run: options.manage },
    { label: options.copyLabel, disabled: !options.canCopy, run: options.copy },
    { label: options.addLabel, children: addChildren, disabled: options.addDisabled },
  ]
  if (options.relatedStatus) entries.push({ label: options.relatedStatus, disabled: true })
  else entries.push(...options.relatedProjects.map(project => ({
    label: project.title,
    run: () => options.onOpenRelated(project),
  })))
  return entries
}
