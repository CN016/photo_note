import { describe, expect, it } from 'vitest'
import { friendlyExtra, formatMetadataValue, rawMetadataValue, gpsMapUrl } from './imageMetadataDisplay'

const t = (key: string) => key

describe('图片属性展示', () => {
  it('只翻译已知枚举，未知值原样保留', () => {
    expect(formatMetadataValue('Orientation', 6, t, 'zh-CN')).toBe('editor.metadataEnums.orientationRotate90')
    expect(formatMetadataValue('light_source', 21, t, 'zh-CN')).toBe('21')
    expect(formatMetadataValue('exposure_program', 'Program AE', t, 'zh-CN')).toBe('editor.metadataEnums.program')
    expect(formatMetadataValue('exposure_time_seconds', 1 / 125, t, 'en-US')).toBe('1/125 s')
    expect(formatMetadataValue('aperture_f_number', 2.8, t, 'en-US')).toBe('f/2.8')
  })
  it('只为有效经纬度生成地图链接', () => {
    expect(gpsMapUrl(31.2, 121.5)).toContain('mlat=31.2&mlon=121.5')
    expect(gpsMapUrl(null, 121)).toBeNull()
    expect(gpsMapUrl(91, 121)).toBeNull()
    expect(gpsMapUrl(0, 0)).not.toBeNull()
  })
  it('对多来源同一标签去重，同时不把未知和复杂值误认成友好属性', () => {
    const rows = friendlyExtra({ 'exifr:ifd0:Orientation': 'Horizontal (normal)', 'ExifTool:Orientation': 1, 'ExifTool:Unknown': 99, 'exifr:icc:BlueTRC': { 0: 1 } }, t, 'en-US')
    expect(rows).toHaveLength(1)
    expect(rows[0].value).toBe('editor.metadataEnums.orientationNormal')
    expect(rawMetadataValue({ 0: 1 })).toContain('"0": 1')
    expect(rawMetadataValue(['a', 'b'])).toContain('"b"')
  })
})
