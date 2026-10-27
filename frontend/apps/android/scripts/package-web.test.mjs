import { test } from 'node:test'
import assert from 'node:assert/strict'
import { generateKeyPairSync, verify } from 'node:crypto'
import { signRelease, safePath } from './package-web.mjs'

// 使用即时生成的测试密钥，不创建或保存生产私钥。
test('发布载荷能验证且篡改后拒绝', () => {
  const { privateKey, publicKey } = generateKeyPairSync('rsa', { modulusLength: 2048 })
  const envelope = signRelease({ release: 42, files: { 'index.html': 'digest' } }, privateKey.export({ type: 'pkcs8', format: 'pem' }))
  const data = Buffer.from(envelope.payload, 'base64')
  assert.equal(verify('RSA-SHA256', data, publicKey, Buffer.from(envelope.signature, 'base64')), true)
  assert.equal(verify('RSA-SHA256', Buffer.concat([data, Buffer.from(' ')]), publicKey, Buffer.from(envelope.signature, 'base64')), false)
})
test('拒绝目录穿越、绝对路径和Windows路径', () => {
  for (const name of ['../secret', '/absolute', 'a/../b', 'C:/x', 'a\\b', 'a//b', '.release.json']) assert.equal(safePath(name), false, name)
  for (const name of ['index.html', 'assets/app.js', 'vendor/model.wasm']) assert.equal(safePath(name), true)
})
