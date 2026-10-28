/** 图片属性展示只转换语义明确的值；未知值与原始扩展数据保留以供核对。 */
const labels = new Set(["Orientation", "ExposureMode", "SceneCaptureType", "SceneType", "SensingMethod", "OffsetTimeOriginal", "ModifyDate", "SubSecDateTimeOriginal", "ImageSize", "Megapixels", "MIMEType", "FileType", "DeviceManufacturer", "DeviceModel", "ProfileCopyright", "ProfileCreator", "ProfileDescription", "ProfileVersion", "ProfileDateTime", "ColorSpaceData", "RenderingIntent", "PrimaryPlatform", "ProfileClass", "FOV", "HyperfocalDistance", "LightValue", "ExifImageWidth", "ExifImageHeight", "InteropIndex", "OffsetTimeDigitized"])
const aliases: Record<string, string> = { orientation: 'Orientation', exposuremode: 'ExposureMode', scenecapturetype: 'SceneCaptureType', scenetype: 'SceneType', sensingmethod: 'SensingMethod', offsettimeoriginal: 'OffsetTimeOriginal', modifydate: 'ModifyDate', subsecdatetimeoriginal: 'SubSecDateTimeOriginal', imagesize: 'ImageSize', megapixels: 'Megapixels', mimetype: 'MIMEType', filetype: 'FileType', devicemanufacturer: 'DeviceManufacturer', devicemodel: 'DeviceModel', profilecopyright: 'ProfileCopyright', profilecreator: 'ProfileCreator', profiledescription: 'ProfileDescription', profileversion: 'ProfileVersion', profiledatetime: 'ProfileDateTime', colorspacedata: 'ColorSpaceData', renderingintent: 'RenderingIntent', primaryplatform: 'PrimaryPlatform', profileclass: 'ProfileClass', fov: 'FOV', hyperfocaldistance: 'HyperfocalDistance', lightvalue: 'LightValue', exifimagewidth: 'ExifImageWidth', exifimageheight: 'ExifImageHeight', interopindex: 'InteropIndex', offsettimedigitized: 'OffsetTimeDigitized' }
type Translate = (key: string) => string
/** 仅将白名单标签提升为友好属性，并按语义去重；原始列表始终完整保留。 */
export function friendlyExtra(extra: Record<string, unknown>, t: Translate, locale: string) {
  const found = new Map<string, { key: string; label: string; value: string }>()
  for (const [rawKey, rawValue] of Object.entries(extra)) {
    const name = rawKey.split(':').at(-1) || ''
    const key = aliases[name.toLowerCase()]
    if (!key || !labels.has(key) || found.has(key) || rawValue == null || rawValue === '' || typeof rawValue === 'object') continue
    found.set(key, { key, label: t(`editor.metadataLabels.${key}`), value: formatMetadataValue(key, rawValue, t, locale) })
  }
  return [...found.values()]
}
/** 安全格式化已知枚举和数值；不转换未知值、不更改原始元数据。 */
export function formatMetadataValue(key: string, value: unknown, t: Translate, locale: string): string {
  if (Array.isArray(value)) return value.map(item => formatMetadataValue(key, item, t, locale)).join(', ')
  if (value && typeof value === 'object') return JSON.stringify(value)
  if (typeof value === 'boolean') return t(value ? 'editor.metadataYes' : 'editor.metadataNo')
  const text = String(value)
  const enumValues: Record<string, Record<string, string>> = {
    Orientation: { '1': 'orientationNormal', '2': 'orientationMirrorH', '3': 'orientationRotate180', '4': 'orientationMirrorV', '5': 'orientationTranspose', '6': 'orientationRotate90', '7': 'orientationTransverse', '8': 'orientationRotate270', 'horizontal (normal)': 'orientationNormal' },
    ExposureMode: { '0': 'auto', '1': 'manual', '2': 'autoBracket', auto: 'auto', manual: 'manual' },
    SceneCaptureType: { '0': 'standard', '1': 'landscape', '2': 'portrait', '3': 'night', standard: 'standard', landscape: 'landscape', portrait: 'portrait' },
    SceneType: { '1': 'direct', 'directly photographed': 'direct' }, RenderingIntent: { '0': 'perceptual', perceptual: 'perceptual' },
    exposure_program: { '2': 'program', 'program ae': 'program' }, metering_mode: { '2': 'centerWeighted', 'center-weighted average': 'centerWeighted' },
    light_source: { '0': 'unknown', '1': 'daylight', daylight: 'daylight' }, white_balance: { '0': 'auto', '1': 'manual', auto: 'auto', manual: 'manual' },
    flash_mode: { 'compulsory suppression, did not fire': 'flashOff' },
  }
  const translated = enumValues[key]?.[text.toLowerCase()]
  if (translated) return t(`editor.metadataEnums.${translated}`)
  if (key === 'source_last_modified_at' && typeof value === 'string') {
    const date = new Date(value)
    if (Number.isFinite(date.getTime())) return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
  }
  const number = typeof value === 'number' ? value : NaN
  if (Number.isFinite(number)) {
    if (key === 'source_file_size' && number >= 0) return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(number / 1024 / 1024)} MiB (${new Intl.NumberFormat(locale).format(number)} B)`
    if (key === 'exposure_time_seconds' && number > 0 && number < 1) return `1/${Math.round(1 / number)} s`
    if (key === 'aperture_f_number' || key === 'maximum_aperture_f_number') return `f/${new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(number)}`
    if (!Number.isInteger(number)) return new Intl.NumberFormat(locale, { maximumFractionDigits: 4 }).format(number)
  }
  return text
}
/** 复杂值可展开原文，摘要避免在窄列中铺满页面。 */
export function rawMetadataValue(value: unknown): string {
  if (typeof value === 'string') return value
  if (value === undefined) return ''
  return JSON.stringify(value, null, 2) ?? String(value)
}

/** 仅在完整且位于地理范围内的坐标存在时提供地图链接；点击才会向地图服务发送坐标。 */
export function gpsMapUrl(latitude: unknown, longitude: unknown): string | null {
  if (typeof latitude !== 'number' || typeof longitude !== 'number' || !Number.isFinite(latitude) || !Number.isFinite(longitude) || Math.abs(latitude) > 90 || Math.abs(longitude) > 180) return null
  return `https://www.openstreetmap.org/?mlat=${latitude}&mlon=${longitude}#map=16/${latitude}/${longitude}`
}
