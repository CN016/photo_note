/** 编辑文档只保存轻量参数和备注，不包含图片像素，原图二进制独立存储。 */
export interface NormalizedRect { x: number; y: number; width: number; height: number }
export interface RGBOffsets { red: number; green: number; blue: number }
export interface ImageAnchor { id: string; x: number; y: number; note: string }
export interface EditorDocument {
  schema_version: 1
  rotation: 0 | 90 | 180 | 270
  /** 裁切框采用 EXIF 方向校正后的原图归一化坐标。 */
  crop: NormalizedRect
  rgb: RGBOffsets
  anchors: ImageAnchor[]
  /** 前端扩展字段：指向未经后端压缩的原始文件。 */
  source_image_key?: string
  /** 保留尚未被本版本识别的文档扩展字段。 */
  [key: string]: unknown
}

/** 新建默认文档，使用全图裁切和零颜色偏移。 */
export function createEditorDocument(): EditorDocument {
  return { schema_version: 1, rotation: 0, crop: { x: 0, y: 0, width: 1, height: 1 }, rgb: { red: 0, green: 0, blue: 0 }, anchors: [] }
}

/** 从服务端 JSON 安全读取一期文档；未知字段原样保留以兼容后续版本扩展。 */
export function parseEditorDocument(value: Record<string, unknown>): EditorDocument {
  const base = createEditorDocument()
  const rotationValue = Number(value.rotation)
  const rotation = ([0, 90, 180, 270] as number[]).includes(rotationValue) ? rotationValue as EditorDocument['rotation'] : 0
  const rawCrop = value.crop && typeof value.crop === 'object' ? value.crop as Record<string, unknown> : {}
  const crop = clampRect({ x: Number(rawCrop.x), y: Number(rawCrop.y), width: Number(rawCrop.width), height: Number(rawCrop.height) })
  const rawRgb = value.rgb && typeof value.rgb === 'object' ? value.rgb as Record<string, unknown> : {}
  const rgb = {
    red: clampOffset(Number(rawRgb.red)), green: clampOffset(Number(rawRgb.green)), blue: clampOffset(Number(rawRgb.blue)),
  }
  const anchors = Array.isArray(value.anchors) ? value.anchors.flatMap((anchor, index) => {
    if (!anchor || typeof anchor !== 'object') return []
    const item = anchor as Record<string, unknown>
    if (typeof item.note !== 'string' || !item.note.trim()) return []
    return [{ id: typeof item.id === 'string' ? item.id : `anchor-${index}`, x: clampUnit(Number(item.x)), y: clampUnit(Number(item.y)), note: item.note }]
  }) : []
  return { ...base, ...value, schema_version: 1, rotation, crop, rgb, anchors }
}

/** 将不可靠的裁切参数收敛到合法且非零的原图范围。 */
export function clampRect(rect: NormalizedRect): NormalizedRect {
  const x = Math.min(0.99, clampUnit(rect.x))
  const y = Math.min(0.99, clampUnit(rect.y))
  const width = Math.max(0.01, Math.min(Number.isFinite(rect.width) ? rect.width : 1, 1 - x))
  const height = Math.max(0.01, Math.min(Number.isFinite(rect.height) ? rect.height : 1, 1 - y))
  return { x, y, width, height }
}

/** RGB 偏移和锚点坐标统一限制在数据契约规定的数值范围。 */
export function clampOffset(value: number): number { return Math.max(-100, Math.min(100, Number.isFinite(value) ? Math.round(value) : 0)) }
export function clampUnit(value: number): number { return Math.max(0, Math.min(1, Number.isFinite(value) ? value : 0)) }

/** 将原图归一化坐标映射到当前旋转后的完整画布。 */
export function sourceToRotated(point: { x: number; y: number }, rotation: EditorDocument['rotation']): { x: number; y: number } {
  if (rotation === 90) return { x: 1 - point.y, y: point.x }
  if (rotation === 180) return { x: 1 - point.x, y: 1 - point.y }
  if (rotation === 270) return { x: point.y, y: 1 - point.x }
  return { ...point }
}

/** 将当前旋转画布坐标反算到方向校正后的原图归一化坐标。 */
export function rotatedToSource(point: { x: number; y: number }, rotation: EditorDocument['rotation']): { x: number; y: number } {
  if (rotation === 90) return { x: point.y, y: 1 - point.x }
  if (rotation === 180) return { x: 1 - point.x, y: 1 - point.y }
  if (rotation === 270) return { x: 1 - point.y, y: point.x }
  return { ...point }
}

/** 将原图裁切框映射到当前旋转画布，旋转后仍保持正交矩形。 */
export function cropOnRotatedCanvas(doc: Pick<EditorDocument, 'crop' | 'rotation'>): NormalizedRect {
  const { x, y, width, height } = doc.crop
  const corners = [
    { x, y }, { x: x + width, y }, { x, y: y + height }, { x: x + width, y: y + height },
  ].map(point => sourceToRotated(point, doc.rotation))
  const minX = Math.min(...corners.map(point => point.x))
  const maxX = Math.max(...corners.map(point => point.x))
  const minY = Math.min(...corners.map(point => point.y))
  const maxY = Math.max(...corners.map(point => point.y))
  return { x: minX, y: minY, width: maxX - minX, height: maxY - minY }
}

/** 把原图锚点转换为裁切后可直接用于画布叠层的归一化位置。 */
export function anchorToView(anchor: Pick<ImageAnchor, 'x' | 'y'>, doc: EditorDocument): { x: number; y: number; visible: boolean } {
  const point = sourceToRotated(anchor, doc.rotation)
  const crop = cropOnRotatedCanvas(doc)
  const x = (point.x - crop.x) / crop.width
  const y = (point.y - crop.y) / crop.height
  return { x, y, visible: x >= 0 && x <= 1 && y >= 0 && y <= 1 }
}

/** 将用户在裁切后画布上的点击位置反算至原图锚点坐标。 */
export function viewToSource(point: { x: number; y: number }, doc: EditorDocument): { x: number; y: number } {
  const crop = cropOnRotatedCanvas(doc)
  return rotatedToSource({ x: crop.x + point.x * crop.width, y: crop.y + point.y * crop.height }, doc.rotation)
}

/** 把当前画布上拖选的矩形映射回原图空间，供持久化裁切框使用。 */
export function viewSelectionToSourceRect(start: { x: number; y: number }, end: { x: number; y: number }, doc: EditorDocument): NormalizedRect {
  const points = [start, end, { x: start.x, y: end.y }, { x: end.x, y: start.y }].map(point => viewToSource(point, doc))
  const minX = Math.min(...points.map(point => point.x))
  const maxX = Math.max(...points.map(point => point.x))
  const minY = Math.min(...points.map(point => point.y))
  const maxY = Math.max(...points.map(point => point.y))
  return clampRect({ x: minX, y: minY, width: maxX - minX, height: maxY - minY })
}
