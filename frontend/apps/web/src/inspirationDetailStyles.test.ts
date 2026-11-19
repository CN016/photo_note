import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

// 详情专用覆盖必须晚于全站玻璃层加载，否则 glass.css 的 !important 会再次覆盖图片舞台与 Dock。
describe('灵感图详情样式加载顺序', () => {
  it('inspiration-detail.css 位于 glass.css 之后', () => {
    const source = readFileSync(resolve(process.cwd(), 'src/main.ts'), 'utf8')
    const glass = source.indexOf("import './glass.css'")
    const detail = source.indexOf("import './inspiration-detail.css'")
    expect(glass).toBeGreaterThanOrEqual(0)
    expect(detail).toBeGreaterThan(glass)
  })
})
