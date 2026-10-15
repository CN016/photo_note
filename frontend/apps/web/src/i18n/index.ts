import { createI18n } from 'vue-i18n'
import commonZh from './common.zh-CN.json'
import commonEn from './common.en-US.json'
import loginZh from './login.zh-CN.json'
import loginEn from './login.en-US.json'
import registerZh from './register.zh-CN.json'
import registerEn from './register.en-US.json'
import galleryZh from './gallery.zh-CN.json'
import galleryEn from './gallery.en-US.json'
import editorZh from './editor.zh-CN.json'
import editorEn from './editor.en-US.json'
import maintenanceZh from './maintenance.zh-CN.json'
import maintenanceEn from './maintenance.en-US.json'
import profileZh from './profile.zh-CN.json'
import profileEn from './profile.en-US.json'
import feedbackZh from './feedback.zh-CN.json'
import feedbackEn from './feedback.en-US.json'
import ledgerZh from './ledger.zh-CN.json'
import ledgerEn from './ledger.en-US.json'

/** 每个页面独立维护语言 JSON，统一在此处装配为 Vue I18n 命名空间。 */
export const i18n = createI18n({
  legacy: false,
  locale: localStorage.getItem('photo-nini.locale') === 'en-US' ? 'en-US' : 'zh-CN',
  fallbackLocale: 'zh-CN',
  messages: {
    'zh-CN': { common: commonZh, login: loginZh, register: registerZh, gallery: galleryZh, editor: editorZh, maintenance: maintenanceZh, profile: profileZh, feedback: feedbackZh, ledger: ledgerZh },
    'en-US': { common: commonEn, login: loginEn, register: registerEn, gallery: galleryEn, editor: editorEn, maintenance: maintenanceEn, profile: profileEn, feedback: feedbackEn, ledger: ledgerEn },
  },
})

/** 切换语言同时更新页面语言标记和长期偏好。 */
export function setLocale(locale: 'zh-CN' | 'en-US'): void {
  i18n.global.locale.value = locale
  document.documentElement.lang = locale
  document.title = i18n.global.t('common.brand')
  localStorage.setItem('photo-nini.locale', locale)
}
