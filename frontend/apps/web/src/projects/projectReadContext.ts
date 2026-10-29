import type { ComputedRef, InjectionKey } from 'vue'

/** 节点视图只读取当前模式，并按正文位置打开图片；不授予编辑权限。 */
export const projectReadContext: InjectionKey<{ readOnly: ComputedRef<boolean>; openImage: (position: number) => void }> = Symbol('project-read-context')
