import { fitPreviewDimensions, releaseCanvas } from './decode'

/** 颜色画像采用独立版本，便于以后调整采样算法并识别需要补算的旧记录。 */
export const COLOR_ANALYSIS_VERSION = 1 as const

/** 单个代表色及其在可见图像中的占比，percentage 的单位为百分比。 */
export interface PaletteColor {
  hex: string
  percentage: number
}

/** 灵感图详情使用的轻量颜色画像，不包含原始像素或图片内容。 */
export interface ColorAnalysis {
  version: typeof COLOR_ANALYSIS_VERSION
  palette: PaletteColor[]
  other_percentage: number
  average_brightness: number
  average_saturation: number
}

const MAX_SAMPLE_EDGE = 64
const HISTOGRAM_CHANNEL_BITS = 4
const PERCENTAGE_UNITS = 10_000

/**
 * 从有界尺寸画布读取颜色画像；透明像素按应用工作副本的白色底图合成，临时像素缓冲区随后释放。
 */
export function analyzeImageColors(source: HTMLCanvasElement): ColorAnalysis {
  if (!source.width || !source.height) throw new Error('image_canvas_empty')

  const dimensions = fitPreviewDimensions(source.width, source.height, MAX_SAMPLE_EDGE)
  const sample = document.createElement('canvas')
  sample.width = dimensions.width
  sample.height = dimensions.height

  try {
    const context = sample.getContext('2d', { willReadFrequently: true })
    if (!context) throw new Error('canvas_unavailable')
    context.imageSmoothingEnabled = true
    context.drawImage(source, 0, 0, sample.width, sample.height)
    return analyzeColorPixels(context.getImageData(0, 0, sample.width, sample.height).data)
  } finally {
    releaseCanvas(sample)
  }
}

/**
 * 汇总有限像素样本：以 4 位 RGB 直方图合并相近颜色，使用最大余数法让前五色与其他占比精确合计为 100%。
 */
export function analyzeColorPixels(pixels: ArrayLike<number>): ColorAnalysis {
  if (!pixels.length || pixels.length % 4 !== 0) throw new Error('image_pixels_invalid')

  const buckets = new Map<number, { count: number; red: number; green: number; blue: number }>()
  let brightnessTotal = 0
  let saturationTotal = 0
  const pixelCount = pixels.length / 4

  for (let index = 0; index < pixels.length; index += 4) {
    const alpha = Math.max(0, Math.min(255, Number(pixels[index + 3]))) / 255
    const red = Math.round(Number(pixels[index]) * alpha + 255 * (1 - alpha))
    const green = Math.round(Number(pixels[index + 1]) * alpha + 255 * (1 - alpha))
    const blue = Math.round(Number(pixels[index + 2]) * alpha + 255 * (1 - alpha))
    const maximum = Math.max(red, green, blue)
    const minimum = Math.min(red, green, blue)

    brightnessTotal += 0.2126 * red + 0.7152 * green + 0.0722 * blue
    saturationTotal += maximum === 0 ? 0 : (maximum - minimum) / maximum * 100

    const key = (red >> (8 - HISTOGRAM_CHANNEL_BITS)) << (HISTOGRAM_CHANNEL_BITS * 2)
      | (green >> (8 - HISTOGRAM_CHANNEL_BITS)) << HISTOGRAM_CHANNEL_BITS
      | blue >> (8 - HISTOGRAM_CHANNEL_BITS)
    const bucket = buckets.get(key) ?? { count: 0, red: 0, green: 0, blue: 0 }
    bucket.count += 1
    bucket.red += red
    bucket.green += green
    bucket.blue += blue
    buckets.set(key, bucket)
  }

  const ranked = [...buckets.entries()].sort((left, right) => right[1].count - left[1].count || left[0] - right[0])
  const dominant = ranked.slice(0, 5).map(([, bucket]) => bucket)
  const otherCount = pixelCount - dominant.reduce((total, bucket) => total + bucket.count, 0)
  const shares = allocatePercentages([...dominant.map(bucket => bucket.count), otherCount])

  return {
    version: COLOR_ANALYSIS_VERSION,
    palette: dominant.map((bucket, index) => ({
      hex: averageHex(bucket),
      percentage: shares[index],
    })),
    other_percentage: shares[dominant.length],
    average_brightness: roundOneDecimal(brightnessTotal / pixelCount / 255 * 100),
    average_saturation: roundOneDecimal(saturationTotal / pixelCount),
  }
}

/** 校验从服务端 JSONB 读取的画像版本和字段，损坏或旧版本记录会走自动补算。 */
export function isColorAnalysis(value: unknown): value is ColorAnalysis {
  if (!value || typeof value !== 'object') return false
  const profile = value as Partial<ColorAnalysis>
  if (profile.version !== COLOR_ANALYSIS_VERSION || !Array.isArray(profile.palette) || profile.palette.length < 1 || profile.palette.length > 5) return false
  if (!Number.isFinite(profile.other_percentage) || !Number.isFinite(profile.average_brightness) || !Number.isFinite(profile.average_saturation)) return false
  const validPalette = profile.palette.every(color => color
    && typeof color.hex === 'string'
    && /^#[0-9a-f]{6}$/i.test(color.hex)
    && Number.isFinite(color.percentage)
    && color.percentage >= 0
    && color.percentage <= 100)
  if (!validPalette || profile.other_percentage! < 0 || profile.other_percentage! > 100) return false
  if (profile.average_brightness! < 0 || profile.average_brightness! > 100 || profile.average_saturation! < 0 || profile.average_saturation! > 100) return false
  const total = profile.palette.reduce((sum, color) => sum + color.percentage, profile.other_percentage!)
  return Math.abs(total - 100) < 0.011
}

/** 将颜色桶内像素均值写成大写 RGB 十六进制色值。 */
function averageHex(bucket: { count: number; red: number; green: number; blue: number }): string {
  return `#${[bucket.red, bucket.green, bucket.blue]
    .map(channel => Math.round(channel / bucket.count).toString(16).padStart(2, '0'))
    .join('')
    .toUpperCase()}`
}

/** 把原始像素占比按百分之一精度分配，避免各项独立舍入后总和偏离 100%。 */
function allocatePercentages(counts: number[]): number[] {
  const total = counts.reduce((sum, count) => sum + count, 0)
  const exactUnits = counts.map(count => count / total * PERCENTAGE_UNITS)
  const units = exactUnits.map(Math.floor)
  const order = exactUnits.map((value, index) => ({ index, remainder: value - units[index] }))
    .sort((left, right) => right.remainder - left.remainder || left.index - right.index)
  const remaining = PERCENTAGE_UNITS - units.reduce((sum, value) => sum + value, 0)
  for (let index = 0; index < remaining; index += 1) units[order[index % order.length].index] += 1
  return units.map(value => value / 100)
}

/** 把统计指标限制为一位小数，详情页无需处理浮点噪声。 */
function roundOneDecimal(value: number): number {
  return Math.round(value * 10) / 10
}
