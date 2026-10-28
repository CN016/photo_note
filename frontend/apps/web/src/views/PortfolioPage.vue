<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, UploadFilled } from '@element-plus/icons-vue'
import QRCode from 'qrcode'
import { portfolioPublicBase } from './portfolioPublicBase'
import AppHeader from '../components/AppHeader.vue'
import ContentZoom from '../components/ContentZoom.vue'
import PortfolioGallery from '../components/PortfolioGallery.vue'
import { api } from '../api/client'
import type { InspirationRecord, PortfolioInput, PortfolioPhoto, PortfolioRecord, ProjectSummary, TagRecord } from '../api/types'
import { useAuthStore } from '../stores/auth'
const { t } = useI18n()
const route = useRoute(); const router = useRouter(); const auth = useAuthStore()
const id = computed(() => typeof route.params.id === 'string' ? route.params.id : '')
const portfolios = ref<PortfolioRecord[]>([]); const current = ref<PortfolioRecord | null>(null); const photos = ref<PortfolioPhoto[]>([])
const tags = ref<TagRecord[]>([]); const projects = ref<ProjectSummary[]>([]); const inspirations = ref<InspirationRecord[]>([])
const portfolioTag = ref(''); const portfolioSearch = ref('')
const filteredPortfolios = computed(() => portfolios.value.filter(item => (!portfolioTag.value || item.tag_ids.includes(portfolioTag.value)) && (!portfolioSearch.value.trim() || `${item.title} ${item.description}`.toLocaleLowerCase().includes(portfolioSearch.value.trim().toLocaleLowerCase()))))
const loading = ref(false); const busy = ref(false); const error = ref(''); const editOpen = ref(false); const shareOpen = ref(false); const copyOpen = ref(false)
const newTag = ref(''); const tagBusy = ref(false)
const link = ref(''); const qr = ref(''); const selectedInspirations = ref<string[]>([]); const inspirationQuery = ref(''); const inspirationPage = ref(1); const inspirationTotal = ref(0)
const form = ref<PortfolioInput>({ title: '', description: '', tag_ids: [], project_ids: [] }); const fileInput = ref<HTMLInputElement | null>(null)
let generation = 0
/** 在切换空间或路由后丢弃迟到响应，避免跨空间显示。 */
async function load() {
 const version=++generation; loading.value=true; error.value=''; current.value=null; photos.value=[]; link.value=''; qr.value=''
 try {
  const fetchAll = async <T,>(fetchPage: (page: number) => Promise<{ items:T[]; total:number }>): Promise<T[]> => {
   const first=await fetchPage(1);const results=[...first.items];for(let page=2;results.length<first.total;page++){const batch=await fetchPage(page);if(!batch.items.length)break;results.push(...batch.items)}return results
  }
  const [tagResult, projectResult] = await Promise.all([fetchAll(page=>api.tags(page,100)),fetchAll(page=>api.projects(page,100))])
  if(version!==generation)return; tags.value=tagResult; projects.value=projectResult
  if(id.value) {
   const [record, pictures, links]=await Promise.all([api.portfolio(id.value),api.portfolioPhotos(id.value),api.portfolioProjects(id.value)])
   if(version!==generation)return; current.value=record; photos.value=pictures.items
   form.value={title:record.title,description:record.description,tag_ids:[...record.tag_ids],project_ids:links.items}
  } else { const result=await api.portfolios(); if(version!==generation)return; portfolios.value=result.items }
 } catch { if(version===generation)error.value=t('portfolio.error') } finally { if(version===generation)loading.value=false }
}
watch(() => [id.value,auth.orgid,auth.user?.business_id,auth.sessionVersion],()=>{void load()})
onMounted(()=>{void load()})
/** 新建时清空当前空间内的表单草稿。 */
function newPortfolio(){newTag.value='';form.value={title:'',description:'',tag_ids:[],project_ids:[]};editOpen.value=true}
/** 校验标题并保存新建或编辑内容。 */
async function save(){
 if(!form.value.title.trim()){ElMessage.warning(t('portfolio.newName'));return}
 busy.value=true;try { if(id.value){await api.savePortfolio(id.value,form.value); editOpen.value=false; await load()} else {const item=await api.createPortfolio(form.value);editOpen.value=false;await router.push(`/portfolios/${item.business_id}`)} }
 catch{ElMessage.error(t('portfolio.error'))}finally{busy.value=false}
}
/** 创建当前空间的共享标签，并立即加入作品集表单。 */
async function addTag() {
 const content=newTag.value.trim(); if(!content)return
 const existing=tags.value.find(tag=>tag.content===content)
 if(existing){if(!form.value.tag_ids.includes(existing.business_id))form.value.tag_ids.push(existing.business_id);newTag.value='';return}
 tagBusy.value=true
 try {const tag=await api.createTag(content);tags.value.push(tag);form.value.tag_ids.push(tag.business_id);newTag.value=''} catch {ElMessage.error(t('portfolio.error'))} finally {tagBusy.value=false}
}
/** 删除前明确提示公开文件也将被清理。 */
async function removePortfolio(){try{await ElMessageBox.confirm(t('portfolio.confirmDelete'),t('portfolio.delete'),{type:'warning'});busy.value=true;await api.deletePortfolio(id.value);await router.push('/portfolios')}catch(e){if(e!=='cancel'&&e!=='close')ElMessage.error(t('portfolio.error'))}finally{busy.value=false}}
/** 逐张上传并在空间切换后停止剩余任务。 */
async function upload(event: Event){const input=event.target as HTMLInputElement;const files=Array.from(input.files??[]);input.value='';if(!files.length)return;const batchID=id.value;const batchGeneration=generation;busy.value=true;let failures=0;for(const file of files){if(generation!==batchGeneration)break;try{await api.uploadPortfolioPhoto(batchID,file)}catch{failures++}};if(failures)ElMessage.error(t('portfolio.uploadError'));await load();busy.value=false}
/** 保存照片的标题、标签及展示顺序。 */
async function savePhoto(photo:PortfolioPhoto){busy.value=true;try{await api.updatePortfolioPhoto(id.value,photo);await load()}catch{ElMessage.error(t('portfolio.error'))}finally{busy.value=false}}
/** 删除前请求确认；失败时保留作品并提示重试。 */
async function removePhoto(photo:PortfolioPhoto){try{await ElMessageBox.confirm(t('portfolio.confirmPhoto'),t('portfolio.removePhoto'),{type:'warning'});busy.value=true;await api.deletePortfolioPhoto(id.value,photo.business_id);await load()}catch(e){if(e!=='cancel'&&e!=='close')ElMessage.error(t('portfolio.error'))}finally{busy.value=false}}
/** 搜索本人灵感库并按后端分页读取。 */
async function loadInspirations(page=1){try{const data=await api.inspirations(page,20,{q:inspirationQuery.value});inspirations.value=data.items;inspirationPage.value=data.page;inspirationTotal.value=data.total}catch{ElMessage.error(t('portfolio.error'))}}
/** 使用独立公开副本复制所选图片，切换空间后停止后续请求。 */
async function copyPhotos(){const batchID=id.value;const batchGeneration=generation;busy.value=true;let failures=0;for(const photo of selectedInspirations.value){if(generation!==batchGeneration)break;try{await api.copyPortfolioPhoto(batchID,photo)}catch{failures++}};selectedInspirations.value=[];copyOpen.value=false;if(failures)ElMessage.error(t('portfolio.error'));else ElMessage.success(t('portfolio.copySuccess'));await load();busy.value=false}
/** 先验证公开站点地址，再生成仅本次可见的令牌和二维码。 */
async function createShare(){try{const base = portfolioPublicBase(import.meta.env.VITE_PUBLIC_WEB_BASE_URL, import.meta.env.VITE_ANDROID_WEB_UPDATE_URL, location.origin)
 if (!base) { ElMessage.error(t('portfolio.shareBaseMissing')); return }
 if(current.value?.share_enabled)await ElMessageBox.confirm(t('portfolio.shareConfirm'),t('portfolio.shareReset'),{type:'warning'});busy.value=true;const data=await api.createPortfolioShare(id.value);link.value=new URL(`p/${data.token}`,base).href;qr.value=await QRCode.toDataURL(link.value,{width:240,margin:2});if(current.value)current.value.share_enabled=true}catch(e){if(e!=='cancel'&&e!=='close')ElMessage.error(t('portfolio.error'))}finally{busy.value=false}}
/** 撤销作品集分享，同时提示用户已知照片直链仍可访问。 */
async function revokeShare(){busy.value=true;try{await api.revokePortfolioShare(id.value);link.value='';qr.value='';if(current.value)current.value.share_enabled=false}catch{ElMessage.error(t('portfolio.error'))}finally{busy.value=false}}
/** 将当前分享地址写入剪贴板并反馈结果。 */
async function copyLink(){try{await navigator.clipboard.writeText(link.value);ElMessage.success(t('portfolio.copied'))}catch{ElMessage.error(t('portfolio.error'))}}
</script>
<template>
 <div class="workspace-page"><AppHeader /><ContentZoom><main class="portfolio-main">
  <header class="portfolio-header"><div><RouterLink v-if="id" to="/portfolios">{{ t('portfolio.back') }}</RouterLink><h1>{{ current?.title || t('portfolio.title') }}</h1><p v-if="current">{{ current.description }}</p></div>
   <div class="portfolio-actions"><el-button v-if="!id" type="primary" @click="newPortfolio"><el-icon><Plus /></el-icon>{{ t('portfolio.new') }}</el-button><template v-else-if="current"><el-button @click="newTag='';editOpen=true">{{ t('portfolio.edit') }}</el-button><el-button @click="shareOpen=true">{{ t('portfolio.share') }}</el-button><el-button type="danger" plain @click="removePortfolio">{{ t('portfolio.delete') }}</el-button></template></div>
  </header>
  <p v-if="loading" role="status">{{ t('portfolio.loading') }}</p><p v-else-if="error" role="alert">{{ error }}</p>
  <template v-else-if="!id"><div class="portfolio-actions"><el-input v-model="portfolioSearch" :placeholder="t('portfolio.searchPortfolio')" clearable class="portfolio-search" /><el-select v-model="portfolioTag" :aria-label="t('portfolio.filter')"><el-option :label="t('portfolio.all')" value=""/><el-option v-for="tag in tags" :key="tag.business_id" :label="tag.content" :value="tag.business_id" /></el-select></div><p v-if="!filteredPortfolios.length">{{ portfolios.length ? t('portfolio.noResults') : t('portfolio.empty') }}</p><div class="portfolio-list"><RouterLink v-for="item in filteredPortfolios" :key="item.business_id" :to="`/portfolios/${item.business_id}`" class="portfolio-card"><img v-if="item.cover_image_key" :src="api.portfolioPhotoURL(item.cover_image_key)" :alt="item.title" loading="lazy"/><strong>{{ item.title }}</strong><span>{{ item.description }}</span><span>{{ item.tag_ids.map(id => tags.find(tag => tag.business_id === id)?.content).filter(Boolean).join(' · ') }}</span></RouterLink></div></template>
  <template v-else-if="current"><div class="portfolio-actions" :aria-label="t('portfolio.tags')"><el-tag v-for="tag in tags.filter(item => current?.tag_ids.includes(item.business_id))" :key="tag.business_id" effect="plain">{{ tag.content }}</el-tag></div><p>{{ t('portfolio.privacy') }}</p><nav v-if="form.project_ids.length" class="portfolio-projects"><RouterLink v-for="project in form.project_ids" :key="project" :to="`/projects/${project}`">{{ projects.find(item => item.business_id === project)?.title || t('portfolio.projectLink') }}</RouterLink></nav>
   <div class="portfolio-actions"><el-button type="primary" :loading="busy" @click="fileInput?.click()"><el-icon><UploadFilled /></el-icon>{{ t('portfolio.upload') }}</el-button><input ref="fileInput" type="file" accept="image/*" multiple hidden @change="upload" /><el-button :disabled="busy" @click="copyOpen=true;loadInspirations()">{{ t('portfolio.copy') }}</el-button></div>
   <PortfolioGallery :photos="photos" :tags="tags" namespace="portfolio" editable :busy="busy" @save="savePhoto" @remove="removePhoto" />
  </template>
  <el-dialog v-model="editOpen" :title="id ? t('portfolio.edit') : t('portfolio.new')" width="min(620px, 94vw)"><el-form label-position="top" @submit.prevent><el-form-item :label="t('portfolio.name')"><el-input v-model="form.title" maxlength="120" /></el-form-item><el-form-item :label="t('portfolio.description')"><el-input v-model="form.description" type="textarea" maxlength="5000" /></el-form-item><el-form-item :label="t('portfolio.tags')"><el-select v-model="form.tag_ids" multiple filterable><el-option v-for="tag in tags" :key="tag.business_id" :label="tag.content" :value="tag.business_id" /></el-select><div class="portfolio-tag-create"><el-input v-model="newTag" maxlength="64" :placeholder="t('portfolio.newTagPlaceholder')" @keydown.enter.prevent="addTag" /><el-button :loading="tagBusy" :disabled="!newTag.trim()" @click="addTag">{{ t('portfolio.addTag') }}</el-button></div></el-form-item><el-form-item v-if="id" :label="t('portfolio.projects')"><el-select v-model="form.project_ids" multiple filterable><el-option v-for="project in projects" :key="project.business_id" :label="project.title" :value="project.business_id" /></el-select></el-form-item></el-form><template #footer><el-button @click="editOpen=false">{{ t('portfolio.cancel') }}</el-button><el-button type="primary" :loading="busy" @click="save">{{ t('portfolio.save') }}</el-button></template></el-dialog>
  <el-dialog v-model="shareOpen" :title="t('portfolio.share')" width="min(480px, 94vw)"><p>{{ t('portfolio.shareHint') }}</p><p>{{ t('portfolio.privacy') }}</p><el-button type="primary" :loading="busy" @click="createShare">{{ current?.share_enabled ? t('portfolio.shareReset') : t('portfolio.shareOn') }}</el-button><el-button v-if="current?.share_enabled" :disabled="busy" @click="revokeShare">{{ t('portfolio.shareOff') }}</el-button><div v-if="link" class="portfolio-share"><img :src="qr" :alt="t('portfolio.shareLink')" /><label>{{ t('portfolio.shareLink') }}<el-input :model-value="link" readonly /></label><el-button @click="copyLink">{{ t('portfolio.copyLink') }}</el-button></div></el-dialog>
  <el-dialog v-model="copyOpen" :title="t('portfolio.copy')" width="min(620px, 94vw)"><el-input v-model="inspirationQuery" :placeholder="t('portfolio.searchInspiration')" @keyup.enter="loadInspirations(1)" /><el-button @click="loadInspirations(1)">{{ t('portfolio.searchInspiration') }}</el-button><el-checkbox-group v-model="selectedInspirations" class="portfolio-inspirations"><el-checkbox v-for="item in inspirations" :key="item.business_id" :value="item.business_id">{{ item.file_name }}</el-checkbox></el-checkbox-group><el-pagination :current-page="inspirationPage" :page-size="20" :total="inspirationTotal" layout="prev, pager, next" @current-change="loadInspirations" /><template #footer><el-button type="primary" :disabled="!selectedInspirations.length" :loading="busy" @click="copyPhotos">{{ t('portfolio.copySelected') }}</el-button></template></el-dialog>
 </main></ContentZoom></div>
</template>
<style scoped>
.portfolio-main { max-width:1200px; margin:auto; padding:28px; } .portfolio-header { display:flex; justify-content:space-between; align-items:start; gap:20px; flex-wrap:wrap; } h1 { margin:8px 0; } .portfolio-header p { white-space:pre-wrap; overflow-wrap:anywhere; } .portfolio-search { max-width:280px; } .portfolio-actions .el-select { max-width:220px; } .portfolio-actions,.portfolio-projects { display:flex; gap:10px; flex-wrap:wrap; margin:16px 0; } .portfolio-list { display:grid; grid-template-columns:repeat(auto-fill,minmax(210px,1fr)); gap:14px; } .portfolio-card { display:grid; gap:8px; min-height:120px; padding:18px; border:1px solid var(--el-border-color); border-radius:12px; color:var(--el-text-color-primary); text-decoration:none; overflow-wrap:anywhere; } .portfolio-card img { display:block; width:100%; aspect-ratio: 4/3; object-fit:cover; border-radius:8px; } .portfolio-card:hover { border-color:var(--el-color-primary); } .portfolio-tag-create { display:flex; gap:8px; width:100%; margin-top:8px; } .portfolio-share { display:grid; justify-items:start; gap:12px; margin-top:20px; } .portfolio-share img { width:200px; } .portfolio-share label { display:grid; gap:6px; width:100%; } .portfolio-inspirations { display:grid; gap:8px; max-height:45vh; overflow:auto; margin:16px 0; } @media(max-width:600px){.portfolio-main{padding:16px}.portfolio-actions .el-button{margin:0}}
</style>
