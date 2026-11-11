import { beforeEach, describe, expect, it, vi } from 'vitest'
vi.mock('gsap', () => ({ gsap: { to: vi.fn(), killTweensOf: vi.fn() } }))

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
