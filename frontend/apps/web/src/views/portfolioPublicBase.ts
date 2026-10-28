/** 作品集分享地址必须面向外部；Android 本地资源地址不能作为公开域名。 */
export function portfolioPublicBase(configured: string | undefined, updateURL: string | undefined, origin: string): string | null {
  try {
    let candidate = configured?.trim() || ''
    if (!candidate && updateURL?.trim()) {
      const update = new URL(updateURL.trim())
      const suffix = '/android-updates/latest.json'
      if (update.pathname.endsWith(suffix)) { update.pathname = update.pathname.slice(0, -suffix.length) + '/'; candidate = update.href }
    }
    if (!candidate) candidate = origin
    const url = new URL(candidate)
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash || ['127.0.0.1', 'localhost', '::1'].includes(url.hostname)) return null
    return url.href.endsWith('/') ? url.href : `${url.href}/`
  } catch { return null }
}
