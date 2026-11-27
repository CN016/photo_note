import { describe, expect, it } from 'vitest'
import { imageExportBackSteps } from './exportNavigation'

describe('图片导出成功后的返回步数', () => {
  it('属性面板位于详情页之后时回退两层到进入图片前的页面', () => {
    expect(imageExportBackSteps(true)).toBe(-2)
  })

  it('水印模式或直接打开属性地址时只回退一层', () => {
    expect(imageExportBackSteps(false)).toBe(-1)
  })
})
