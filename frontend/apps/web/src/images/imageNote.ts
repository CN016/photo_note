/** 整图备注保存在兼容扩展字段data.image_note；旧文档没有此字段时不展示额外行。 */
export function imageNote(data: Record<string, unknown> | undefined): string {
  return typeof data?.image_note === 'string' ? data.image_note.trim() : ''
}
