import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from './stores/auth'
import { checkBackendAvailability } from './backendHealth'

/** 页面边界按账号、灵感图列表和编辑工作区拆分，适合后续壳应用复用。 */
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/inspirations' },
    { path: '/login', name: 'login', component: () => import('./views/LoginPage.vue'), meta: { public: true } },
    { path: '/register', name: 'register', component: () => import('./views/RegisterPage.vue'), meta: { public: true } },
    { path: '/maintenance', name: 'maintenance', component: () => import('./views/BackendMaintenancePage.vue'), meta: { public: true, maintenance: true } },
    { path: '/inspirations', name: 'gallery', component: () => import('./views/GalleryPage.vue') },
    { path: '/inspirations/new', name: 'new-inspiration', component: () => import('./views/EditorPage.vue') },
    { path: '/inspirations/:id', name: 'edit-inspiration', component: () => import('./views/EditorPage.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/inspirations' },
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
  if (to.meta.public && auth.user) return '/inspirations'
  if (!to.meta.public && !auth.user) return '/login'
  return true
})
