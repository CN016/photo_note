<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import en from 'element-plus/es/locale/lang/en'
import FeedbackWidget from './components/FeedbackWidget.vue'
import GlobalMessageButton from './components/GlobalMessageButton.vue'

const { locale } = useI18n()
const route = useRoute()
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
      <Transition name="page" mode="out-in">
        <component :is="Component" :key="pageRoute.path" />
      </Transition>
    </RouterView>
    <FeedbackWidget :editor-route="editorRoute" :has-mobile-nav="hasMobileNav" />
  </el-config-provider>
</template>
