/** HSV 色值分量；色相单位为度，饱和度和明度均为 0 至 1。 */
export interface GalleryHsvColor {
  hue: number
  saturation: number
  value: number
}

/** 色环指针结果同时包含最终色值、色相和用于放大预览的圆内位置。 */
export interface GalleryWheelColor {
  color: string
  hue: number
  saturation: number
  x: number
  y: number
}

/** 把 HSV 转成后端筛选使用的 #rrggbb；本组件只选择全明度色相谱。 */
export function galleryHsvToHex(hue: number, saturation: number, value: number): string {
  const normalizedHue = ((hue % 360) + 360) % 360
  const chroma = Math.max(0, Math.min(1, value)) * Math.max(0, Math.min(1, saturation))
  const segment = normalizedHue / 60
  const secondary = chroma * (1 - Math.abs(segment % 2 - 1))
  const [red, green, blue] = segment < 1 ? [chroma, secondary, 0]
    : segment < 2 ? [secondary, chroma, 0]
      : segment < 3 ? [0, chroma, secondary]
        : segment < 4 ? [0, secondary, chroma]
          : segment < 5 ? [secondary, 0, chroma]
            : [chroma, 0, secondary]
  const offset = Math.max(0, Math.min(1, value)) - chroma
  return `#${[red, green, blue].map(channel => Math.round((channel + offset) * 255).toString(16).padStart(2, '0')).join('')}`
}

/** 读取已有色值并还原色相与饱和度，非法值回退到全饱和红色。 */
export function galleryHexToHsv(value: string): GalleryHsvColor {
  const match = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(value)
  if (!match) return { hue: 0, saturation: 1, value: 1 }
  const red = Number.parseInt(match[1]!, 16) / 255
  const green = Number.parseInt(match[2]!, 16) / 255
  const blue = Number.parseInt(match[3]!, 16) / 255
  const maximum = Math.max(red, green, blue)
  const minimum = Math.min(red, green, blue)
  const delta = maximum - minimum
  let hue = 0
  if (delta) {
    if (maximum === red) hue = 60 * (((green - blue) / delta) % 6)
    else if (maximum === green) hue = 60 * ((blue - red) / delta + 2)
    else hue = 60 * ((red - green) / delta + 4)
  }
  return { hue: (hue + 360) % 360, saturation: maximum ? delta / maximum : 0, value: maximum }
}

/**
 * 把色环内指针位置换算成颜色；圆心为白色，边缘为高饱和色。
 * 指针超出圆形时夹到圆周，保证滑动离开色环后仍能连续预览最后一个有效颜色。
 */
export function galleryWheelColorAtPoint(
  clientX: number,
  clientY: number,
  bounds: Pick<DOMRect, 'left' | 'top' | 'width' | 'height'>,
  value = 1,
): GalleryWheelColor {
  const radius = Math.max(1, Math.min(bounds.width, bounds.height) / 2)
  const centerX = bounds.width / 2
  const centerY = bounds.height / 2
  const rawX = clientX - bounds.left - centerX
  const rawY = clientY - bounds.top - centerY
  const rawDistance = Math.hypot(rawX, rawY)
  const scale = rawDistance > radius ? radius / rawDistance : 1
  const x = centerX + rawX * scale
  const y = centerY + rawY * scale
  const hue = ((Math.atan2(rawY, rawX) * 180 / Math.PI) + 90 + 360) % 360
  const saturation = Math.min(1, rawDistance / radius)
  return { color: galleryHsvToHex(hue, saturation, value), hue, saturation, x, y }
}
