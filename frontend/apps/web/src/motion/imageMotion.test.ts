import { beforeEach, describe, expect, it, vi } from 'vitest'
vi.mock('gsap', () => ({ gsap: { to: vi.fn(), set: vi.fn(), killTweensOf: vi.fn(), isTweening: vi.fn(() => false) } }))

/** 模拟独立像素副本，验证源画布卸载后仍可播放；真实像素另外由浏览器读取核对。 */
class TestImage { naturalWidth = 4000; naturalHeight = 2000; complete = true; pixel = 37 }
class TestCanvas {
  width = 0; height = 0; pixel = 0; style = {}; root: any = null
  getContext() { return { drawImage: (source: TestCanvas | TestImage) => { this.pixel = source.pixel } } }
  closest() { return this.root }
  getBoundingClientRect() { return { left: 24, top: 100, width: 300, height: 375 } }
}
class TestLayer {
  dataset = {}; style = {}; children: TestCanvas[] = []; root: any
  setAttribute() {}
  appendChild(child: TestCanvas) { this.children.push(child) }
  remove() { if (this.root) this.root.children = this.root.children.filter((item: TestLayer) => item !== this) }
}
const root = () => ({ children: [] as TestLayer[], offsetWidth: 390, offsetHeight: 844,
  getBoundingClientRect: () => ({ left: 0, top: 0, width: 390, height: 844 }),
  appendChild(layer: TestLayer) { layer.root = this; this.children.push(layer) },
})
let images: typeof import('./imageMotion')
beforeEach(async () => {
  vi.resetModules(); vi.clearAllMocks()
  vi.stubGlobal('HTMLImageElement', TestImage)
  vi.stubGlobal('document', { createElement: (tag: string) => tag === 'canvas' ? new TestCanvas() : new TestLayer() })
  images = await import('./imageMotion')
})
describe('导航真实图片交接', () => {
  it('限制快照长边，不复制完整超大画布', () => {
    const copy = images.copyVisibleImage(new TestImage() as any)!
    expect([copy.width, copy.height]).toEqual([1600, 800])
    expect((copy as any).pixel).toBe(37)
  })
  it('没有解码出像素时拒绝制造空图片动画', () => {
    const image = new TestImage(); image.naturalWidth = 0
    expect(images.captureListImage({ querySelector: () => image } as any)).toBe(false)
    expect(images.copyVisibleImage(new TestCanvas() as any)).toBeNull()
  })
  it('展开优先使用已准备的详情像素，释放原画布后仍保留清晰快照', () => {
    const marker = { querySelector: () => new TestImage() }
    const detail = new TestCanvas(); detail.width = 1200; detail.height = 800; detail.pixel = 91
    images.prepareListImage(marker as any, detail as any)
    expect(images.captureListImage(marker as any)).toBe(true)
    detail.width = 0; detail.pixel = 0
    const page = root(); images.attachListImage(page as any)
    expect(page.children[0]!.children[0]!.pixel).toBe(91)
    images.resetMotionImages()
    expect(page.children).toHaveLength(0)
  })
  it('中途返回保留正在展开的预览，不跳成另一个完整图片框', () => {
    const page = root()
    images.captureListImage({ querySelector: () => new TestImage() } as any)
    images.attachListImage(page as any)
    const original = page.children[0]
    const source = new TestCanvas(); source.width = 640; source.height = 800; source.pixel = 91; source.root = page
    images.captureDetailImage(source as any)
    expect(page.children[0]).toBe(original)
    expect(page.children[0]!.children[0]!.pixel).toBe(37)
  })
  it('首帧补偿根节点非等比缩放，复现横图和竖图的列表cover裁切', () => {
    for (const [w, h] of [[1600, 800], [800, 1600], [1000, 1000]]) {
      const rect = images.sourceImageRect(w!, h!, 160, 190, 390, 844)
      const screenWidth = rect.width * 160 / 390, screenHeight = rect.height * 190 / 844
      expect(screenWidth / screenHeight).toBeCloseTo(w! / h!)
      expect(screenWidth).toBeGreaterThanOrEqual(160)
      expect(screenHeight).toBeGreaterThanOrEqual(190)
      expect(rect.x * 160 / 390).toBeCloseTo((160 - screenWidth) / 2)
      expect(rect.y * 190 / 844).toBeCloseTo((190 - screenHeight) / 2)
    }
  })
  it('详情就绪仅插值合成层变换，不逐帧改变布局尺寸', async () => {
    const { gsap } = await import('gsap')
    const page = root()
    images.captureListImage({ querySelector: () => new TestImage() } as any)
    images.attachListImage(page as any)
    const source = new TestCanvas(); source.width = 1200; source.height = 800; source.root = page
    images.detailImageReady(source as any)
    const pixels = page.children[0]!.children[0]!
    const geometry = vi.mocked(gsap.to).mock.calls.at(-1)![1] as any
    expect(geometry).toMatchObject({ x: 24, y: 100, scaleX: 300 / pixels.width, scaleY: 375 / pixels.height, overwrite: true })
    expect(geometry).not.toHaveProperty('width')
    expect(geometry).not.toHaveProperty('height')
    expect(geometry.duration).toBeGreaterThanOrEqual(0.02)
    expect(geometry.duration).toBeLessThanOrEqual(0.16)
    images.completeImageOpen(page as any)
    images.captureDetailImage(source as any)
    const snapshot = page.children[0]!.children[0]!
    expect(vi.mocked(gsap.set).mock.calls.at(-1)![1]).toMatchObject({ x: 24, y: 100, scaleX: 300 / snapshot.width, scaleY: 375 / snapshot.height })
  })
  it('完整详情返回先复制真实像素，释放源和结束动画均不泄漏资源', () => {
    const page = root()
    images.captureListImage({ querySelector: () => new TestImage() } as any)
    images.attachListImage(page as any); images.completeImageOpen(page as any)
    const source = new TestCanvas(); source.width = 640; source.height = 800; source.pixel = 91; source.root = page
    images.captureDetailImage(source as any)
    source.width = 0; source.height = 0; source.pixel = 0
    const snapshot = page.children[0]!.children[0]!
    expect([snapshot.width, snapshot.height, snapshot.pixel]).toEqual([640, 800, 91])
    images.resetMotionImages()
    expect(page.children).toHaveLength(0)
    expect([snapshot.width, snapshot.height]).toEqual([0, 0])
  })
})
