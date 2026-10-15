<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import en from 'element-plus/es/locale/lang/en'
import FeedbackWidget from './components/FeedbackWidget.vue'

const { locale } = useI18n()
const route = useRoute()
const elementLocale = computed(() => locale.value === 'en-US' ? en : zhCn)
const editorRoute = computed(() => /^\/inspirations\/(new|[^/]+)$/.test(route.path))
/** 图库、编辑器、档期和个人中心使用移动底部导航；游客预约页只保留页面内容与反馈入口。 */
const hasMobileNav = computed(() => ['gallery', 'new-inspiration', 'edit-inspiration', 'schedule', 'profile'].includes(String(route.name)))
</script>

<template>
  <el-config-provider :locale="elementLocale">
    <RouterView v-slot="{ Component, route: pageRoute }">
      <Transition name="page" mode="out-in">
        <component :is="Component" :key="pageRoute.path" />
      </Transition>
    </RouterView>
    <FeedbackWidget :editor-route="editorRoute" :has-mobile-nav="hasMobileNav" />
  </el-config-provider>
</template>
