/**
 * 将灵感图加入现有策划正文的纯数据结构操作。
 *
 * 策划的灵感图关联由正文节点在保存事务中同步；这里只构造完整快照，
 * 不直接修改关联表，也不携带文件地址或其他用户资源。
 */

/** 判断项目文档任意层级是否已引用指定灵感图，避免重复插入同一节点。 */
function containsInspiration(value: unknown, inspirationID: string): boolean {
  if (!value || typeof value !== 'object') return false
  if (Array.isArray(value)) return value.some(item => containsInspiration(item, inspirationID))
  const node = value as Record<string, unknown>
  if (node.type === 'inspirationImage' && node.attrs && typeof node.attrs === 'object') {
    const src = (node.attrs as Record<string, unknown>).src
    if (src === `inspiration://${inspirationID}`) return true
  }
  return containsInspiration(node.content, inspirationID)
}

/**
 * 在策划正文末尾追加灵感图节点。
 *
 * 若正文原本以可继续输入的段落结尾，图片插到该段落之前并保留段尾输入位置；
 * 否则在图片后补一个空段落。返回值中的 `added=false` 表示正文已引用该图片。
 */
export function appendInspirationToProject(
  document: Record<string, unknown>,
  inspirationID: string,
): { document: Record<string, unknown>; added: boolean } {
  const cloned = JSON.parse(JSON.stringify(document)) as Record<string, unknown>
  if (containsInspiration(cloned, inspirationID)) return { document: cloned, added: false }
  const content = Array.isArray(cloned.content) ? cloned.content as unknown[] : []
  const node = {
    type: 'inspirationImage',
    attrs: { src: `inspiration://${inspirationID}`, width: 100, layout: 'original' },
  }
  const last = content.at(-1) as Record<string, unknown> | undefined
  const trailingParagraph = last?.type === 'paragraph'
    && (!Array.isArray(last.content) || last.content.length === 0)
  if (trailingParagraph) content.splice(content.length - 1, 0, node)
  else content.push(node, { type: 'paragraph' })
  cloned.type = typeof cloned.type === 'string' ? cloned.type : 'doc'
  cloned.content = content
  return { document: cloned, added: true }
}
