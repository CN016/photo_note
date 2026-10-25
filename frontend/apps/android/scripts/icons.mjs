import sharp from 'sharp'
import { mkdir, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { dirname, resolve, join } from 'node:path'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const res = join(root, 'native/app/src/main/res')
const source = join(root, 'assets/icon-master.png')
/** 仅缩放、布局和提取透明度生成 Android 密度资源，原创图形由 imagegen 提供。 */
for (const [density, scale] of Object.entries({ mdpi: 1, hdpi: 1.5, xhdpi: 2, xxhdpi: 3, xxxhdpi: 4 })) {
  const folder = join(res, `mipmap-${density}`)
  await mkdir(folder, { recursive: true })
  const size = Math.round(108 * scale)
  const foreground = await sharp(source).resize(Math.round(size * 0.88), Math.round(size * 0.88)).toBuffer()
  await sharp({ create: { width: size, height: size, channels: 4, background: '#00000000' } }).composite([{ input: foreground, gravity: 'centre' }]).png().toFile(join(folder, 'ic_launcher_foreground.png'))
  const launcher = await sharp(source).resize(Math.round(48 * scale * 0.9)).toBuffer()
  const flat = await sharp({ create: { width: 48 * scale, height: 48 * scale, channels: 4, background: '#F3F7FE' } }).composite([{ input: launcher, gravity: 'centre' }]).png().toBuffer()
  await writeFile(join(folder, 'ic_launcher.png'), flat)
  // 旧系统的圆形入口仍由 Launcher 掩膜处理；底图保留完整背景。
  await writeFile(join(folder, 'ic_launcher_round.png'), flat)
  const alpha = await sharp(join(folder, 'ic_launcher_foreground.png')).extractChannel('alpha').toBuffer()
  await sharp({ create: { width: size, height: size, channels: 3, background: '#FFFFFF' } }).joinChannel(alpha).png().toFile(join(folder, 'ic_launcher_monochrome.png'))
}
const iconXml = '<?xml version="1.0" encoding="utf-8"?>\n<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">\n    <background android:drawable="@color/ic_launcher_background"/>\n    <foreground android:drawable="@mipmap/ic_launcher_foreground"/>\n    <monochrome android:drawable="@mipmap/ic_launcher_monochrome"/>\n</adaptive-icon>\n'
await mkdir(join(res, 'mipmap-anydpi-v33'), { recursive: true })
for (const name of ['ic_launcher', 'ic_launcher_round']) await writeFile(join(res, `mipmap-anydpi-v33/${name}.xml`), iconXml)
await mkdir(join(res, 'drawable-nodpi'), { recursive: true })
await sharp(source).resize(288, 288).toFile(join(res, 'drawable-nodpi/splash_icon.png'))
console.log('已生成全部密度、自适应、单色和启动屏图标')
