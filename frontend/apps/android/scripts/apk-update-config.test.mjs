import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { apkUpdateConfig } from './apk-update-config.mjs'

test('官网配置生成固定发布源并保留空行语义', () => {
  const fixture = mkdtempSync(join(tmpdir(), 'photo-apk-config-'))
  try {
    const root = join(fixture, 'frontend/apps/android'); mkdirSync(root, { recursive: true })
    mkdirSync(join(fixture, 'website')); const file = join(fixture, 'website/.env')
    writeFileSync(file, 'VITE_SITE_URL=\n# comment\nVITE_ANDROID_ASSET_BASE=/releases/android\n')
    assert.throws(() => apkUpdateConfig(root, {}), /VITE_SITE_URL/)
    writeFileSync(file, 'VITE_SITE_URL=https://example.org\nVITE_ANDROID_ASSET_BASE=/releases/android\n')
    assert.deepEqual(apkUpdateConfig(root, {}), { feedUrl: 'https://example.org/releases/android/release.json', downloadUrl: 'https://example.org/download/' })
    for (const url of ['http://example.org', 'https://u:p@example.org', 'https://example.org/#x']) assert.throws(() => apkUpdateConfig(root, { VITE_SITE_URL: url }))
    assert.throws(() => apkUpdateConfig(root, { PHOTO_NINI_DOWNLOAD_PAGE_URL: 'https://other.invalid/download/' }), /同源/)
  } finally { rmSync(fixture, { recursive: true, force: true }) }
})
