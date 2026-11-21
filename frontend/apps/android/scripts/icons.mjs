import sharp from 'sharp'
import { mkdir, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { dirname, resolve, join } from 'node:path'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const res = join(root, 'native/app/src/main/res')
const source = join(root, 'assets/lightnote-icon.svg')
const foregroundSource = join(root, 'assets/lightnote-icon-foreground.svg')
/** 同一矢量母版生成传统图标、自适应前景、单色图标和启动屏，避免各尺寸漂移。 */
await sharp(source).resize(1024, 1024).png().toFile(join(root, 'assets/icon-master.png'))
for (const [density, scale] of Object.entries({ mdpi: 1, hdpi: 1.5, xhdpi: 2, xxhdpi: 3, xxxhdpi: 4 })) {
  const folder = join(res, `mipmap-${density}`)
  await mkdir(folder, { recursive: true })
  const size = Math.round(108 * scale)
  const foreground = await sharp(foregroundSource).resize(Math.round(size * 0.84), Math.round(size * 0.84)).toBuffer()
  await sharp({ create: { width: size, height: size, channels: 4, background: '#00000000' } }).composite([{ input: foreground, gravity: 'centre' }]).png().toFile(join(folder, 'ic_launcher_foreground.png'))
  // 传统入口直接使用满幅纯色矩形，不额外生成外边距、边框或预烘焙圆角。
  const flat = await sharp(source).resize(48 * scale, 48 * scale).png().toBuffer()
  await writeFile(join(folder, 'ic_launcher.png'), flat)
  // 方形母版不预烘焙圆角；旧版圆形入口只在生成 round 资源时裁切。
  const diameter = 48 * scale
  const circle = Buffer.from(`<svg xmlns="http://www.w3.org/2000/svg" width="${diameter}" height="${diameter}"><circle cx="${diameter / 2}" cy="${diameter / 2}" r="${diameter / 2}" fill="#fff"/></svg>`)
  await sharp(flat).composite([{ input: circle, blend: 'dest-in' }]).png().toFile(join(folder, 'ic_launcher_round.png'))
  const alpha = await sharp(join(folder, 'ic_launcher_foreground.png')).extractChannel('alpha').toBuffer()
  await sharp({ create: { width: size, height: size, channels: 3, background: '#FFFFFF' } }).joinChannel(alpha).png().toFile(join(folder, 'ic_launcher_monochrome.png'))
}
const iconXml = '<?xml version="1.0" encoding="utf-8"?>\n<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">\n    <background android:drawable="@color/ic_launcher_background"/>\n    <foreground android:drawable="@mipmap/ic_launcher_foreground"/>\n    <monochrome android:drawable="@mipmap/ic_launcher_monochrome"/>\n</adaptive-icon>\n'
await mkdir(join(res, 'mipmap-anydpi-v33'), { recursive: true })
for (const name of ['ic_launcher', 'ic_launcher_round']) await writeFile(join(res, `mipmap-anydpi-v33/${name}.xml`), iconXml)
await mkdir(join(res, 'drawable-nodpi'), { recursive: true })
await sharp(source).resize(288, 288).toFile(join(res, 'drawable-nodpi/splash_icon.png'))
console.log('已生成 LightNote 全部密度、自适应、单色和启动屏图标')
