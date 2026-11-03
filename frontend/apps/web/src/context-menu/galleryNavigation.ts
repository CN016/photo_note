import type { InspirationLibraryFilters, InspirationRecord } from '../api/types'

/** 仅当前标签页内存保留来源；会话代次不同即丢弃，不持久化用户图片及筛选内容。 */
export interface GallerySnapshot {
  scope: string; key: string; position: number; x: number; y: number
  search: string; tags: string[]; match: 'any' | 'all'; sort: 'created_desc' | 'created_asc' | 'updated_desc'
  filters: InspirationLibraryFilters; items: InspirationRecord[]; page: number; total: number
  scroll: number; imageID: string; menuPage: number; menuScroll: number; toolsScroll: number
  tools: { year: number; month: string; view: string }; zoom: { scale: number; scroll: number }
}
let snapshot: GallerySnapshot | null = null
export function saveGallerySnapshot(value: GallerySnapshot) { snapshot = value }
export function gallerySnapshot(scope: string, key?: string): GallerySnapshot | null {
  if (snapshot?.scope !== scope) snapshot = null
  return snapshot && (!key || snapshot.key === key) ? snapshot : null
}
export function clearGallerySnapshot() { snapshot = null }
