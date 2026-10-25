import { cpSync, mkdirSync, readFileSync, writeFileSync, rmSync } from 'node:fs'
import { resolve, dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
/** 只清理本脚本负责的生成目录，绝不遍历或移动用户文件。 */
export function syncWeb(apiUrl) {
  const assets = join(root, 'native/app/src/main/assets/browser')
  if (dirname(resolve(assets)) !== resolve(root, 'native/app/src/main/assets')) throw new Error('生成目录超出安卓资源范围')
  const api = new URL(apiUrl)
  if (api.protocol !== 'https:') throw new Error('内置浏览器只允许 HTTPS 后端')
  rmSync(assets, { recursive: true, force: true })
  mkdirSync(assets, { recursive: true })
  cpSync(join(root, '../web/dist'), assets, { recursive: true })
  const version = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8')).version
  // Web 本身即可信的内置扩展页；仅授予本批生产 API 主机访问权，不放开任意外部地址。
  const manifest = {
    manifest_version: 2,
    name: '取景台', version,
    browser_specific_settings: { gecko: { id: 'photo-nini@imcn016.love' } },
    permissions: ['nativeMessaging', 'geckoViewAddons', `${api.origin}/*`],
    content_security_policy: `script-src 'self' 'wasm-unsafe-eval'; object-src 'self'; connect-src 'self' ${api.origin}; img-src 'self' blob: data:; media-src 'self' blob:; worker-src 'self' blob:; style-src 'self' 'unsafe-inline'; font-src 'self' data:`,
  }
  writeFileSync(join(assets, 'manifest.json'), JSON.stringify(manifest, null, 2))
}
