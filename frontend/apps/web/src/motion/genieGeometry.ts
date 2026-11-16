/** Genie 几何参考 BCGenieEffect（MIT）：先弯曲边界，再沿漏斗滑入目标。许可见 THIRD_PARTY_NOTICES。 */
export interface GenieRect { x: number; y: number; width: number; height: number }
const clamp = (value: number) => Math.max(0, Math.min(1, value))
const smooth = (value: number) => { const t = clamp(value); return t * t * (3 - 2 * t) }
const mix = (a: number, b: number, t: number) => a + (b - a) * t

/** 求控制点位于纵轴中点的三次贝塞尔曲线横向进度；二分保证端点和极端窄屏数值稳定。 */
function curveCrossSection(axis: number): number {
  let lo = 0, hi = 1
  for (let i = 0; i < 18; i++) {
    const t = (lo + hi) / 2
    const y = 1.5 * t - 1.5 * t * t + t * t * t
    if (y < axis) lo = t; else hi = t
  }
  return smooth((lo + hi) / 2)
}

/**
 * 将原画面网格坐标映射到屏幕位置。progress=0 为列表框，1 为完整详情；同一函数倒放保证轨迹一致。
 * 虚拟滑道至少为画面高度，实际口部再随滑动落到目标，避免列表框位于画面内部时曲线翻折。
 */
export function geniePoint(source: GenieRect, destination: GenieRect, progress: number, u: number, v: number): [number, number] {
  const collapse = 1 - clamp(progress)
  if (collapse === 0) return [source.x + u * source.width, source.y + v * source.height]
  if (collapse === 1) return [destination.x + u * destination.width, destination.y + v * destination.height]
  const down = destination.y + destination.height / 2 >= source.y + source.height / 2
  const direction = down ? 1 : -1
  const start = down ? source.y : source.y + source.height
  const mouth = down ? destination.y : destination.y + destination.height
  const depth = Math.max(source.height, direction * (mouth - start), 1)
  const bend = smooth(collapse / 0.4)
  const slide = smooth((collapse - 0.3) / 0.7)
  const along = (down ? v : 1 - v) * source.height + slide * depth
  const mouthLeft = mix(source.x, destination.x, bend)
  const mouthRight = mix(source.x + source.width, destination.x + destination.width, bend)
  const movingMouth = mix(start + direction * depth, mouth, slide)
  if (along >= depth) {
    return [mix(mouthLeft, mouthRight, u), movingMouth + direction * (along - depth) * destination.height / source.height]
  }
  const cross = curveCrossSection(along / depth)
  const left = mix(source.x, mouthLeft, cross), right = mix(source.x + source.width, mouthRight, cross)
  return [mix(left, right, u), mix(start, movingMouth, along / depth)]
}
