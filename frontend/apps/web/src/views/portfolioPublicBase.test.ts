import { describe, expect, it } from 'vitest'
import { portfolioPublicBase } from './portfolioPublicBase'
describe('作品集公开地址', () => {
  it('使用配置地址或安卓更新清单对应的公开站点', () => {
    expect(portfolioPublicBase('https://example.test/app/', '', 'http://127.0.0.1')).toBe('https://example.test/app/')
    expect(portfolioPublicBase('', 'https://example.test/app/android-updates/latest.json', 'http://127.0.0.1')).toBe('https://example.test/app/')
    expect(portfolioPublicBase('', '', 'https://web.example.test')).toBe('https://web.example.test/')
  })
  it('拒绝本机、非 HTTPS、凭据和不合法地址', () => {
    expect(portfolioPublicBase('', '', 'http://127.0.0.1')).toBeNull()
    expect(portfolioPublicBase('https://user:pass@example.test', '', '')).toBeNull()
    expect(portfolioPublicBase('garbage', '', '')).toBeNull()
  })
})
