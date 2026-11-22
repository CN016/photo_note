import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const source = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

/** 锁定移动端项目详情壳与详情浮层的集成边界，防止样式编译或模态层级回归。 */
describe('移动端外壳与详情浮层一致性', () => {
  it('项目编辑底栏由全局选择器隐藏，避免 scoped CSS 把 body 本身隐藏', () => {
    const appHeader = source('./components/AppHeader.vue')
    const globalStyles = source('./style.css')
    expect(appHeader).not.toContain(':global(.project-editor-route) .mobile-navigation-bar')
    expect(globalStyles).toMatch(/\.project-editor-route \.mobile-navigation-bar[\s\S]*?display:\s*none\s*!important/)
  })

  it('图片属性与其他工具共用锚定玻璃浮层，不重新引入模态对话框', () => {
    const toolSurface = source('./components/EditorToolSurface.vue')
    expect(toolSurface).toContain('<el-popover')
    expect(toolSurface).not.toContain('<el-dialog')
    expect(toolSurface).toContain("panel === 'properties' ? 'bottom-end' : 'top'")
  })
})
