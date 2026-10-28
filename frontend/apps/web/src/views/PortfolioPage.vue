<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, UploadFilled, Picture, Search, Download } from '@element-plus/icons-vue'
import QRCode from 'qrcode'
import { portfolioPublicBase } from './portfolioPublicBase'
import AppHeader from '../components/AppHeader.vue'
import ContentZoom from '../components/ContentZoom.vue'
import InspirationThumbnail from '../components/InspirationThumbnail.vue'
import PortfolioGallery from '../components/PortfolioGallery.vue'
import { api } from '../api/client'
import { exportFile, FileExportError } from '../platform'
import type { InspirationRecord, PortfolioInput, PortfolioPhoto, PortfolioRecord, ProjectSummary, TagRecord } from '../api/types'
import { useAuthStore } from '../stores/auth'
const { t } = useI18n()
const route = useRoute(); const router = useRouter(); const auth = useAuthStore()
const id = computed(() => typeof route.params.id === 'string' ? route.params.id : '')
const portfolios = ref<PortfolioRecord[]>([]); const current = ref<PortfolioRecord | null>(null); const photos = ref<PortfolioPhoto[]>([])
const tags = ref<TagRecord[]>([]); const projects = ref<ProjectSummary[]>([]); const inspirations = ref<InspirationRecord[]>([])
/** 单独保存已持久化的关联，避免取消编辑后将草稿当作当前关联。 */
const savedProjectIDs = ref<string[]>([])
const portfolioTag = ref(''); const portfolioSearch = ref('')
const filteredPortfolios = computed(() => portfolios.value.filter(item => (!portfolioTag.value || item.tag_ids.includes(portfolioTag.value)) && (!portfolioSearch.value.trim() || `${item.title} ${item.description}`.toLocaleLowerCase().includes(portfolioSearch.value.trim().toLocaleLowerCase()))))
const loading = ref(false); const busy = ref(false); const error = ref(''); const editOpen = ref(false); const shareOpen = ref(false); const copyOpen = ref(false)
const downloadingQR = ref(false)
const inspirationLoading = ref(false)
let inspirationGeneration = 0
const newTag = ref(''); const tagBusy = ref(false)
const link = ref(''); const qr = ref(''); const selectedInspirations = ref<string[]>([]); const inspirationQuery = ref(''); const inspirationPage = ref(1); const inspirationTotal = ref(0)
const form = ref<PortfolioInput>({ title: '', description: '', tag_ids: [], project_ids: [] }); const fileInput = ref<HTMLInputElement | null>(null)
let generation = 0
/** 在切换空间或路由后丢弃迟到响应，避免跨空间显示。 */
async function load() {
 const version=++generation; inspirationGeneration++; editOpen.value=false; copyOpen.value=false; shareOpen.value=false; selectedInspirations.value=[]; inspirations.value=[]; loading.value=true; error.value=''; current.value=null; photos.value=[]; link.value=''; qr.value=''
 try {
  const fetchAll = async <T,>(fetchPage: (page: number) => Promise<{ items:T[]; total:number }>): Promise<T[]> => {
   const first=await fetchPage(1);const results=[...first.items];for(let page=2;results.length<first.total;page++){const batch=await fetchPage(page);if(!batch.items.length)break;results.push(...batch.items)}return results
  }
  const [tagResult, projectResult] = await Promise.all([fetchAll(page=>api.tags(page,100)),fetchAll(page=>api.projects(page,100))])
  if(version!==generation)return; tags.value=tagResult; projects.value=projectResult
  if(id.value) {
   const [record, pictures, links]=await Promise.all([api.portfolio(id.value),api.portfolioPhotos(id.value),api.portfolioProjects(id.value)])
   if(version!==generation)return; current.value=record; photos.value=pictures.items; savedProjectIDs.value=[...links.items]
   form.value={title:record.title,description:record.description,tag_ids:[...record.tag_ids],project_ids:links.items}
  } else { const result=await api.portfolios(); if(version!==generation)return; portfolios.value=result.items }
 } catch { if(version===generation)error.value=t('portfolio.error') } finally { if(version===generation)loading.value=false }
}
watch(() => [id.value,auth.orgid,auth.user?.business_id,auth.sessionVersion],()=>{void load()})
onMounted(()=>{void load()})
/** 新建时清空当前空间内的表单草稿。 */
function newPortfolio(){newTag.value='';form.value={title:'',description:'',tag_ids:[],project_ids:[]};editOpen.value=true}
/** 每次打开编辑均从已保存记录恢复，取消的草稿不影响后续编辑。 */
function editPortfolio(){if(!current.value)return;newTag.value='';form.value={...form.value,title:current.value.title,description:current.value.description,tag_ids:[...current.value.tag_ids],project_ids:[...savedProjectIDs.value]};editOpen.value=true}
/** 打开选图时清理上一次选择，分页搜索保留本次已选图片。 */
function openPicker(){selectedInspirations.value=[];inspirationQuery.value='';copyOpen.value=true;void loadInspirations()}
/** 复用平台导出接口：浏览器下载PNG，Android交给系统保存面板。 */
async function downloadQR(){
 if(!qr.value||downloadingQR.value)return
 downloadingQR.value=true
 try{const blob=await (await fetch(qr.value)).blob();await exportFile(blob,'portfolio-qr.png')}
 catch(cause){if(!(cause instanceof FileExportError))ElMessage.error(t('portfolio.error'))}
 finally{downloadingQR.value=false}
}
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
async function savePhoto(photo:PortfolioPhoto,done:()=>void){busy.value=true;try{await api.updatePortfolioPhoto(id.value,photo);done();await load()}catch{ElMessage.error(t('portfolio.error'))}finally{busy.value=false}}
/** 删除前请求确认；失败时保留作品并提示重试。 */
async function removePhoto(photo:PortfolioPhoto){try{await ElMessageBox.confirm(t('portfolio.confirmPhoto'),t('portfolio.removePhoto'),{type:'warning'});busy.value=true;await api.deletePortfolioPhoto(id.value,photo.business_id);await load()}catch(e){if(e!=='cancel'&&e!=='close')ElMessage.error(t('portfolio.error'))}finally{busy.value=false}}
/** 搜索本人灵感库并按后端分页读取。 */
async function loadInspirations(page=1){
 const version=++inspirationGeneration;const scope=generation;inspirationLoading.value=true
 try{const data=await api.inspirations(page,20,{q:inspirationQuery.value});if(version!==inspirationGeneration||scope!==generation)return;inspirations.value=data.items;inspirationPage.value=data.page;inspirationTotal.value=data.total}
 catch{if(version===inspirationGeneration&&scope===generation)ElMessage.error(t('portfolio.error'))}
 finally{if(version===inspirationGeneration)inspirationLoading.value=false}
}
/** 使用独立公开副本复制所选图片，切换空间后停止后续请求。 */
async function copyPhotos(){const batchID=id.value;const batchGeneration=generation;busy.value=true;let failures=0;for(const photo of selectedInspirations.value){if(generation!==batchGeneration)break;try{await api.copyPortfolioPhoto(batchID,photo)}catch{failures++}};selectedInspirations.value=[];copyOpen.value=false;if(failures)ElMessage.error(t('portfolio.error'));else ElMessage.success(t('portfolio.copySuccess'));await load();busy.value=false}
/** 先验证公开站点地址，再生成仅本次可见的令牌和二维码。 */
async function createShare(){
 const scope=generation;const portfolioID=id.value
 const base=portfolioPublicBase(import.meta.env.VITE_PUBLIC_WEB_BASE_URL,import.meta.env.VITE_ANDROID_WEB_UPDATE_URL,location.origin)
 if(!base){ElMessage.error(t('portfolio.shareBaseMissing'));return}
 busy.value=true
 try{
  if(current.value?.share_enabled)await ElMessageBox.confirm(t('portfolio.shareConfirm'),t('portfolio.shareReset'),{type:'warning'})
  if(scope!==generation)return
  const data=await api.createPortfolioShare(portfolioID)
  if(scope!==generation)return
  const nextLink=new URL(`p/${data.token}`,base).href
  const nextQR=await QRCode.toDataURL(nextLink,{width:960,margin:2})
  // 二维码编码也是异步步骤，路由或空间变化后不得回填旧作品集链接。
  if(scope!==generation)return
  link.value=nextLink;qr.value=nextQR
  if(current.value)current.value.share_enabled=true
 }catch(cause){if(scope===generation&&cause!=='cancel'&&cause!=='close')ElMessage.error(t('portfolio.error'))}
 finally{busy.value=false}
}
/** 撤销作品集分享并清理本次生成的链接和二维码。 */
async function revokeShare(){busy.value=true;try{await api.revokePortfolioShare(id.value);link.value='';qr.value='';if(current.value)current.value.share_enabled=false}catch{ElMessage.error(t('portfolio.error'))}finally{busy.value=false}}
/** 将当前分享地址写入剪贴板并反馈结果。 */
async function copyLink(){try{await navigator.clipboard.writeText(link.value);ElMessage.success(t('portfolio.copied'))}catch{ElMessage.error(t('portfolio.error'))}}
</script>
<template>
 <div class="workspace-page"><AppHeader /><ContentZoom><main class="portfolio-main">
  <header class="portfolio-header"><div><RouterLink v-if="id" to="/portfolios">{{ t('portfolio.back') }}</RouterLink><h1>{{ current?.title || t('portfolio.title') }}</h1><p v-if="current">{{ current.description }}</p></div>
   <div class="portfolio-actions"><el-button v-if="!id" type="primary" @click="newPortfolio"><el-icon><Plus /></el-icon>{{ t('portfolio.new') }}</el-button><template v-else-if="current"><el-button :disabled="busy" @click="editPortfolio">{{ t('portfolio.edit') }}</el-button><el-button type="primary" :disabled="busy" @click="shareOpen=true">{{ t('portfolio.share') }}</el-button><el-button :disabled="busy" @click="removePortfolio">{{ t('portfolio.delete') }}</el-button></template></div>
  </header>
  <el-skeleton v-if="loading" :rows="4" animated /><div v-else-if="error" role="alert"><p>{{ error }}</p><el-button @click="load">{{ t('portfolio.retry') }}</el-button></div>
  <template v-else-if="!id"><div class="portfolio-actions"><el-input v-model="portfolioSearch" :placeholder="t('portfolio.searchPortfolio')" :aria-label="t('portfolio.searchPortfolio')" :prefix-icon="Search" clearable class="portfolio-search" /><el-select v-model="portfolioTag" :placeholder="t('portfolio.all')" :aria-label="t('portfolio.filter')"><el-option :label="t('portfolio.all')" value=""/><el-option v-for="tag in tags" :key="tag.business_id" :label="tag.content" :value="tag.business_id" /></el-select></div><el-empty v-if="!filteredPortfolios.length" :description="portfolios.length ? t('portfolio.noPortfolioResults') : t('portfolio.empty')"><el-button v-if="!portfolios.length" type="primary" @click="newPortfolio">{{ t('portfolio.new') }}</el-button><el-button v-else @click="portfolioSearch='';portfolioTag=''">{{ t('portfolio.clearFilters') }}</el-button></el-empty><div class="portfolio-list"><RouterLink v-for="item in filteredPortfolios" :key="item.business_id" :to="`/portfolios/${item.business_id}`" class="portfolio-card"><img v-if="item.cover_image_key" :src="api.portfolioPhotoURL(item.cover_image_key)" :alt="item.title" loading="lazy"/><div v-else class="portfolio-cover-placeholder"><el-icon><Picture /></el-icon></div><strong>{{ item.title }}</strong><span>{{ item.description }}</span><span>{{ item.tag_ids.map(id => tags.find(tag => tag.business_id === id)?.content).filter(Boolean).join(' · ') }}</span></RouterLink></div></template>
  <template v-else-if="current"><div v-if="current.tag_ids.length" class="portfolio-actions" :aria-label="t('portfolio.tags')"><el-tag v-for="tag in tags.filter(item => current?.tag_ids.includes(item.business_id))" :key="tag.business_id" effect="plain">{{ tag.content }}</el-tag></div><nav v-if="savedProjectIDs.length" class="portfolio-projects"><RouterLink v-for="project in savedProjectIDs" :key="project" :to="`/projects/${project}`">{{ projects.find(item => item.business_id === project)?.title || t('portfolio.projectLink') }}</RouterLink></nav>
   <div v-if="photos.length" class="portfolio-actions"><el-button type="primary" :loading="busy" @click="fileInput?.click()"><el-icon><UploadFilled /></el-icon>{{ t('portfolio.upload') }}</el-button><el-button :disabled="busy" @click="openPicker">{{ t('portfolio.copy') }}</el-button></div>
   <input ref="fileInput" type="file" accept="image/*" multiple hidden @change="upload" />
   <el-empty v-if="!photos.length" :description="t('portfolio.noPhotos')"><el-button type="primary" :disabled="busy" @click="fileInput?.click()">{{ t('portfolio.upload') }}</el-button><el-button :disabled="busy" @click="openPicker">{{ t('portfolio.copy') }}</el-button></el-empty>
   <PortfolioGallery v-else :photos="photos" :tags="tags" namespace="portfolio" editable :busy="busy" @save="savePhoto" @remove="removePhoto" />
  </template>
  <el-dialog :close-on-click-modal="!busy" :close-on-press-escape="!busy" :show-close="!busy" v-model="editOpen" :title="id ? t('portfolio.edit') : t('portfolio.new')" width="min(620px, 94vw)"><el-form label-position="top" @submit.prevent><el-form-item :label="t('portfolio.name')"><el-input v-model="form.title" maxlength="120" /></el-form-item><el-form-item :label="t('portfolio.description')"><el-input v-model="form.description" type="textarea" maxlength="5000" /></el-form-item><el-form-item :label="t('portfolio.tags')"><el-select v-model="form.tag_ids" :aria-label="t('portfolio.tags')" multiple filterable><el-option v-for="tag in tags" :key="tag.business_id" :label="tag.content" :value="tag.business_id" /></el-select><div class="portfolio-tag-create"><el-input v-model="newTag" maxlength="64" :placeholder="t('portfolio.newTagPlaceholder')" @keydown.enter.prevent="addTag" /><el-button :loading="tagBusy" :disabled="!newTag.trim()" @click="addTag">{{ t('portfolio.addTag') }}</el-button></div></el-form-item><el-form-item v-if="id" :label="t('portfolio.projects')"><el-select v-model="form.project_ids" multiple filterable><el-option v-for="project in projects" :key="project.business_id" :label="project.title" :value="project.business_id" /></el-select></el-form-item></el-form><template #footer><el-button :disabled="busy" @click="editOpen=false">{{ t('portfolio.cancel') }}</el-button><el-button type="primary" :loading="busy" @click="save">{{ t('portfolio.save') }}</el-button></template></el-dialog>
  <el-dialog :close-on-click-modal="!busy" :close-on-press-escape="!busy" :show-close="!busy" v-model="shareOpen" :title="t('portfolio.share')" width="min(480px, 94vw)"><el-button v-if="!link" type="primary" :loading="busy" @click="createShare">{{ current?.share_enabled ? t('portfolio.shareReset') : t('portfolio.shareOn') }}</el-button><el-button v-if="current?.share_enabled" :disabled="busy" @click="revokeShare">{{ t('portfolio.shareOff') }}</el-button><div v-if="link" class="portfolio-share"><img :src="qr" :alt="t('portfolio.shareLink')" /><label>{{ t('portfolio.shareLink') }}<el-input :model-value="link" readonly /></label><div class="portfolio-actions"><el-button type="primary" @click="copyLink">{{ t('portfolio.copyLink') }}</el-button><el-button :icon="Download" :loading="downloadingQR" @click="downloadQR">{{ t('portfolio.downloadQR') }}</el-button><el-button tag="a" :href="link" target="_blank" rel="noopener noreferrer">{{ t('portfolio.open') }}</el-button></div></div></el-dialog>
  <el-dialog v-model="copyOpen" :title="t('portfolio.copy')" width="min(760px, 94vw)" :close-on-click-modal="!busy" :close-on-press-escape="!busy" :show-close="!busy">
   <form class="picker-search" @submit.prevent="loadInspirations(1)"><el-input v-model="inspirationQuery" :aria-label="t('portfolio.searchInspiration')" :placeholder="t('portfolio.searchInspiration')" :prefix-icon="Search" clearable /><el-button native-type="submit" :loading="inspirationLoading">{{ t('portfolio.search') }}</el-button></form>
   <el-skeleton v-if="inspirationLoading" :rows="5" animated />
   <el-checkbox-group v-else v-model="selectedInspirations" class="portfolio-inspirations" :disabled="busy"><el-checkbox v-for="item in inspirations" :key="item.business_id" :value="item.business_id" class="picker-photo"><InspirationThumbnail :record="item" /><span class="picker-name">{{ item.file_name }}</span></el-checkbox></el-checkbox-group>
   <el-empty v-if="!inspirationLoading&&!inspirations.length" :description="t('portfolio.noResults')" />
   <el-pagination v-if="inspirationTotal>20" :disabled="busy||inspirationLoading" :current-page="inspirationPage" :page-size="20" :total="inspirationTotal" layout="prev, pager, next" @current-change="loadInspirations" />
   <template #footer><el-button :disabled="busy" @click="copyOpen=false">{{ t('portfolio.cancel') }}</el-button><el-button type="primary" :disabled="!selectedInspirations.length" :loading="busy" @click="copyPhotos">{{ t('portfolio.addSelected', { count: selectedInspirations.length }) }}</el-button></template>
  </el-dialog>
 </main></ContentZoom></div>
</template>
<style scoped>
.portfolio-main { max-width:1200px; margin:auto; padding:28px; } .portfolio-header { display:flex; justify-content:space-between; align-items:start; gap:20px; flex-wrap:wrap; } h1 { margin:8px 0; } .portfolio-header p { white-space:pre-wrap; overflow-wrap:anywhere; } .portfolio-search { max-width:280px; } .portfolio-actions .el-select { max-width:220px; } .portfolio-actions,.portfolio-projects { display:flex; gap:10px; flex-wrap:wrap; margin:16px 0; } .portfolio-list { display:grid; grid-template-columns:repeat(auto-fill,minmax(210px,1fr)); gap:14px; } .portfolio-card { display:grid; gap:8px; min-height:120px; padding:18px; border:1px solid var(--el-border-color); border-radius:12px; color:var(--el-text-color-primary); text-decoration:none; overflow-wrap:anywhere; } .portfolio-card img { display:block; width:100%; aspect-ratio: 4/3; object-fit:cover; border-radius:8px; } .portfolio-card:hover { border-color:var(--el-color-primary); } .portfolio-tag-create { display:flex; gap:8px; width:100%; margin-top:8px; } .portfolio-share { display:grid; justify-items:start; gap:12px; margin-top:20px; } .portfolio-share img { width:200px; } .portfolio-share label { display:grid; gap:6px; width:100%; } .portfolio-inspirations { display:grid; gap:8px; max-height:45vh; overflow:auto; margin:16px 0; } @media(max-width:600px){.portfolio-main{padding:16px}.portfolio-actions .el-button{margin:0}}

.portfolio-list{grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:22px}.portfolio-card{padding:0 0 18px;gap:8px;transition:border-color .2s,box-shadow .2s}.portfolio-card>strong,.portfolio-card>span{margin:0 18px}.portfolio-card>strong{font-size:18px}.portfolio-card>span{color:var(--el-text-color-secondary);display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}.portfolio-card img{border-radius:0;aspect-ratio:4/3}.portfolio-card:hover{box-shadow:var(--el-box-shadow-light)}.portfolio-card:focus-visible{outline:2px solid var(--el-color-primary);outline-offset:4px}.portfolio-cover-placeholder{aspect-ratio:4/3;display:grid;place-items:center;background:var(--el-fill-color-light);font-size:40px;color:var(--el-text-color-placeholder)}.portfolio-header h1{font-size:28px}.portfolio-header p:empty{display:none}.picker-search{display:flex;gap:10px;margin-bottom:18px}.portfolio-inspirations{grid-template-columns:repeat(4,minmax(0,1fr));gap:12px;max-height:50vh;padding:3px}.picker-photo{position:relative;display:flex;flex-direction:column-reverse;align-items:stretch;height:auto;margin:0;padding:8px;border:1px solid var(--el-border-color);border-radius:10px;overflow:hidden}.picker-photo.is-checked{border-color:var(--el-color-primary);background:var(--el-color-primary-light-9)}.picker-photo :deep(.el-checkbox__label){display:block;padding:0;min-width:0}.picker-photo :deep(.el-checkbox__input){position:absolute;top:14px;right:14px;z-index:1}.picker-photo :deep(.thumbnail-frame){aspect-ratio:1;background:var(--el-fill-color-light);display:grid;place-items:center;overflow:hidden;border-radius:6px}.picker-photo :deep(.thumbnail-frame img){width:100%;height:100%;object-fit:cover}.picker-photo :deep(.thumbnail-state){font-size:12px;white-space:normal}.picker-name{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;margin-top:8px}.portfolio-share{justify-items:center}.portfolio-share .portfolio-actions{justify-content:center}.portfolio-actions :deep(.el-button){margin-left:0;min-height:40px}.portfolio-main :deep(.el-select){max-width:100%}@media(max-width:600px){.portfolio-header{gap:0}.portfolio-header>div{width:100%}.portfolio-header h1{font-size:24px}.portfolio-header .portfolio-actions{margin-top:8px}.portfolio-inspirations{grid-template-columns:repeat(2,minmax(0,1fr))}.portfolio-list{grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.portfolio-card>strong,.portfolio-card>span{margin:0 12px}.portfolio-card>strong{font-size:16px}.portfolio-actions :deep(.el-button){min-height:44px}.portfolio-search{max-width:none}}

/* 桌面固定侧栏宽226px，主体需预留空间；手机底部导航保留安全留白。 */
@media(min-width:769px){.portfolio-main{width:auto;max-width:1320px;margin:0 34px 0 260px;padding:28px 0 64px}}
@media(max-width:768px){.portfolio-main{padding-bottom:calc(100px + env(safe-area-inset-bottom))}}
</style>
