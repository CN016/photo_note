import { createI18n } from 'vue-i18n'
import portfolioZh from './portfolio.zh-CN.json'
import portfolioEn from './portfolio.en-US.json'
import publicPortfolioZh from './public-portfolio.zh-CN.json'
import publicPortfolioEn from './public-portfolio.en-US.json'
import commonZh from './common.zh-CN.json'
import commonEn from './common.en-US.json'
import loginZh from './login.zh-CN.json'
import loginEn from './login.en-US.json'
import registerZh from './register.zh-CN.json'
import registerEn from './register.en-US.json'
import galleryZh from './gallery.zh-CN.json'
import galleryEn from './gallery.en-US.json'
import statisticsZh from './statistics.zh-CN.json'
import statisticsEn from './statistics.en-US.json'
import editorZh from './editor.zh-CN.json'
import editorEn from './editor.en-US.json'
import inspirationUploadZh from './inspiration-upload.zh-CN.json'
import inspirationUploadEn from './inspiration-upload.en-US.json'
import maintenanceZh from './maintenance.zh-CN.json'
import maintenanceEn from './maintenance.en-US.json'
import profileZh from './profile.zh-CN.json'
import profileEn from './profile.en-US.json'
import feedbackZh from './feedback.zh-CN.json'
import feedbackEn from './feedback.en-US.json'
import ledgerZh from './ledger.zh-CN.json'
import ledgerEn from './ledger.en-US.json'
import scheduleZh from './schedule.zh-CN.json'
import scheduleEn from './schedule.en-US.json'
import plansZh from './plans.zh-CN.json'
import plansEn from './plans.en-US.json'
import projectsZh from './projects.zh-CN.json'
import projectsEn from './projects.en-US.json'
import workbenchZh from './workbench.zh-CN.json'
import workbenchEn from './workbench.en-US.json'
import messagesZh from './messages.zh-CN.json'
import messagesEn from './messages.en-US.json'
import consoleZh from './console.zh-CN.json'
import consoleEn from './console.en-US.json'
import spacesZh from './spaces.zh-CN.json'
import spacesEn from './spaces.en-US.json'
import organizationsZh from './organizations.zh-CN.json'
import organizationsEn from './organizations.en-US.json'
import organizationConsoleZh from './organization-console.zh-CN.json'
import organizationConsoleEn from './organization-console.en-US.json'

/** 每个页面独立维护语言 JSON，统一在此处装配为 Vue I18n 命名空间。 */
export const i18n = createI18n({
  legacy: false,
  locale: localStorage.getItem('photo-nini.locale') === 'en-US' ? 'en-US' : 'zh-CN',
  fallbackLocale: 'zh-CN',
  messages: {
    'zh-CN': { portfolio: portfolioZh, publicPortfolio: publicPortfolioZh, common: commonZh, login: loginZh, register: registerZh, gallery: galleryZh, statistics: statisticsZh, editor: editorZh, inspirationUpload: inspirationUploadZh, maintenance: maintenanceZh, profile: profileZh, feedback: feedbackZh, ledger: ledgerZh, schedule: scheduleZh, plans: plansZh, projects: projectsZh, workbench: workbenchZh, messages: messagesZh, console: consoleZh, spaces: spacesZh, organizations: organizationsZh, organizationConsole: organizationConsoleZh },
    'en-US': { portfolio: portfolioEn, publicPortfolio: publicPortfolioEn, common: commonEn, login: loginEn, register: registerEn, gallery: galleryEn, statistics: statisticsEn, editor: editorEn, inspirationUpload: inspirationUploadEn, maintenance: maintenanceEn, profile: profileEn, feedback: feedbackEn, ledger: ledgerEn, schedule: scheduleEn, plans: plansEn, projects: projectsEn, workbench: workbenchEn, messages: messagesEn, console: consoleEn, spaces: spacesEn, organizations: organizationsEn, organizationConsole: organizationConsoleEn },
  },
})

/** 切换语言同时更新页面语言标记和长期偏好。 */
export function setLocale(locale: 'zh-CN' | 'en-US'): void {
  i18n.global.locale.value = locale
  document.documentElement.lang = locale
  document.title = i18n.global.t('common.brand')
  localStorage.setItem('photo-nini.locale', locale)
}
