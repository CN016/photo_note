<script setup lang="ts">
import { computed, defineComponent, h, nextTick, ref, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import en from 'element-plus/es/locale/lang/en'
import { clearGallerySnapshot } from './context-menu/galleryNavigation'
import GlobalContextMenu from './components/GlobalContextMenu.vue'
import FeedbackWidget from './components/FeedbackWidget.vue'
import GlobalMessageButton from './components/GlobalMessageButton.vue'
import StartupRecovery from './components/StartupRecovery.vue'
import AppHeader from './components/AppHeader.vue'
import { useAuthStore } from './stores/auth'
import { configureSpaceCoordination, preparingSpaceSwitch } from './spaces/coordination'
import { router, defaultAuthenticatedRoute } from './router'
import { beforeRouteEnter, enterRoute, leaveRoute, resetNavigationMotion } from './motion/navigationMotion'

import { isAndroid } from '../../../packages/platform-adapters'

// 禁止安卓容器将整页和导航一起放大，主体通过 ContentZoom 独立处理两指手势。
const scopedZoom = isAndroid()
const viewport = document.querySelector<HTMLMetaElement>('meta[name="viewport"]')
const originalViewport = viewport?.content
if (scopedZoom) {
  document.documentElement.classList.add('native-content-zoom')
  // Gecko 的原生视口手势可能抢占网页双指事件；只在 APK 内关闭整页缩放，主体仍提供 1～3 倍缩放。
  if (viewport) viewport.content = 'width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no, viewport-fit=cover'
}
onBeforeUnmount(() => {
  document.documentElement.classList.remove('native-content-zoom')
  if (scopedZoom && viewport && originalViewport !== undefined) viewport.content = originalViewport
})
const { locale, t } = useI18n()
const route = useRoute()
const auth = useAuthStore()
// 仅缓存图库路由框；账号、空间或会话变化会销毁整个缓存，私有图片不跨范围复用。
const galleryScope = computed(() => `${auth.sessionID}:${auth.orgid}:${auth.sessionVersion}:${auth.user?.business_id ?? ''}`)
// 进入上传或其他业务页时释放图库，下次进入重新查询，避免长期缓存掩盖新增和删除。
const cachedGallery = computed(() => ['gallery', 'edit-inspiration', 'inspiration-statistics'].includes(String(route.name)) || (route.name === 'project-editor' && route.query.from === 'gallery') ? 'GalleryRouteFrame' : '')
const GalleryRouteFrame = defineComponent({ name: 'GalleryRouteFrame', setup: (_, { slots }) => () => h('div', { class: 'route-page-frame' }, slots.default?.()) })
const RoutePageFrame = defineComponent({ name: 'RoutePageFrame', setup: (_, { slots }) => () => h('div', { class: 'route-page-frame' }, slots.default?.()) })
const pageAnnouncement = ref('')
watch(() => `${auth.sessionID}:${auth.orgid}:${auth.sessionVersion}:${auth.user?.business_id ?? ''}`, () => {
  clearGallerySnapshot()
  resetNavigationMotion()
}, { flush: 'sync' })
// 等待首个导航完成后再处理失效会话，避免初始路由尚未解析时覆盖公开分享页。
watch(() => [auth.ready, auth.user] as const, async ([ready, user]) => {
  if (!ready || user) return
  await router.isReady()
  if (auth.ready && !auth.user && !router.currentRoute.value.meta.public) await router.replace('/login')
})
configureSpaceCoordination({
  prepare: async () => {
    await router.push('/spaces')
    await nextTick()
    if (router.currentRoute.value.path !== '/spaces') return false
    // 必须确认新页面已挂载，不能仅以路由地址变化作为旧编辑器已卸载的证明。
    const deadline = Date.now() + 30000
    while (!document.querySelector('[data-space-ready]')) {
      if (Date.now() >= deadline || router.currentRoute.value.path !== '/spaces') return false
      await new Promise(resolve => window.setTimeout(resolve, 16))
    }
    return true
  },
  committed: async () => { await auth.refreshProfile(); await router.replace(defaultAuthenticatedRoute()) },
  loggedOut: async () => { auth.user = null; await router.replace('/login') },
})
const elementLocale = computed(() => locale.value === 'en-US' ? en : zhCn)
const projectEditorRoute = computed(() => route.name === 'project-editor')
watch(projectEditorRoute, active => document.body.classList.toggle('project-editor-route', active), { immediate: true })

/** 路由完成后用当前页面主标题更新文档标题，并向读屏器播报一次导航结果。 */
watch(() => route.fullPath, async () => {
  await nextTick()
  // 移动端会隐藏页面标题区，但 textContent 仍可用于文档标题和读屏路由播报。
  const heading = document.querySelector<HTMLElement>('main h1')?.textContent?.trim() ?? ''
  pageAnnouncement.value = heading || document.title
  document.title = heading ? `${heading} · ${t('common.brand')}` : t('common.brand')
}, { immediate: true, flush: 'post' })
</script>

<template>
  <el-config-provider :locale="elementLocale">
    <a class="skip-link" href="#main-content">{{ t('common.skipToContent') }}</a>
    <p class="sr-only route-announcer" role="status" aria-live="polite" aria-atomic="true">{{ pageAnnouncement }}</p>
    <StartupRecovery />
    <GlobalMessageButton v-if="!route.meta.standalone" />
    <AppHeader v-if="auth.user && !route.meta.public && !route.meta.standalone" />
    <GlobalContextMenu v-if="!route.meta.standalone" />
    <RouterView v-slot="{ Component, route: pageRoute }">
      <Transition
        :css="false"
        @before-enter="beforeRouteEnter"
        @enter="enterRoute"
        @leave="leaveRoute"
      >
        <KeepAlive :key="galleryScope" :include="cachedGallery" :max="1">
          <component
            :is="pageRoute.name === 'gallery' ? GalleryRouteFrame : RoutePageFrame"
            v-if="pageRoute.meta.public || auth.user"
            :key="`${galleryScope}:${pageRoute.path}`"
          >
            <component :is="Component" />
          </component>
        </KeepAlive>
      </Transition>
    </RouterView>
    <FeedbackWidget v-if="route.name && !route.meta.standalone" />
    <div v-if="preparingSpaceSwitch" class="space-switch-overlay" role="status" aria-live="polite">{{ $t('spaces.preparing') }}</div>
  </el-config-provider>
</template>

<style scoped>
.space-switch-overlay { position: fixed; inset: 0; z-index: 10000; display: grid; place-items: center; background: var(--el-mask-color); color: var(--el-text-color-primary); padding: 32px; text-align: center; }
</style>
