import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from './stores/auth'
import { checkBackendAvailability } from './backendHealth'
import { api } from './api/client'

/** 根据当前设备宽度选择登录后的默认页面，桌面入口继续落在原图库页。 */
export function defaultAuthenticatedRoute(): string {
  const isMobile = typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(max-width: 768px)').matches
  return isMobile ? '/workbench' : '/inspirations'
}

/** 页面边界按账号、灵感图列表、批量新增和编辑工作区拆分，适合后续壳应用复用。 */
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: defaultAuthenticatedRoute },
    { path: '/login', name: 'login', component: () => import('./views/LoginPage.vue'), meta: { public: true } },
    { path: '/register', name: 'register', component: () => import('./views/RegisterPage.vue'), meta: { public: true } },
    { path: '/maintenance', name: 'maintenance', component: () => import('./views/BackendMaintenancePage.vue'), meta: { public: true, maintenance: true } },
    { path: '/book/:shareToken', name: 'public-booking', component: () => import('./views/PublicBookingPage.vue'), meta: { public: true } },
    { path: '/workbench', name: 'workbench', component: () => import('./views/WorkbenchPage.vue') },
    { path: '/inspirations', name: 'gallery', component: () => import('./views/GalleryPage.vue') },
    { path: '/inspirations/statistics', name: 'inspiration-statistics', component: () => import('./views/InspirationStatisticsPage.vue') },
    { path: '/inspirations/new', name: 'new-inspiration', component: () => import('./views/InspirationUploadPage.vue') },
    { path: '/inspirations/:id', name: 'edit-inspiration', component: () => import('./views/EditorPage.vue') },
    { path: '/ledger', name: 'ledger', component: () => import('./views/LedgerPage.vue') },
    { path: '/schedule', name: 'schedule', component: () => import('./views/SchedulePage.vue') },
    { path: '/plans', name: 'plans', component: () => import('./views/PlansPage.vue') },
    { path: '/projects', name: 'projects', component: () => import('./views/ProjectsPage.vue') },
    { path: '/projects/:id', name: 'project-editor', component: () => import('./views/ProjectEditorPage.vue') },
    { path: '/messages', name: 'messages', component: () => import('./views/MessagesPage.vue') },
    { path: '/profile', name: 'profile', component: () => import('./views/ProfilePage.vue') },
    { path: '/atelier-7f3c9a2e', name: 'atelier-console', component: () => import('./views/ConsolePage.vue') },
    { path: '/:pathMatch(.*)*', redirect: defaultAuthenticatedRoute },
  ],
})

/** 导航前确认服务可达并恢复登录态，避免离线时进入需要数据的页面。 */
router.beforeEach(async to => {
  const backendAvailable = await checkBackendAvailability()
  if (!backendAvailable && to.name !== 'maintenance') {
    if (to.fullPath && to.fullPath !== '/maintenance') sessionStorage.setItem('photo-nini.maintenance-return-path', to.fullPath)
    return { name: 'maintenance', replace: true }
  }
  if (backendAvailable && to.name === 'maintenance') {
    const previous = sessionStorage.getItem('photo-nini.maintenance-return-path')
    sessionStorage.removeItem('photo-nini.maintenance-return-path')
    return previous?.startsWith('/') && !previous.startsWith('//') && previous !== '/maintenance' ? previous : '/login'
  }
  if (!backendAvailable && to.name === 'maintenance') return true
  const auth = useAuthStore()
  await auth.initialize()
  if (to.meta.public && auth.user && to.name !== 'public-booking') return defaultAuthenticatedRoute()
  if (!to.meta.public && !auth.user) return '/login'
  if (to.name === 'atelier-console') {
    try {
      auth.user = await api.currentUser()
    } catch {
      return auth.user ? defaultAuthenticatedRoute() : '/login'
    }
    if (!auth.user.is_admin) return defaultAuthenticatedRoute()
  }
  return true
})
