import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'

/** 验证已部署静态文件的响应和完整字节；仅校验，不保存或执行安装包。 */
const manifestUrl = process.argv[2]
if (!manifestUrl) throw new Error('请传入完整 release.json 地址，例如 http://127.0.0.1:4176/releases/android/release.json')
const manifestResponse = await fetch(manifestUrl)
assert.equal(manifestResponse.status, 200)
const manifest = await manifestResponse.json()
assert.deepEqual(manifest.variants.map(item => item.abi).sort(), ['arm64-v8a', 'armeabi-v7a', 'x86_64'].sort())
for (const item of manifest.variants) {
  const url = new URL(item.url, manifestUrl)
  assert.equal(url.origin, new URL(manifestUrl).origin)
  const response = await fetch(url)
  assert.equal(response.status, 200, item.filename)
  assert.match(response.headers.get('content-type') || '', /application\/vnd.android.package-archive/)
  const hash = createHash('sha256')
  let bytes = 0
  for await (const chunk of response.body) { hash.update(chunk); bytes += chunk.length }
  assert.equal(bytes, item.bytes)
  assert.equal(hash.digest('hex'), item.sha256)
  const checksum = await fetch(new URL(item.checksumUrl, url))
  assert.equal(checksum.status, 200)
  assert.equal((await checksum.text()).trim().split(/\s+/)[0], item.sha256)
  console.log(`${item.abi}: HTTP、MIME、长度、SHA-256、校验文件通过`)
}
assert.equal((await fetch(new URL('missing.apk', manifestUrl))).status, 404)
console.log('不存在的安装包返回 404：通过')
