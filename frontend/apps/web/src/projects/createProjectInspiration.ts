import { createEditorDocument } from '../../../../packages/editor-core/src'
import { analyzeImageColors, decodeImagePreview, makeWorkingJpeg, readInspirationImageMetadata, releaseCanvas } from '../../../../packages/image-processing/src'
import { api } from '../api/client'
import type { InspirationRecord } from '../api/types'

/** 项目新建灵感图沿用图库的原图、JPEG 工作副本、元数据和异步压缩流程，只保存业务 ID 引用。 */
export async function createProjectInspiration(file: File): Promise<InspirationRecord> {
  const metadata = await readInspirationImageMetadata(file)
  const original = await api.upload(file, file.name)
  const decoded = await decodeImagePreview(file)
  let working: Blob
  let colorAnalysis: unknown = null
  try {
    try { colorAnalysis = analyzeImageColors(decoded.canvas) } catch { colorAnalysis = null }
    working = await makeWorkingJpeg(decoded.canvas)
  } finally { releaseCanvas(decoded.canvas) }

  const baseName = file.name.replace(/\.[^.]+$/, '') || 'inspiration'
  const preview = await api.upload(working, `${baseName}.jpg`)
  const document: Record<string, unknown> = { ...createEditorDocument(), source_image_key: original.storekey }
  if (colorAnalysis) document.color_analysis = colorAnalysis
  return api.createInspiration(preview.storekey, file.name, document, metadata)
}
