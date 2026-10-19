import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { i18n, setLocale } from './i18n'
import { router } from './router'
import 'element-plus/es/components/message/style/css'
import 'element-plus/es/components/notification/style/css'
// ElMessageBox 通过服务方法调用，不会由模板组件的按需解析自动加载样式。
import 'element-plus/es/components/message-box/style/css'
import './style.css'
import { initializeTheme } from './theme'
import { installBackendHealthMonitor } from './backendHealth'

/** SPA 启动时同步根节点语言状态，再挂载共享 Web 应用。 */
document.documentElement.lang = i18n.global.locale.value
document.title = i18n.global.t('common.brand')
initializeTheme()
const app = createApp(App)
app.use(createPinia())
app.use(i18n)
app.use(router)
app.provide('setLocale', setLocale)
installBackendHealthMonitor(router)
app.mount('#app')
