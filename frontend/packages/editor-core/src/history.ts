import type { EditorDocument } from './document'

/** 文档历史仅复制轻量 JSON，不触碰图片像素或图片二进制资源。 */
export class DocumentHistory {
  private past: EditorDocument[] = []
  private future: EditorDocument[] = []
  private current: EditorDocument

  constructor(initial: EditorDocument, private readonly limit = 100) { this.current = clone(initial) }
  get value(): EditorDocument { return clone(this.current) }
  get canUndo(): boolean { return this.past.length > 0 }
  get canRedo(): boolean { return this.future.length > 0 }

  /** 执行一次完整文档变更，连续滑杆操作可由调用方合并后再提交。 */
  commit(next: EditorDocument): EditorDocument {
    this.past.push(clone(this.current))
    if (this.past.length > this.limit) this.past.shift()
    this.current = clone(next)
    this.future = []
    return this.value
  }

  /** 回退到前一份编辑参数快照，并保留重做入口。 */
  undo(): EditorDocument {
    const previous = this.past.pop()
    if (previous) { this.future.push(clone(this.current)); this.current = previous }
    return this.value
  }

  /** 重做最近撤销的编辑参数变化。 */
  redo(): EditorDocument {
    const next = this.future.pop()
    if (next) { this.past.push(clone(this.current)); this.current = next }
    return this.value
  }
}

/** 复制的是文档参数对象，图片资源始终由外层引用管理。 */
function clone(document: EditorDocument): EditorDocument {
  return { ...document, crop: { ...document.crop }, rgb: { ...document.rgb }, anchors: document.anchors.map(anchor => ({ ...anchor })) }
}
