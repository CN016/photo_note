import { beforeEach, describe, expect, it, vi } from 'vitest'
import exifr from 'exifr'
import { readInspirationImageMetadata } from './imageMetadata'

vi.mock('exifr', () => ({ default: { parse: vi.fn() } }))

const parse = vi.mocked(exifr.parse)

function photo(): File {
  return new File([new Uint8Array([1, 2, 3, 4])], 'camera.jpg', {
    type: 'image/jpeg',
    lastModified: Date.parse('2026-04-15T01:06:00.000Z'),
  })
}

describe('readInspirationImageMetadata', () => {
  beforeEach(() => parse.mockReset())

  it('maps EXIF, IPTC and XMP values and preserves unzoned dates and namespace tags', async () => {
    parse.mockResolvedValue({
      ifd0: { Make: 'Canon', Model: 'EOS R6', XResolution: 96, YResolution: 96, ResolutionUnit: 'inches', BitDepth: 8, ColorType: 2, ProfileDescription: 'sRGB IEC61966-2.1', Compression: 6 },
      exif: { DateTimeOriginal: '2026:04:15 09:06:00', FNumber: 2.8, ExposureTime: 0.008, ISO: 500, Flash: 1, FlashEnergy: 2.5 },
      gps: { latitude: 31.2, longitude: 121.5 },
      iptc: { ObjectName: 'IMG title', Keywords: ['street', 'night'], Byline: 'Nini' },
      xmp: { Rating: 4, CreatorTool: 'Existing Photo Editor', CustomTag: 'custom-value' },
    })

    const metadata = await readInspirationImageMetadata(photo())

    expect(metadata.source_file_size).toBe(4)
    expect(metadata.source_last_modified_at).toBe('2026-04-15T01:06:00.000Z')
    expect(metadata.camera_make).toBe('Canon')
    expect(metadata.bit_depth).toBe(24)
    expect(metadata.color_space).toBe('sRGB')
    expect(metadata.compression).toBe('JPEG')
    expect(metadata.date_time_original).toBe('2026:04:15 09:06:00')
    expect(metadata.aperture_f_number).toBe(2.8)
    expect(metadata.flash_fired).toBe(true)
    expect(metadata.flash_energy_bcps).toBe(2.5)
    expect(metadata.gps_latitude).toBe(31.2)
    expect(metadata.title).toBe('IMG title')
    expect(metadata.tags).toEqual(['street', 'night'])
    expect(metadata.extra['exifr:xmp:CustomTag']).toBe('custom-value')
  })

  it('keeps browser file metadata when the image has no supported tags or parsing fails', async () => {
    parse.mockResolvedValueOnce(null).mockRejectedValueOnce(new Error('unsupported image'))

    const empty = await readInspirationImageMetadata(photo())
    const failed = await readInspirationImageMetadata(photo())

    expect(empty).toEqual({ source_file_size: 4, source_last_modified_at: '2026-04-15T01:06:00.000Z', extra: {} })
    expect(failed.source_file_size).toBe(4)
    expect(failed.source_last_modified_at).toBe('2026-04-15T01:06:00.000Z')
    expect(failed.extra).toEqual({})
  })

  it('preserves a finite zero last-modified value reported by the File API', async () => {
    parse.mockResolvedValue(null)
    const file = new File([new Uint8Array([1])], 'epoch.jpg', { lastModified: 0 })

    const metadata = await readInspirationImageMetadata(file)

    expect(metadata.source_last_modified_at).toBe('1970-01-01T00:00:00.000Z')
  })

  it('converts JFIF resolution units to DPI', async () => {
    parse.mockResolvedValue({ jfif: { ResolutionUnit: 2, XResolution: 38, YResolution: 76 } })

    const metadata = await readInspirationImageMetadata(photo())

    expect(metadata.horizontal_resolution_dpi).toBeCloseTo(96.52)
    expect(metadata.vertical_resolution_dpi).toBeCloseTo(193.04)
  })

  it('omits oversized extension tags but retains fixed fields and GPS-free images remain valid', async () => {
    parse.mockResolvedValue({
      exif: { Make: 'Canon', Model: 'EOS R6' },
      xmp: { oversized: 'x'.repeat(1024 * 1024 + 32) },
    })

    const metadata = await readInspirationImageMetadata(photo())

    expect(metadata.camera_make).toBe('Canon')
    expect(metadata.gps_latitude).toBeUndefined()
    expect(metadata.extra).toEqual({})
  })
})
