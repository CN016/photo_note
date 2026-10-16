/** 灵感图 API 可读写的常见图片属性和带命名空间的扩展标签。 */
export interface InspirationImageMetadata {
  image_id?: string
  source_file_size?: number
  source_last_modified_at?: string
  image_width?: number
  image_height?: number
  horizontal_resolution_dpi?: number
  vertical_resolution_dpi?: number
  bit_depth?: number
  color_space?: string
  compression?: string
  title?: string
  subject?: string
  rating?: number
  tags?: string[]
  author?: string
  comments?: string
  copyright?: string
  software?: string
  date_acquired?: string
  date_time_original?: string
  date_time_digitized?: string
  camera_make?: string
  camera_model?: string
  camera_serial_number?: string
  lens_make?: string
  lens_model?: string
  lens_serial_number?: string
  flash_make?: string
  flash_model?: string
  flash_serial_number?: string
  aperture_f_number?: number
  maximum_aperture_f_number?: number
  exposure_time_seconds?: number
  iso_speed?: number
  exposure_compensation_ev?: number
  exposure_program?: string
  flash_fired?: boolean
  flash_mode?: string
  flash_energy_bcps?: number
  focal_length_mm?: number
  focal_length_35mm_mm?: number
  subject_distance_m?: number
  metering_mode?: string
  light_source?: string
  white_balance?: string
  contrast?: string
  brightness_ev?: number
  saturation?: string
  sharpness?: string
  digital_zoom_ratio?: number
  compression_bits_per_pixel?: number
  exif_version?: string
  gps_latitude?: number
  gps_longitude?: number
  gps_altitude_m?: number
  gps_timestamp?: string
  extra: Record<string, unknown>
}

const maxExtraBytes = 1024 * 1024

/** 先记录 File API 明确可见的原图属性，再尽量读取 EXIF、IPTC、XMP 等标签。 */
export async function readInspirationImageMetadata(file: File): Promise<InspirationImageMetadata> {
  const metadata: InspirationImageMetadata = {
    source_file_size: file.size,
    extra: {},
  }
  if (Number.isFinite(file.lastModified)) {
    const modifiedAt = new Date(file.lastModified)
    if (Number.isFinite(modifiedAt.getTime())) metadata.source_last_modified_at = modifiedAt.toISOString()
  }

  try {
    const { default: exifr } = await import('exifr')
    const tags = await exifr.parse(file, {
      tiff: true,
      xmp: true,
      icc: true,
      iptc: true,
      jfif: true,
      ihdr: true,
      reviveValues: false,
      mergeOutput: false,
    }) as Record<string, unknown> | null
    if (tags) {
      const parsed = splitParserOutput(tags)
      Object.assign(metadata, mapKnownTags(parsed.known))
      metadata.extra = limitMetadataExtra(parsed.extra)
    }
  } catch {
    // 图片格式或标签解析失败时仍保存 File API 可读的大小和修改时间。
  }
  return metadata
}

/** 保留各解析分段作为扩展属性命名空间，同时展平别名供固定字段映射使用。 */
function splitParserOutput(output: Record<string, unknown>): { known: Record<string, unknown>; extra: Record<string, unknown> } {
  const known: Record<string, unknown> = {}
  const extra: Record<string, unknown> = {}
  for (const [group, value] of Object.entries(output)) {
    // 文件系统分组可能包含本机路径和文件时间，不作为图片自身属性保存。
    if (['file', 'system'].includes(group.toLowerCase())) continue
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      extra[`exifr:${group}`] = value
      continue
    }
    for (const [name, tagValue] of Object.entries(value as Record<string, unknown>)) {
      if (!(name in known)) known[name] = tagValue
      known[`${group}:${name}`] = tagValue
      extra[`exifr:${group}:${name}`] = tagValue
    }
  }
  return { known, extra }
}

/** 将常见标签压缩成稳定字段；没有识别的值以 exifr 命名空间保留。 */
function mapKnownTags(tags: Record<string, unknown>): Partial<InspirationImageMetadata> {
  const get = (...names: string[]) => names.map(name => tags[name]).find(value => value !== undefined && value !== null)
  const result: Partial<InspirationImageMetadata> = {}
  assignString(result, 'image_id', get('ImageUniqueID', 'ImageID'))
  assignNumber(result, 'image_width', get('ImageWidth', 'PixelXDimension', 'width'))
  assignNumber(result, 'image_height', get('ImageHeight', 'PixelYDimension', 'height'))
  const jfifResolutionUnit = get('jfif:ResolutionUnit')
  const exifResolutionUnit = get('ifd0:ResolutionUnit')
  const genericResolutionUnit = get('ResolutionUnit')
  const multiplier = exifResolutionUnit !== undefined
    ? exifResolutionUnit === 3 ? 2.54 : exifResolutionUnit === 2 ? 1 : 0
    : jfifResolutionUnit !== undefined
      ? jfifResolutionUnit === 2 ? 2.54 : jfifResolutionUnit === 1 ? 1 : 0
      : genericResolutionUnit === 3 || genericResolutionUnit === 'cm' ? 2.54
        : genericResolutionUnit === 2 || genericResolutionUnit === 'inches' ? 1 : 0
  if (multiplier) {
    assignNumber(result, 'horizontal_resolution_dpi', get('ifd0:XResolution', 'jfif:XResolution', 'XResolution'), multiplier)
    assignNumber(result, 'vertical_resolution_dpi', get('ifd0:YResolution', 'jfif:YResolution', 'YResolution'), multiplier)
  }
  const bits = get('BitsPerSample', 'BitDepth', 'BitDepthPerSample')
  if (Array.isArray(bits)) {
    const total = bits.reduce<number>((sum, item) => sum + numeric(item), 0)
    assignNumber(result, 'bit_depth', bits.length === 1 ? total * metadataSampleCount(tags) : total)
  } else assignNumber(result, 'bit_depth', numeric(bits) * metadataSampleCount(tags))
  const colorValue = get('ColorSpace', 'ProfileDescription', 'ColorModel')
  if (typeof colorValue === 'string' && colorValue.toLowerCase().includes('srgb')) assignString(result, 'color_space', 'sRGB')
  else if (colorValue !== undefined && colorValue !== null) assignString(result, 'color_space', colorValue)
  else {
    const colorType = numeric(get('ColorType'))
    const colorTypes: Record<number, string> = { 0: 'grayscale', 2: 'RGB', 3: 'indexed color', 4: 'grayscale with alpha', 6: 'RGB with alpha' }
    assignString(result, 'color_space', colorTypes[colorType])
  }
  const compression = get('Compression')
  const compressions: Record<number, string> = { 1: 'Uncompressed', 5: 'LZW', 6: 'JPEG', 7: 'JPEG', 8: 'Deflate', 32773: 'PackBits' }
  assignString(result, 'compression', typeof compression === 'number' ? (compressions[compression] ?? compression) : compression)
  assignString(result, 'title', get('Title', 'ObjectName', 'DocumentName'))
  assignString(result, 'subject', get('Subject', 'Description', 'Caption-Abstract'))
  assignNumber(result, 'rating', get('Rating'))
  const keywords = get('Keywords', 'HierarchicalSubject')
  if (Array.isArray(keywords)) result.tags = keywords.map(stringValue).filter(Boolean)
  else if (keywords !== undefined && keywords !== null) result.tags = [stringValue(keywords)].filter(Boolean)
  assignString(result, 'author', get('Artist', 'Creator', 'By-line', 'Author'))
  assignString(result, 'comments', get('UserComment', 'Comment'))
  assignString(result, 'copyright', get('Copyright', 'CopyrightNotice'))
  assignString(result, 'software', get('Software', 'ProgramName'))
  assignString(result, 'date_acquired', get('DateAcquired'))
  assignString(result, 'date_time_original', get('DateTimeOriginal', 'DateCreated'))
  assignString(result, 'date_time_digitized', get('CreateDate', 'DateTimeDigitized'))
  assignString(result, 'camera_make', get('Make', 'CameraMake'))
  assignString(result, 'camera_model', get('Model', 'CameraModel'))
  assignString(result, 'camera_serial_number', get('SerialNumber', 'BodySerialNumber', 'CameraSerialNumber'))
  assignString(result, 'lens_make', get('LensMake', 'LensManufacturer'))
  assignString(result, 'lens_model', get('LensModel', 'LensID'))
  assignString(result, 'lens_serial_number', get('LensSerialNumber'))
  assignString(result, 'flash_make', get('FlashMake', 'FlashManufacturer'))
  assignString(result, 'flash_model', get('FlashModel'))
  assignString(result, 'flash_serial_number', get('FlashSerialNumber'))
  const aperture = numeric(get('FNumber'))
  assignNumber(result, 'aperture_f_number', Number.isFinite(aperture) ? aperture : Math.pow(2, numeric(get('ApertureValue')) / 2))
  assignNumber(result, 'maximum_aperture_f_number', Math.pow(2, numeric(get('MaxApertureValue')) / 2))
  const exposure = numeric(get('ExposureTime'))
  assignNumber(result, 'exposure_time_seconds', Number.isFinite(exposure) ? exposure : Math.pow(2, -numeric(get('ShutterSpeedValue'))))
  assignNumber(result, 'iso_speed', get('ISO', 'ISOSpeedRatings', 'PhotographicSensitivity'))
  assignNumber(result, 'exposure_compensation_ev', get('ExposureCompensation', 'ExposureBiasValue'))
  assignString(result, 'exposure_program', get('ExposureProgram'))
  const flash = get('Flash')
  if (typeof flash === 'number') result.flash_fired = (flash & 1) === 1
  else if (flash !== undefined && flash !== null) {
    result.flash_mode = stringValue(flash)
    result.flash_fired = /fired/i.test(result.flash_mode) && !/did not fire|not fired/i.test(result.flash_mode)
  }
  assignString(result, 'flash_mode', get('FlashMode'))
  assignNumber(result, 'flash_energy_bcps', get('FlashEnergy'))
  assignNumber(result, 'focal_length_mm', get('FocalLength'))
  assignNumber(result, 'focal_length_35mm_mm', get('FocalLengthIn35mmFormat', 'FocalLength35efl'))
  assignNumber(result, 'subject_distance_m', get('SubjectDistance'))
  assignString(result, 'metering_mode', get('MeteringMode'))
  assignString(result, 'light_source', get('LightSource'))
  assignString(result, 'white_balance', get('WhiteBalance'))
  assignString(result, 'contrast', get('Contrast'))
  assignNumber(result, 'brightness_ev', get('BrightnessValue'))
  assignString(result, 'saturation', get('Saturation'))
  assignString(result, 'sharpness', get('Sharpness'))
  assignNumber(result, 'digital_zoom_ratio', get('DigitalZoomRatio'))
  assignNumber(result, 'compression_bits_per_pixel', get('CompressedBitsPerPixel'))
  assignString(result, 'exif_version', get('ExifVersion', 'EXIFVersion'))
  assignNumber(result, 'gps_latitude', get('latitude', 'GPSLatitude'))
  assignNumber(result, 'gps_longitude', get('longitude', 'GPSLongitude'))
  assignNumber(result, 'gps_altitude_m', get('GPSAltitude'))
  assignString(result, 'gps_timestamp', get('GPSDateTime', 'GPSDateStamp'))
  return result
}

/** 以稳定顺序纳入扩展标签，超过 1 MiB 的单项被跳过，已映射字段不受影响。 */
function limitMetadataExtra(tags: Record<string, unknown>): Record<string, unknown> {
  const extra: Record<string, unknown> = {}
  const knownNames = new Set([
    'ImageUniqueID', 'ImageID', 'ImageWidth', 'PixelXDimension', 'width', 'ImageHeight', 'PixelYDimension', 'height',
    'ResolutionUnit', 'XResolution', 'YResolution', 'BitsPerSample', 'BitDepth', 'BitDepthPerSample', 'SamplesPerPixel', 'ColorSpace', 'ColorModel', 'ColorType', 'ProfileDescription', 'Compression',
    'Title', 'ObjectName', 'DocumentName', 'Subject', 'Description', 'Caption-Abstract', 'Rating', 'Keywords', 'HierarchicalSubject',
    'Artist', 'Creator', 'By-line', 'Byline', 'Author', 'UserComment', 'Comment', 'Copyright', 'CopyrightNotice', 'Software', 'ProgramName',
    'DateAcquired', 'DateTimeOriginal', 'DateCreated', 'CreateDate', 'DateTimeDigitized', 'Make', 'CameraMake', 'Model', 'CameraModel', 'SerialNumber', 'BodySerialNumber', 'CameraSerialNumber', 'LensMake', 'LensManufacturer', 'LensModel', 'LensID', 'LensSerialNumber', 'FlashMake', 'FlashManufacturer', 'FlashModel', 'FlashSerialNumber',
    'FNumber', 'ApertureValue', 'MaxApertureValue', 'ExposureTime', 'ShutterSpeedValue', 'ISO', 'ISOSpeedRatings', 'PhotographicSensitivity', 'ExposureCompensation', 'ExposureBiasValue', 'ExposureProgram', 'Flash', 'FlashMode', 'FlashEnergy',
    'FocalLength', 'FocalLengthIn35mmFormat', 'FocalLength35efl', 'SubjectDistance', 'MeteringMode', 'LightSource', 'WhiteBalance', 'Contrast', 'BrightnessValue', 'Saturation', 'Sharpness', 'DigitalZoomRatio', 'CompressedBitsPerPixel', 'ExifVersion', 'EXIFVersion',
    'latitude', 'longitude', 'GPSLatitude', 'GPSLongitude', 'GPSAltitude', 'GPSDateTime', 'GPSDateStamp',
  ])
  for (const name of Object.keys(tags).sort()) {
    if (knownNames.has(name.split(':').at(-1) ?? name) || ['SourceFile', 'FileName', 'Directory', 'FileSize'].includes(name)) continue
    const key = name.includes(':') ? name : `exifr:${name}`
    extra[key] = tags[name]
    if (new TextEncoder().encode(JSON.stringify(extra)).byteLength > maxExtraBytes) delete extra[key]
  }
  return extra
}

/** 仅将非空属性转为字符串，避免把解析失败或空值写成可见文本。 */
function assignString(target: Partial<InspirationImageMetadata>, key: keyof InspirationImageMetadata, value: unknown): void {
  if (value === undefined || value === null) return
  const converted = stringValue(value)
  if (converted) Object.assign(target, { [key]: converted })
}

/** 仅保存有限数值，并在调用方确认来源单位后应用单位换算倍率。 */
function assignNumber(target: Partial<InspirationImageMetadata>, key: keyof InspirationImageMetadata, value: unknown, multiplier = 1): void {
  const converted = numeric(value) * multiplier
  if (Number.isFinite(converted) && value !== undefined && value !== null) Object.assign(target, { [key]: converted })
}

/** 用显式样本数或 PNG 颜色类型推导每像素通道数，统一转换为总位深。 */
function metadataSampleCount(tags: Record<string, unknown>): number {
  const samples = numeric(tags.SamplesPerPixel)
  if (Number.isFinite(samples) && samples > 0) return samples
  const colorType = numeric(tags.ColorType)
  return ({ 0: 1, 2: 3, 3: 1, 4: 2, 6: 4 } as Record<number, number>)[colorType] ?? 1
}

/** 将数值或数值文本转为 number；其他类型返回 NaN 供上层忽略。 */
function numeric(value: unknown): number {
  if (typeof value === 'number') return value
  if (typeof value === 'string' && value.trim()) return Number(value)
  return Number.NaN
}

/** 将解析库返回的常见标量和数组规范为可展示文本，日期保留 ISO 时间信息。 */
function stringValue(value: unknown): string {
  if (value instanceof Date) return value.toISOString()
  if (typeof value === 'string') return value.trim()
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  if (Array.isArray(value)) return value.map(stringValue).filter(Boolean).join(', ')
  return ''
}
