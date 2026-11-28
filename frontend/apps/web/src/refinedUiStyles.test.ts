import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

describe('现代视觉层加载顺序', () => {
  it('refined-ui.css 位于全站玻璃和详情专用样式之后', () => {
    const source = readFileSync(resolve(process.cwd(), 'src/main.ts'), 'utf8')
    const glass = source.indexOf("import './glass.css'")
    const detail = source.indexOf("import './inspiration-detail.css'")
    const refined = source.indexOf("import './refined-ui.css'")
    expect(glass).toBeGreaterThanOrEqual(0)
    expect(detail).toBeGreaterThan(glass)
    expect(refined).toBeGreaterThan(detail)
  })

  it('覆盖全部浅色色盘和暗色主题', () => {
    const source = readFileSync(resolve(process.cwd(), 'src/refined-ui.css'), 'utf8')
    for (const selector of ["data-palette='green'", "data-palette='aqua'", "data-palette='blue'", "data-palette='gold'", "data-theme='dark'"]) {
      expect(source).toContain(selector)
    }
  })
})
