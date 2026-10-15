import { decodeImagePreview, makeWorkingJpeg, releaseCanvas } from '../../../packages/image-processing/src'

export interface PreparedImageUpload {
  blob: Blob
  filename: string
  previewUrl: string
}

/** 校验图片可解码，并在 HEIC/HEIF 环境中生成 JPEG 副本，原始文件不被改写。 */
export async function prepareImageUpload(file: File, maxDimension = 1600): Promise<PreparedImageUpload> {
  const decoded = await decodeImagePreview(file, maxDimension)
  try {
    if (decoded.format === 'heic') {
      const blob = await makeWorkingJpeg(decoded.canvas)
      const stem = file.name.replace(/\.[^.]+$/, '') || 'image'
      return { blob, filename: `${stem}.jpg`, previewUrl: URL.createObjectURL(blob) }
    }
    return { blob: file, filename: file.name || 'image', previewUrl: URL.createObjectURL(file) }
  } finally {
    releaseCanvas(decoded.canvas)
  }
}
