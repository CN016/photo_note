import type { TagRecord } from '../api/types'

export const MAX_INSPIRATION_TAGS = 8

/**
 * 按业务 UUID 去重并稳定排序标签。
 *
 * 系统预设优先于用户自定义标签，同类型内保持输入顺序，使新增标签自然排在分组末尾。
 */
export function orderTagIDs(ids: string[], tags: Pick<TagRecord, 'business_id' | 'tag_type'>[]): string[] {
  const byID = new Map(tags.map(tag => [tag.business_id, tag]))
  return Array.from(new Set(ids)).sort((left, right) => {
    const leftRank = byID.get(left)?.tag_type === 'system' ? 0 : 1
    const rightRank = byID.get(right)?.tag_type === 'system' ? 0 : 1
    return leftRank - rightRank
  })
}

/** 合并已有与新增标签，返回排序后的标签及是否超过单图 8 个上限。 */
export function mergeTagIDs(existing: string[], additions: string[], tags: Pick<TagRecord, 'business_id' | 'tag_type'>[]): { ids: string[]; overflow: boolean } {
  const ordered = orderTagIDs([...existing, ...additions], tags)
  return { ids: ordered.slice(0, MAX_INSPIRATION_TAGS), overflow: ordered.length > MAX_INSPIRATION_TAGS }
}
