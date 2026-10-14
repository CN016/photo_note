import { describe, expect, it } from 'vitest'
import { fitPreviewDimensions } from './decode'

describe('preview dimensions', () => {
  it('preserves aspect ratio while bounding the longest edge', () => {
    expect(fitPreviewDimensions(12000, 8000, 1800)).toEqual({ width: 1800, height: 1200 })
    expect(fitPreviewDimensions(640, 480, 1800)).toEqual({ width: 640, height: 480 })
    expect(fitPreviewDimensions(0, Number.NaN, 1800)).toEqual({ width: 1, height: 1 })
  })
})
