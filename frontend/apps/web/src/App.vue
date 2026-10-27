<script setup lang="ts">
import { computed, nextTick, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import en from 'element-plus/es/locale/lang/en'
import FeedbackWidget from './components/FeedbackWidget.vue'
import GlobalMessageButton from './components/GlobalMessageButton.vue'
import { useAuthStore } from './stores/auth'
import { configureSpaceCoordination, preparingSpaceSwitch } from './spaces/coordination'
import { router, defaultAuthenticatedRoute } from './router'

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
const { locale } = useI18n()
const route = useRoute()
const auth = useAuthStore()
watch(() => [auth.ready, auth.user] as const, ([ready, user]) => { if (ready && !user && !router.currentRoute.value.meta.public) void router.replace('/login') })
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
const editorRoute = computed(() => route.name === 'new-inspiration' || route.name === 'edit-inspiration' || route.name === 'project-editor')
const projectEditorRoute = computed(() => route.name === 'project-editor')
/** 登录后的主要页面共用移动底部导航；游客预约页只保留页面内容与反馈入口。 */
const hasMobileNav = computed(() => ['workbench', 'gallery', 'inspiration-statistics', 'new-inspiration', 'edit-inspiration', 'ledger', 'schedule', 'plans', 'projects', 'project-editor', 'messages', 'profile', 'atelier-console'].includes(String(route.name)))
watch(projectEditorRoute, active => document.body.classList.toggle('project-editor-route', active), { immediate: true })
</script>

<template>
  <el-config-provider :locale="elementLocale">
    <GlobalMessageButton />
    <RouterView v-slot="{ Component, route: pageRoute }">
      <component :is="Component" v-if="pageRoute.meta.public || auth.user" :key="`${auth.sessionID}:${auth.orgid}:${auth.sessionVersion}:${pageRoute.path}`" />
    </RouterView>
    <FeedbackWidget :editor-route="editorRoute" :has-mobile-nav="hasMobileNav" />
    <div v-if="preparingSpaceSwitch" class="space-switch-overlay" role="status" aria-live="polite">{{ $t('spaces.preparing') }}</div>
  </el-config-provider>
</template>

<style scoped>
.space-switch-overlay { position: fixed; inset: 0; z-index: 10000; display: grid; place-items: center; background: var(--el-mask-color); color: var(--el-text-color-primary); padding: 32px; text-align: center; }
</style>
