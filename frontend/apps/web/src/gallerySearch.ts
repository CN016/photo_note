import type { InspirationLibraryFilters } from './api/types'

/** 灵感库搜索面板一次只启用一种条件，避免隐藏的旧条件继续影响结果。 */
export type GallerySearchKind = 'text' | 'color' | 'tags' | 'people'
export type GalleryPeopleMode = 'present' | 'absent' | 'exact' | 'range'
export type GalleryPeoplePreset = 'absent' | '1' | '2' | '3' | '4' | '5' | '6' | 'many'
export type GalleryTagSource = 'all' | 'manual' | 'ai'
export type GallerySort = 'created_desc' | 'created_asc' | 'updated_desc' | 'color_match'

export interface GallerySearchState {
  kind: GallerySearchKind
  query: string
  color: string
  tagIds: string[]
  tagMatch: 'any' | 'all'
  tagSource: GalleryTagSource
  peopleMode: GalleryPeopleMode
  peopleMin: number
  peopleMax: number
}

export interface InspirationRequestOptions extends InspirationLibraryFilters {
  q?: string
  tagIds?: string[]
  tagMatch?: 'any' | 'all'
  sort?: GallerySort
}

/** 返回新的默认值；调用方不应跨条件类型复用旧输入。 */
export function createGallerySearchState(kind: GallerySearchKind = 'text'): GallerySearchState {
  return {
    kind,
    query: '',
    color: '',
    tagIds: [],
    tagMatch: 'any',
    tagSource: 'all',
    peopleMode: 'exact',
    peopleMin: 1,
    peopleMax: 1,
  }
}

/** 将详情页标签胶囊传入的路由参数转换为图库标签交集筛选状态。 */
export function gallerySearchFromTagQuery(rawIDs: unknown, rawMatch?: unknown): GallerySearchState {
  const values = Array.isArray(rawIDs) ? rawIDs : [rawIDs]
  const tagIds = Array.from(new Set(values.map(value => String(value ?? '').trim()).filter(Boolean)))
  return {
    ...createGallerySearchState(tagIds.length ? 'tags' : 'text'),
    tagIds,
    tagMatch: rawMatch === 'any' ? 'any' : 'all',
  }
}

/** 切换搜索条件时清空其他条件，确保界面只显示且只提交当前一个搜索维度。 */
export function switchGallerySearchKind(current: GallerySearchState, kind: GallerySearchKind): GallerySearchState {
  if (current.kind === kind) return current
  return createGallerySearchState(kind)
}

/** 快捷人数把“多人”映射为 6 人及以上，其余按钮使用无人或精确人数条件。 */
export function applyGalleryPeoplePreset(current: GallerySearchState, preset: GalleryPeoplePreset): GallerySearchState {
  if (preset === 'absent') return { ...current, peopleMode: 'absent', peopleMin: 0, peopleMax: 0 }
  if (preset === 'many') return { ...current, peopleMode: 'range', peopleMin: 6, peopleMax: 100000 }
  const count = Number(preset)
  return { ...current, peopleMode: 'exact', peopleMin: count, peopleMax: count }
}

/** 识别当前状态对应的快捷人数按钮；高级条件不强行映射到快捷按钮。 */
export function galleryPeoplePreset(state: GallerySearchState): GalleryPeoplePreset | '' {
  if (state.peopleMode === 'absent') return 'absent'
  if (state.peopleMode === 'exact' && state.peopleMin >= 1 && state.peopleMin <= 6) return String(state.peopleMin) as GalleryPeoplePreset
  if (state.peopleMode === 'range' && Math.min(state.peopleMin, state.peopleMax) === 6 && Math.max(state.peopleMin, state.peopleMax) === 100000) return 'many'
  return ''
}

/** 判断当前条件是否已有可提交值；色环未选色、标签未选或文本为空都视为未设置。 */
export function gallerySearchHasValue(state: GallerySearchState): boolean {
  if (state.kind === 'text') return Boolean(state.query.trim())
  if (state.kind === 'color') return /^#[0-9a-f]{6}$/i.test(state.color)
  if (state.kind === 'tags') return state.tagIds.length > 0
  return Boolean(state.peopleMode)
}

/** 把单条件面板转换为后请求参数；日期、项目等独立浏览条件继续从 filters 合并。 */
export function inspirationRequestOptions(
  state: GallerySearchState,
  sort: Exclude<GallerySort, 'color_match'>,
  filters: InspirationLibraryFilters = {},
): InspirationRequestOptions {
  const options: InspirationRequestOptions = { ...filters, sort }
  if (state.kind === 'text') {
    if (state.query.trim()) options.q = state.query.trim()
    return options
  }
  if (state.kind === 'color') {
    if (/^#[0-9a-f]{6}$/i.test(state.color)) {
      options.color = state.color
      options.sort = 'color_match'
    }
    return options
  }
  if (state.kind === 'tags') {
    if (state.tagIds.length) {
      options.tagIds = [...state.tagIds]
      options.tagMatch = state.tagMatch
      if (state.tagSource !== 'all') options.tag_source = state.tagSource
    }
    return options
  }
  options.people_mode = state.peopleMode
  if (state.peopleMode === 'exact') options.people_min = state.peopleMin
  if (state.peopleMode === 'range') {
    options.people_min = Math.min(state.peopleMin, state.peopleMax)
    options.people_max = Math.max(state.peopleMin, state.peopleMax)
  }
  return options
}
