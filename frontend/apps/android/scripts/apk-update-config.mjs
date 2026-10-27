import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/** APK 的发布源复用官网公开配置；原生检查器不从网络响应接收跳转地址。 */
export function apkUpdateConfig(root, env = process.env) {
  const website = resolve(root, '../../../website')
  const values = {}
  for (const name of ['.env', '.env.production']) {
    let text = ''
    try { text = readFileSync(resolve(website, name), 'utf8') } catch {}
    for (const line of text.split(/\r?\n/)) {
      const match = line.match(/^([A-Z_]+)[ \t]*=[ \t]*([^\r\n]*)$/)
      if (match) values[match[1]] = match[2].trim().replace(/^["']|["']$/g, '')
    }
  }
  const site = env.VITE_SITE_URL || values.VITE_SITE_URL
  if (!site) throw new Error('APK 更新检查需要配置官网 VITE_SITE_URL')
  const safe = value => {
    const url = new URL(value)
    if (url.protocol !== 'https:' || url.username || url.password || url.hash || url.search) throw new Error('APK 发布地址必须为不含凭据、查询或片段的 HTTPS 地址')
    return url
  }
  const base = safe(site)
  const feed = safe(env.PHOTO_NINI_APK_RELEASE_URL || new URL(`${values.VITE_ANDROID_ASSET_BASE || '/releases/android'}/release.json`, base).href)
  const download = safe(env.PHOTO_NINI_DOWNLOAD_PAGE_URL || new URL('/download/', base).href)
  if (feed.origin !== base.origin || download.origin !== base.origin) throw new Error('APK 检查与下载页必须属于配置的官网同源')
  return { feedUrl: feed.href, downloadUrl: download.href }
}
