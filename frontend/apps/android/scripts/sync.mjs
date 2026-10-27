import { createPublicKey } from 'node:crypto'
import { cpSync, mkdirSync, readFileSync, writeFileSync, rmSync } from 'node:fs'
import { resolve, dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { apkUpdateConfig } from './apk-update-config.mjs'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
/** 只清理本脚本负责的生成目录，绝不遍历或移动用户文件。 */
export function syncWeb(apiUrl) {
  const assets = join(root, 'native/app/src/main/assets/browser')
  if (dirname(resolve(assets)) !== resolve(root, 'native/app/src/main/assets')) throw new Error('生成目录超出安卓资源范围')
  const api = new URL(apiUrl)
  if (api.protocol !== 'https:') throw new Error('内置浏览器只允许 HTTPS 后端')
  rmSync(assets, { recursive: true, force: true })
  mkdirSync(assets, { recursive: true })
  cpSync(join(root, '../web/dist'), assets, { recursive: true, filter: source => !source.startsWith(join(root, '../web/dist/android-updates')) })
  const version = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8')).version
  // 固定内置扩展负责来源受限的桥接；业务资源从签名包提供，外部网页不获得原生权限。
  const manifest = {
    manifest_version: 2,
    name: '取景台', version,
    browser_specific_settings: { gecko: { id: 'photo-nini@imcn016.love' } },
    permissions: ['nativeMessaging', 'nativeMessagingFromContent', 'geckoViewAddons', `${api.origin}/*`, 'http://127.0.0.1/*'],
    content_scripts: [{ matches: ['http://127.0.0.1/*'], js: ['content-bridge.js'], run_at: 'document_start', all_frames: false }],
    content_security_policy: `script-src 'self' 'wasm-unsafe-eval'; object-src 'self'; connect-src 'self' ${api.origin}; img-src 'self' blob: data:; media-src 'self' blob:; worker-src 'self' blob:; style-src 'self' 'unsafe-inline'; font-src 'self' data:`,
  }
  cpSync(join(root, 'native/app/src/main/assets/native/content-bridge.js'), join(assets, 'content-bridge.js'))
  cpSync(join(root, 'native/app/src/main/assets/native/migrate.js'), join(assets, 'migrate.js'))
  writeFileSync(join(assets, 'migrate.html'), '<!doctype html><html><head><meta charset="utf-8"></head><body><script src="migrate.js"></script></body></html>')
  const shell = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8'))
  const release = JSON.parse(readFileSync(join(assets, 'web-release.json'), 'utf8'))
  if (release.apiBase && release.apiBase !== api.href.replace(/\/$/, '')) throw new Error('APK的API配置与Web构建不一致')
  const updateEnv = ['.env', '.env.production'].map(name => { try { return readFileSync(join(root, '../web', name), 'utf8') } catch { return '' } }).join('\n')
  const updateUrl = process.env.PHOTO_NINI_WEB_UPDATE_URL || process.env.VITE_ANDROID_WEB_UPDATE_URL || [...updateEnv.matchAll(/^VITE_ANDROID_WEB_UPDATE_URL\s*=\s*(.+)$/gm)].at(-1)?.[1]?.trim().replace(/^["']|["']$/g, '') || ''
  const publicFile = updateUrl ? (process.env.PHOTO_NINI_WEB_UPDATE_PUBLIC_KEY || join(root, 'assets/web-update-public.pem')) : ''
  if (Boolean(updateUrl) !== Boolean(publicFile)) throw new Error('更新地址和公钥必须同时配置')
  if (updateUrl && (new URL(updateUrl).protocol !== 'https:' || new URL(updateUrl).username || new URL(updateUrl).password)) throw new Error('更新地址必须为无凭据的HTTPS地址')
  const publicKey = publicFile ? createPublicKey(readFileSync(publicFile)).export({ type: 'spki', format: 'der' }).toString('base64') : ''
  const config = { enabled: Boolean(updateUrl && publicKey), url: updateUrl, publicKey, apiOrigin: api.href.replace(/\/$/, ''), release: release.release, shellCode: shell.androidVersionCode, bridgeVersion: 1, version: release.version }
  writeFileSync(join(root, 'native/app/src/main/assets/update-config.json'), JSON.stringify(config))
  writeFileSync(join(root, 'native/app/src/main/assets/apk-update-config.json'), JSON.stringify(apkUpdateConfig(root)))
  writeFileSync(join(assets, 'manifest.json'), JSON.stringify(manifest, null, 2))
}
