import { homedir } from 'node:os'
import { existsSync, readFileSync, writeFileSync, mkdirSync, readdirSync, statSync, copyFileSync, rmSync } from 'node:fs'
import { resolve, join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createHash, createPrivateKey, createPublicKey, sign, verify } from 'node:crypto'
import { spawnSync } from 'node:child_process'

/** 对明确的发布载荷签名；私钥仅从外部文件读取，不进入资源包或日志。 */
export function signRelease(payload, privatePem) {
  const key = createPrivateKey(privatePem)
  if (key.asymmetricKeyType !== 'rsa' || key.asymmetricKeyDetails.modulusLength < 2048) throw new Error('更新签名要求RSA 2048位或更高')
  const bytes = Buffer.from(JSON.stringify(payload))
  const signature = sign('RSA-SHA256', bytes, key)
  if (!verify('RSA-SHA256', bytes, createPublicKey(key), signature)) throw new Error('签名自检失败')
  return { payload: bytes.toString('base64'), signature: signature.toString('base64') }
}
/** 路径与Android校验规则保持一致，不包含隐藏目录、绝对路径、越界路径和符号链接。 */
export function safePath(name) {
  return Boolean(name) && name.length <= 512 && !name.startsWith('.') && !name.startsWith('/') && !/[\\:\0]/.test(name) && name.split('/').every(part => part && part !== '.' && part !== '..')
}
export function packageWeb(env = process.env) {
  const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
  const dist = resolve(root, '../web/dist')
  const destination = join(dist, 'android-updates')
  const privateFile = env.PHOTO_NINI_WEB_UPDATE_PRIVATE_KEY || join(homedir(), '.photo-nini/keys/web-update-private.pem')
  const updateEnv = ['.env', '.env.production'].map(name => { try { return readFileSync(resolve(root, '../web', name), 'utf8') } catch { return '' } }).join('\n')
  const feed = env.PHOTO_NINI_WEB_UPDATE_URL || env.VITE_ANDROID_WEB_UPDATE_URL || [...updateEnv.matchAll(/^VITE_ANDROID_WEB_UPDATE_URL\s*=\s*(.+)$/gm)].at(-1)?.[1]?.trim().replace(/^["']|["']$/g, '')
  if (!existsSync(privateFile) && !env.PHOTO_NINI_WEB_UPDATE_PRIVATE_KEY && process.argv.includes('--if-configured')) { console.log('未配置安卓更新签名，跳过更新包；普通Web构建已完成。'); return }
  if (!privateFile || !feed) throw new Error('发布更新包需要PHOTO_NINI_WEB_UPDATE_PRIVATE_KEY和PHOTO_NINI_WEB_UPDATE_URL')
  const feedUrl = new URL(feed)
  if (feedUrl.protocol !== 'https:' || feedUrl.username || feedUrl.password || feedUrl.hash) throw new Error('更新清单要求无凭据HTTPS地址')
  const record = JSON.parse(readFileSync(join(dist, 'web-release.json'), 'utf8'))
  if (!Number.isSafeInteger(record.release) || record.release <= 0) throw new Error('发布号无效')
  const configText = ['.env', '.env.production'].map(name => { try { return readFileSync(resolve(root, '../web', name), 'utf8') } catch { return '' } }).join('\n')
  const configured = env.VITE_API_BASE_URL || [...configText.matchAll(/^VITE_API_BASE_URL\s*=\s*(.+)$/gm)].at(-1)?.[1]?.trim().replace(/^["']|["']$/g, '')
  const api = new URL(record.apiBase || configured)
  if (record.apiBase && new URL(configured).href.replace(/\/$/, '') !== record.apiBase) throw new Error('当前API配置与已构建Web不一致，请重新构建')
  if (api.protocol !== 'https:') throw new Error('更新包API必须为HTTPS')
  const files = {}
  function walk(directory, prefix = '') {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const name = prefix + entry.name
      if (name === 'android-updates') continue
      if (!safePath(name) || entry.isSymbolicLink()) throw new Error('更新资源路径不合法')
      if (entry.isDirectory()) walk(join(directory, entry.name), name + '/')
      else if (entry.isFile()) files[name] = createHash('sha256').update(readFileSync(join(directory, entry.name))).digest('hex')
    }
  }
  walk(dist)
  if (!files['index.html'] || Object.keys(files).length > 4096) throw new Error('更新资源数量或入口不合法')
  mkdirSync(destination, { recursive: true })
  const archiveName = `web-${record.release}.zip`
  const archive = join(destination, archiveName)
  const temporary = archive + '.partial'
  // Python标准库跨平台生成ZIP，文件清单显式传入，避免把旧更新包递归打入新包。
  const script = 'import sys,json,zipfile,pathlib; r=pathlib.Path(sys.argv[1]); z=zipfile.ZipFile(sys.argv[2],"w",zipfile.ZIP_DEFLATED,compresslevel=6); [z.write(r/n,n) for n in json.loads(sys.stdin.read())]; z.close()'
  const result = spawnSync(env.PHOTO_NINI_PYTHON || 'python', ['-c', script, dist, temporary], { input: JSON.stringify(Object.keys(files).sort()), encoding: 'utf8' })
  if (result.error || result.status !== 0) throw new Error('ZIP生成失败，请配置PHOTO_NINI_PYTHON')
  if (statSync(temporary).size > 128 * 1024 * 1024) { rmSync(temporary); throw new Error('更新包超过128MiB') }
  const bytes = readFileSync(temporary)
  const payload = { schema: 1, release: record.release, version: record.version, publishedAt: new Date().toISOString(), notes: env.PHOTO_NINI_WEB_UPDATE_NOTES || '', minShellCode: 4, bridgeVersion: 1, apiOrigin: api.href.replace(/\/$/, ''), url: new URL(archiveName, feedUrl).href, bytes: bytes.length, sha256: createHash('sha256').update(bytes).digest('hex'), files }
  const privatePem = readFileSync(privateFile)
  const pinned = createPublicKey(readFileSync(env.PHOTO_NINI_WEB_UPDATE_PUBLIC_KEY || join(root, 'assets/web-update-public.pem'))).export({ type: 'spki', format: 'der' })
  if (!pinned.equals(createPublicKey(createPrivateKey(privatePem)).export({ type: 'spki', format: 'der' }))) throw new Error('签名私钥与APK内置公钥不一致')
  const envelope = signRelease(payload, privatePem)
  copyFileSync(temporary, archive); rmSync(temporary)
  writeFileSync(archive + '.sha256', `${payload.sha256}  ${archiveName}\n`)
  // 不删除历史不可变包：部署时先上传ZIP、最后替换latest.json，避免客户端读到不存在的资源。
  writeFileSync(join(destination, 'latest.json'), JSON.stringify(envelope))
  writeFileSync(join(destination, `release-${record.release}.json`), JSON.stringify({ ...payload, files: undefined }, null, 2))
  console.log(`安卓Web更新包已生成：${archiveName}（${bytes.length}字节）`)
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { packageWeb() } catch (error) { console.error(error.message); process.exitCode = 1 }
}
