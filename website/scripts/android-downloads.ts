import { createReadStream, copyFileSync, existsSync, mkdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { fileURLToPath } from 'node:url'
import { resolve, join } from 'node:path'
import type { Plugin } from 'vite'

/** 随官网部署真实 APK：先校验来源与摘要，再生成页面元数据和静态下载文件。 */
export function androidDownloads(env: Record<string, string>): Plugin {
  const enabled = env.VITE_ANDROID_BUNDLED_DOWNLOADS === 'true'
  const root = fileURLToPath(new URL('../', import.meta.url))
  const android = resolve(root, '../frontend/apps/android')
  const base = (env.VITE_ANDROID_ASSET_BASE || '').replace(/\/$/, '')
  if (enabled && !/^\/[a-zA-Z0-9_-]+(?:\/[a-zA-Z0-9_-]+)*$/.test(base)) throw new Error('VITE_ANDROID_ASSET_BASE 必须为官网同源根路径，例如 /releases/android')
  type Item = { abi: string; filename: string; url: string; checksumUrl: string; bytes: number; sha256: string }
  let records: Item[] = []
  let version = ''
  let ready: Promise<void> | undefined
  let output = ''
  let building = false
  async function sha256(path: string) {
    const hash = createHash('sha256')
    for await (const chunk of createReadStream(path)) hash.update(chunk)
    return hash.digest('hex')
  }
  function prepare() {
    return ready ||= (async () => {
      if (!enabled) return
      version = JSON.parse(readFileSync(join(android, 'package.json'), 'utf8')).version
      if (!/^\d+\.\d+\.\d+$/.test(version)) throw new Error('安卓应用版本格式无效')
      for (const abi of ['arm64-v8a', 'armeabi-v7a']) {
        const filename = `photo-nini-${version}-${abi}-debug.apk`
        const path = join(android, 'artifacts', filename)
        try {
          const expected = readFileSync(`${path}.sha256`, 'utf8').trim().split(/\s+/)[0]
          const actual = await sha256(path)
          if (!/^[a-f0-9]{64}$/.test(expected) || expected !== actual) throw new Error('摘要不匹配')
          records.push({ abi, filename, url: `${base}/${filename}`, checksumUrl: `${base}/${filename}.sha256`, bytes: statSync(path).size, sha256: actual })
        } catch { throw new Error(`安卓安装包缺失或校验失败：${filename}。请先在 frontend/apps/android 执行 npm run build，或关闭 VITE_ANDROID_BUNDLED_DOWNLOADS。`) }
      }
    })()
  }
  return {
    name: 'verified-android-downloads',
    configResolved(config) { output = resolve(root, config.build.outDir); building = config.command === 'build' },
    async buildStart() { await prepare() },
    resolveId(id) { if (id === 'virtual:android-downloads') return '\0android-downloads' },
    async load(id) {
      if (id !== '\0android-downloads') return
      await prepare()
      return `export default ${JSON.stringify({ version, variants: records })}`
    },
    async closeBundle() {
      if (!enabled || !building) return
      await prepare()
      const destination = join(output, base.slice(1))
      mkdirSync(destination, { recursive: true })
      for (const item of records) {
        const target = join(destination, item.filename)
        copyFileSync(join(android, 'artifacts', item.filename), target)
        if (await sha256(target) !== item.sha256) throw new Error(`安装包复制校验失败：${item.filename}`)
        writeFileSync(`${target}.sha256`, `${item.sha256}  ${item.filename}\n`)
      }
      writeFileSync(join(destination, 'release.json'), JSON.stringify({ version, channel: 'debug', minAndroid: 9, variants: records }, null, 2))
    },
    configureServer(server) {
      server.middlewares.use(async (request, response, next) => {
        if (!enabled) return next()
        await prepare()
        const pathname = request.url?.split('?')[0]
        const item = records.find(item => item.url === pathname || item.checksumUrl === pathname)
        if (!item) return next()
        if (pathname === item.checksumUrl) { response.setHeader('Content-Type', 'text/plain; charset=utf-8'); response.end(`${item.sha256}  ${item.filename}\n`); return }
        response.setHeader('Content-Type', 'application/vnd.android.package-archive')
        response.setHeader('Content-Length', item.bytes)
        response.setHeader('Content-Disposition', `attachment; filename="${item.filename}"`)
        if (request.method === 'HEAD') response.end()
        else createReadStream(join(android, 'artifacts', item.filename)).pipe(response)
      })
    },
    configurePreviewServer(server) {
      if (!enabled) return
      // 预览只使用 dist 内的实际文件；缺失安装包返回 404，不能回退成 HTML 下载。
      server.middlewares.use((request, response, next) => {
        const pathname = request.url?.split('?')[0]
        if (!pathname?.startsWith(`${base}/`)) return next()
        const filename = pathname.slice(base.length + 1)
        if (!/^photo-nini-\d+\.\d+\.\d+-(arm64-v8a|armeabi-v7a)-debug\.apk(?:\.sha256)?$/.test(filename) && filename !== 'release.json') { response.statusCode = 404; response.end(); return }
        if (!existsSync(join(output, base.slice(1), filename))) { response.statusCode = 404; response.end(); return }
        response.setHeader('Content-Type', filename.endsWith('.apk') ? 'application/vnd.android.package-archive' : filename.endsWith('.json') ? 'application/json' : 'text/plain; charset=utf-8')
        if (filename.endsWith('.apk')) response.setHeader('Content-Disposition', `attachment; filename="${filename}"`)
        next()
      })
    },
  }
}
