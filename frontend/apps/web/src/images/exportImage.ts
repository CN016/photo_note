/**
 * 图片导出源与文件命名的纯逻辑。
 *
 * 无可见编辑时可以直接保存原始字节，完整保留分辨率、编码质量和元数据；
 * 只有旋转、裁切、色彩调整、锚点或水印存在时，才需要重新渲染像素。
 */

import type { EditorDocument } from '../../../../packages/editor-core/src'

/** 判断导出是否需要重新渲染；普通导出且文档无视觉变化时可复用原图字节。 */
export function hasExportVisualChanges(document: EditorDocument, withWatermark: boolean): boolean {
  const crop = document.crop
  return withWatermark
    || document.rotation !== 0
    || Math.abs(crop.x) > 0.000001
    || Math.abs(crop.y) > 0.000001
    || Math.abs(crop.width - 1) > 0.000001
    || Math.abs(crop.height - 1) > 0.000001
    || document.rgb.red !== 0
    || document.rgb.green !== 0
    || document.rgb.blue !== 0
    || document.anchors.length > 0
}

/** 原图 key 只在当前文档确有独立原图时使用，避免把工作副本误当原图。 */
export function exportSourceKey(document: EditorDocument, fallback: string): string {
  const source = typeof document.source_image_key === 'string' ? document.source_image_key.trim() : ''
  return source || fallback
}

/** 根据 MIME 或文件名确定直出原图的扩展名；未知类型回退到 jpg。 */
export function originalExportFormat(blob: Blob, filename: string): { mime: string; extension: string } {
  const mime = blob.type.split(';')[0]?.trim().toLowerCase() || ''
  const known: Record<string, string> = {
    'image/jpeg': 'jpg',
    'image/png': 'png',
    'image/webp': 'webp',
    'image/gif': 'gif',
    'image/avif': 'avif',
    'image/bmp': 'bmp',
    'image/heic': 'heic',
    'image/heif': 'heif',
  }
  const extension = filename.match(/\.([a-z0-9]+)$/i)?.[1]?.toLowerCase()
  return {
    mime: mime.startsWith('image/') ? mime : extension ? `image/${extension === 'jpg' ? 'jpeg' : extension}` : 'image/jpeg',
    extension: known[mime] || extension || 'jpg',
  }
}
