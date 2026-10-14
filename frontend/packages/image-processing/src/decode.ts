import { decodeHeic, isHeic } from '@su-engineering/heic'

export interface DecodedPreview {
  canvas: HTMLCanvasElement
  width: number
  height: number
  format: 'heic' | 'image'
  wasDownsampled: boolean
}

const PREVIEW_LIMIT = 1800

/** 按最长边限制预览尺寸并保持宽高比，防止编辑时长期持有原尺寸像素。 */
export function fitPreviewDimensions(width: number, height: number, maxDimension = PREVIEW_LIMIT): { width: number; height: number } {
  const safeWidth = Math.max(1, Math.floor(Number.isFinite(width) ? width : 1))
  const safeHeight = Math.max(1, Math.floor(Number.isFinite(height) ? height : 1))
  const scale = Math.min(1, Math.max(1, maxDimension) / Math.max(safeWidth, safeHeight))
  return { width: Math.max(1, Math.round(safeWidth * scale)), height: Math.max(1, Math.round(safeHeight * scale)) }
}

/** HEIC 在浏览器或 WebCodecs 不可用时才动态加载 LGPL WASM 解码器。 */
async function loadWasmDecoder() {
  const module = await import('@su-engineering/heic/wasm')
  return module.wasmDecoder
}

/** 将输入图片解码为有边长限制的预览画布，原始 File/Blob 字节不作改写。 */
export async function decodeImagePreview(file: Blob, maxDimension = PREVIEW_LIMIT): Promise<DecodedPreview> {
  let heic = false
  try { heic = (await isHeic(file)).isHeic } catch { /* 内容探测失败时再由普通浏览器解码器给出错误。 */ }
  if (heic) {
    const decoded = await decodeHeic(file, { maxDimension, wasmLoader: loadWasmDecoder })
    try {
      const canvas = document.createElement('canvas')
      canvas.width = decoded.width
      canvas.height = decoded.height
      const context = canvas.getContext('2d', { alpha: true })
      if (!context) throw new Error('canvas_unavailable')
      context.drawImage(decoded.image, 0, 0, decoded.width, decoded.height)
      return { canvas, width: canvas.width, height: canvas.height, format: 'heic', wasDownsampled: decoded.width < decoded.sourceWidth || decoded.height < decoded.sourceHeight }
    } finally { decoded.image.close() }
  }

  let bitmap: ImageBitmap
  try {
    if (typeof createImageBitmap !== 'function') throw new Error('create_image_bitmap_unavailable')
    bitmap = await createImageBitmap(file)
  } catch {
    return decodeThroughImageElement(file, maxDimension)
  }
  try {
    const dimensions = fitPreviewDimensions(bitmap.width, bitmap.height, maxDimension)
    const { width, height } = dimensions
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const context = canvas.getContext('2d', { alpha: true })
    if (!context) throw new Error('canvas_unavailable')
    context.drawImage(bitmap, 0, 0, width, height)
    return { canvas, width, height, format: 'image', wasDownsampled: width < bitmap.width || height < bitmap.height }
  } finally { bitmap.close() }
}

/** 为缺少 createImageBitmap 的浏览器回退到原生 HTMLImageElement 解码器。 */
async function decodeThroughImageElement(file: Blob, maxDimension: number): Promise<DecodedPreview> {
  const url = URL.createObjectURL(file)
  try {
    const image = new Image()
    await new Promise<void>((resolve, reject) => {
      image.onload = () => resolve()
      image.onerror = () => reject(new Error('image_decode_failed'))
      image.src = url
    })
    const sourceWidth = image.naturalWidth
    const sourceHeight = image.naturalHeight
    const dimensions = fitPreviewDimensions(sourceWidth, sourceHeight, maxDimension)
    const canvas = document.createElement('canvas')
    canvas.width = dimensions.width
    canvas.height = dimensions.height
    const context = canvas.getContext('2d', { alpha: true })
    if (!context) throw new Error('canvas_unavailable')
    context.drawImage(image, 0, 0, canvas.width, canvas.height)
    image.src = ''
    return { canvas, width: canvas.width, height: canvas.height, format: 'image', wasDownsampled: canvas.width < sourceWidth || canvas.height < sourceHeight }
  } finally { URL.revokeObjectURL(url) }
}

/** 下载后的原图 Blob 转为可懒加载的浏览器对象地址，调用方负责释放地址。 */
export async function previewObjectUrl(file: Blob, maxDimension = 720): Promise<string> {
  const preview = await decodeImagePreview(file, maxDimension)
  try {
    const image = await canvasToBlob(preview.canvas, 'image/jpeg', 0.86)
    return URL.createObjectURL(image)
  } finally { releaseCanvas(preview.canvas) }
}

/** 生成独立的轻量 JPEG 工作副本；用户原始字节不会被此编码结果覆盖。 */
export async function makeWorkingJpeg(source: HTMLCanvasElement): Promise<Blob> {
  const flattened = document.createElement('canvas')
  flattened.width = source.width
  flattened.height = source.height
  const context = flattened.getContext('2d')
  if (!context) throw new Error('canvas_unavailable')
  context.fillStyle = '#ffffff'
  context.fillRect(0, 0, flattened.width, flattened.height)
  context.drawImage(source, 0, 0)
  try { return await canvasToBlob(flattened, 'image/jpeg', 0.9) }
  finally { releaseCanvas(flattened) }
}

/** 把文档中的旋转、原图坐标裁切和 RGB 偏移渲染到新预览画布。 */
export function renderDocumentPreview(source: HTMLCanvasElement, doc: {
  rotation: 0 | 90 | 180 | 270
  crop: { x: number; y: number; width: number; height: number }
  rgb: { red: number; green: number; blue: number }
}): HTMLCanvasElement {
  const rotated = document.createElement('canvas')
  const swapsAxes = doc.rotation === 90 || doc.rotation === 270
  rotated.width = swapsAxes ? source.height : source.width
  rotated.height = swapsAxes ? source.width : source.height
  const rotationContext = rotated.getContext('2d', { alpha: true })
  if (!rotationContext) throw new Error('canvas_unavailable')
  if (doc.rotation === 90) { rotationContext.translate(rotated.width, 0); rotationContext.rotate(Math.PI / 2) }
  else if (doc.rotation === 180) { rotationContext.translate(rotated.width, rotated.height); rotationContext.rotate(Math.PI) }
  else if (doc.rotation === 270) { rotationContext.translate(0, rotated.height); rotationContext.rotate(-Math.PI / 2) }
  rotationContext.drawImage(source, 0, 0)

  const crop = cropOnRotatedCanvas(doc)
  const sx = Math.round(crop.x * rotated.width)
  const sy = Math.round(crop.y * rotated.height)
  const sw = Math.max(1, Math.min(rotated.width - sx, Math.round(crop.width * rotated.width)))
  const sh = Math.max(1, Math.min(rotated.height - sy, Math.round(crop.height * rotated.height)))
  const output = document.createElement('canvas')
  output.width = sw
  output.height = sh
  const outputContext = output.getContext('2d', { alpha: true, willReadFrequently: true })
  if (!outputContext) throw new Error('canvas_unavailable')
  outputContext.drawImage(rotated, sx, sy, sw, sh, 0, 0, sw, sh)
  rotated.width = 0
  rotated.height = 0

  if (doc.rgb.red || doc.rgb.green || doc.rgb.blue) {
    const pixels = outputContext.getImageData(0, 0, output.width, output.height)
    const { red, green, blue } = doc.rgb
    for (let index = 0; index < pixels.data.length; index += 4) {
      pixels.data[index] = Math.max(0, Math.min(255, pixels.data[index] + red * 2.55))
      pixels.data[index + 1] = Math.max(0, Math.min(255, pixels.data[index + 1] + green * 2.55))
      pixels.data[index + 2] = Math.max(0, Math.min(255, pixels.data[index + 2] + blue * 2.55))
    }
    outputContext.putImageData(pixels, 0, 0)
  }
  return output
}

/** 释放已不再显示的离屏预览像素，帮助移动设备尽快回收图像内存。 */
export function releaseCanvas(canvas: HTMLCanvasElement | null): void {
  if (canvas) { canvas.width = 0; canvas.height = 0 }
}

/** Canvas 编码结果用于列表缩略图，编辑数据和上传原图不从此结果回写。 */
function canvasToBlob(canvas: HTMLCanvasElement, type: string, quality: number): Promise<Blob> {
  return new Promise((resolve, reject) => canvas.toBlob(blob => blob ? resolve(blob) : reject(new Error('canvas_encode_failed')), type, quality))
}

/** 使用与编辑核心相同的归一化旋转几何，避免裁切与锚点的坐标语义分叉。 */
import { cropOnRotatedCanvas } from '../../editor-core/src/document'
