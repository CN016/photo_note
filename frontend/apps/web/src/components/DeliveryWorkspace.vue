<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter, onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Upload, Download, Search, Setting, Share, Check, Close, Picture } from '@element-plus/icons-vue'
import QRCode from 'qrcode'
import { api } from '../api/client'
import type { DeliveryTask, DeliveryDetail, DeliveryPhoto, DeliverySettings } from '../api/delivery'
import { exportFile } from '../platform'
import { isAndroid, MAX_NATIVE_EXPORT_BYTES } from '../../../../packages/platform-adapters'
import { portfolioPublicBase } from '../views/portfolioPublicBase'
import { matchOriginals, selectionCSV } from '../delivery/matching'
import { localOriginalZip } from '../delivery/localZip'
import DeliveryImage from './DeliveryImage.vue'
import CreateFab from './CreateFab.vue'
import LanguageSwitcher from './LanguageSwitcher.vue'
import ThemeSwitcher from './ThemeSwitcher.vue'

const props=defineProps<{namespace:string;public?:boolean}>()
const {t,locale}=useI18n();const tr=(key:string,args:Record<string,string|number>={})=>t(`${props.namespace}.${key}`,args)
const route=useRoute();const router=useRouter()
const id=computed(()=>String(route.params.id||''));const token=computed(()=>String(route.params.token||''))
const tasks=ref<DeliveryTask[]>([]);const detail=ref<DeliveryDetail>();const loading=ref(false);const busy=ref(false);const error=ref('');const code=ref('');const unlocked=ref(false)
const tab=ref('upload');const search=ref('');const taskFilter=ref('');const photoFilter=ref('');const page=ref(1);const group=ref('');const checked=ref<string[]>([])
const selected=ref<string[]>([]);const notes=ref<Record<string,string>>({});const dirty=ref(false);const saving=ref(false);const saveError=ref(false)
const exportProgress=ref('');
const settingsOpen=ref(false);const shareOpen=ref(false);const historyOpen=ref(false);const link=ref('');const qr=ref('');const createMode=ref(false)
const directoryPicker=ref<HTMLInputElement>();const picker=ref<HTMLInputElement>();const localPicker=ref<HTMLInputElement>();const uploadKind=ref<'proof'|'final'>('proof')
const queue=ref<{file:File;status:string;kind:'proof'|'final';group:string}[]>([]);const cancelled=ref(false);const uploading=ref(false)
const viewer=ref<DeliveryPhoto>();const matchOpen=ref(false);const matches=ref<ReturnType<typeof matchOriginals>>([])
const form=ref<DeliverySettings>(defaults())
let draftKey='';let generation=0;let timer:ReturnType<typeof setTimeout>|undefined;let disposed=false

function defaults():DeliverySettings{return {title:'',customer:'',description:'',internal_note:'',limit_mode:'max',selection_limit:20,deadline:null,expires_at:null,allow_original:false,allow_download:true,access_code:'',clear_access_code:false}}
async function adoptServerSelection(){try{await ElMessageBox.confirm(tr('discardDraft'),tr('confirm'),{confirmButtonText:tr('confirm'),cancelButtonText:tr('cancel')});await load(false);selected.value=[...(round.value?.selected_ids||[])];notes.value={...(round.value?.notes||{})};dirty.value=false;saveError.value=false;if(draftKey)sessionStorage.removeItem(draftKey)}catch{/* 取消保留草稿。 */}}
const task=computed(()=>detail.value?.task)
const round=computed(()=>detail.value?.rounds.find(r=>r.number===task.value?.current_round))
const stages=['upload','cull','select','retouch']
const currentStage=computed(()=>task.value?.stage==='delivered'?'retouch':task.value?.stage||'upload')
const filteredTasks=computed(()=>tasks.value.filter(v=>(!taskFilter.value||v.status===taskFilter.value)&&`${v.title} ${v.customer}`.toLocaleLowerCase().includes(search.value.toLocaleLowerCase())))
const photos=computed(()=>{
 const kind=props.public ? (tab.value==='retouch'?'final':'proof') : tab.value==='retouch'?'final':'proof'
 return (detail.value?.photos||[]).filter(p=>p.kind===kind&&(!search.value||p.filename.toLocaleLowerCase().includes(search.value.toLocaleLowerCase()))&&(!group.value||p.group_name===group.value)&&(!photoFilter.value||(photoFilter.value==='selected'?selected.value.includes(p.business_id):p.mark===photoFilter.value)))
})
const visible=computed(()=>photos.value.slice((page.value-1)*60,page.value*60))
const groups=computed(()=>[...new Set((detail.value?.photos||[]).map(p=>p.group_name).filter(Boolean))])
const selectedPhotos=computed(()=>(detail.value?.photos||[]).filter(p=>selected.value.includes(p.business_id)))
const finalCount=computed(()=>(detail.value?.photos||[]).filter(p=>p.kind==='final').length)
const matchedCount=computed(()=>matches.value.filter(m=>m.file).length)
const canSelect=computed(()=>props.public&&round.value?.status==='open'&&task.value?.status==='active'&&task.value.stage==='select')
const availableFinal=computed(()=>(detail.value?.photos||[]).some(p=>p.kind==='final'&&p.published_batch>0))
const missingFinals=computed(()=>selectedPhotos.value.filter(p=>!(detail.value?.photos||[]).some(f=>f.kind==='final'&&f.source_id===p.business_id)))
const formatDate=(v:string)=>new Intl.DateTimeFormat(locale.value,{dateStyle:'medium',timeStyle:'short'}).format(new Date(v))
const number=(v:number)=>new Intl.NumberFormat(locale.value).format(v)
watch([search,group,photoFilter,tab],()=>{page.value=1;checked.value=[]})

/** 每次路由加载独立代次；公开访问码仅存内存，不写 URL、本地存储或日志。 */
async function load(reset=true){
 const stamp=++generation;loading.value=true;error.value=''
 try{
  if(props.public||id.value){
   const result=props.public?await api.deliveryPublicGet(token.value,code.value):await api.deliveryGet(id.value)
   if(stamp!==generation||disposed)return;detail.value=result;unlocked.value=true
   if(reset){tab.value=props.public?(result.task.stage==='delivered'?'retouch':'select'):result.task.stage==='delivered'?'retouch':result.task.stage;selected.value=[...(round.value?.selected_ids||[])];notes.value={...(round.value?.notes||{})};dirty.value=false;if(props.public)await restoreDraft()}
  }else{const result=await api.deliveryList();if(stamp===generation)tasks.value=result.items}
 }catch{if(stamp===generation)error.value=tr(props.public?'accessError':'loadError')}
 finally{if(stamp===generation)loading.value=false}
}
watch(()=>route.fullPath,()=>{generation++;detail.value=undefined;search.value='';group.value='';queue.value=[];link.value='';qr.value='';shareOpen.value=false;unlocked.value=false;void load()}, {immediate:true})
function beforeUnload(event:BeforeUnloadEvent){if(dirty.value||uploading.value){event.preventDefault();event.returnValue=''}}
window.addEventListener('beforeunload',beforeUnload)
onBeforeUnmount(()=>{window.removeEventListener('beforeunload',beforeUnload);disposed=true;generation++;if(timer)clearTimeout(timer);cancelled.value=true})
onBeforeRouteLeave(async()=>{if(uploading.value||dirty.value){try{await ElMessageBox.confirm(tr('leaveWarning'),tr('confirm'),{confirmButtonText:tr('leave'),cancelButtonText:tr('stay')});return true}catch{return false}};return true})
function fail(key='operationError'){ElMessage.error(tr(key))}

/** 管理操作串行提交当前任务版本，成功后重读服务端事实。 */
async function action(name:string,value?:string,extra:Record<string,unknown>={}){
 if(!task.value||busy.value||uploading.value)return
 busy.value=true
 try{
  const result=await api.deliveryAction(id.value,{version:task.value.version,action:name,value,...extra})
  if(name==='share'){link.value=new URL(`d/${result.token}`, publicBase()).href;qr.value=await QRCode.toDataURL(link.value,{width:480,margin:2});shareOpen.value=true}
  await load(false);if(name==='publish'||name==='reopen'||name==='confirm'){selected.value=[...(round.value?.selected_ids||[])];notes.value={...(round.value?.notes||{})};tab.value=currentStage.value}
  if(name==='publish_final')tab.value='retouch';ElMessage.success(tr('saved'))
 }catch{fail();await load(false)}finally{busy.value=false}
}
async function confirmAction(name:string,value?:string){try{await ElMessageBox.confirm(tr(`confirm_${name}`),tr('confirm'),{confirmButtonText:tr('confirm'),cancelButtonText:tr('cancel')});await action(name,value)}catch{/* 取消不改变任务。 */}}
function openSettings(create=false){createMode.value=create;form.value=create?defaults():{...defaults(),...task.value,access_code:'',clear_access_code:false};settingsOpen.value=true}
async function saveSettings(){
 if(!form.value.title.trim()){fail('titleRequired');return};busy.value=true
 try{if(createMode.value){const result=await api.deliveryCreate(form.value);settingsOpen.value=false;await router.push(`/delivery/tasks/${result.business_id}`)}else if(task.value){await api.deliveryAction(id.value,{version:task.value.version,action:'settings',settings:form.value});settingsOpen.value=false;await load(false)}}catch{fail()}finally{busy.value=false}
}
function chooseUpload(kind:'proof'|'final'){uploadKind.value=kind;picker.value?.click()}
/** 文件选择与拖入共用队列；目录选择只接收支持的位图，不把未知格式静默当作成功。 */
function enqueueFiles(files:File[]){
 if(!task.value||task.value.status!=='active'||props.public||uploading.value||busy.value)return
 const accepted=files.filter(f=>/\.(jpe?g|png|webp|gif)$/i.test(f.name))
 if(accepted.length!==files.length)ElMessage.info(tr('unsupportedFiles'))
 queue.value.push(...accepted.map(file=>({file,status:'queued',kind:uploadKind.value,group:group.value||file.webkitRelativePath.split('/').slice(0,-1).join('/').slice(0,120)})))
 void runQueue()
}
function addFiles(event:Event){const input=event.target as HTMLInputElement;enqueueFiles(Array.from(input.files||[]));input.value=''}
function dropFiles(event:DragEvent){if(props.public)return;event.preventDefault();uploadKind.value=tab.value==='retouch'?'final':'proof';enqueueFiles(Array.from(event.dataTransfer?.files||[]))}
function chooseDirectory(){uploadKind.value=tab.value==='retouch'?'final':'proof';directoryPicker.value?.click()}

/** 顺序上传释放每张文件的请求内存；失败单独保留，刷新后通过资产列表恢复已成功文件。 */
async function runQueue(){
 if(uploading.value||!task.value)return;uploading.value=true;cancelled.value=false
 try{for(const item of queue.value){if(cancelled.value||disposed)break;if(item.status!=='queued')continue;item.status='uploading'
  const body=new FormData();body.append('file',item.file,item.file.name);body.append('kind',item.kind);body.append('version',String(task.value.version));body.append('group_name',item.group)
  try{await api.deliveryUpload(id.value,body);item.status='done';await load(false)}catch{item.status='failed';await load(false)}
 }}finally{uploading.value=false}
}
function retryQueue(){for(const item of queue.value){if(item.status==='failed')item.status='queued'};void runQueue()}
function toggle(photo:DeliveryPhoto){
 if(!canSelect.value)return
 const idx=selected.value.indexOf(photo.business_id)
 if(idx>=0)selected.value.splice(idx,1);else{
  if(round.value?.limit_mode!=='unlimited'&&selected.value.length>=(round.value?.selection_limit||0)){fail('limitReached');return};selected.value.push(photo.business_id)
 };changed()
}
function changed(){dirty.value=true;saveError.value=false;persistDraft();if(timer)clearTimeout(timer);timer=setTimeout(()=>{void saveSelection(false)},700)}
/** 断网草稿仅存当前标签页会话，键使用令牌摘要，不保存访问码或令牌；重开旧轮次不会自动覆盖新轮次。 */
function persistDraft(){if(!draftKey||!round.value)return;try{sessionStorage.setItem(draftKey,JSON.stringify({round:round.value.number,version:round.value.version,selected_ids:selected.value,notes:notes.value}))}catch{/* 浏览器拒绝会话存储时仍保留内存草稿和离开确认。 */}}
async function restoreDraft(){
 if(!globalThis.crypto?.subtle)return
 const hash=await crypto.subtle.digest('SHA-256',new TextEncoder().encode(token.value));draftKey='photo-nini.delivery-draft.'+[...new Uint8Array(hash)].map(v=>v.toString(16).padStart(2,'0')).join('')
 try{
  const raw=sessionStorage.getItem(draftKey);if(!raw)return
  const draft=JSON.parse(raw) as {round:number;version:number;selected_ids:string[];notes:Record<string,string>}
  if(round.value?.status!=='open'||draft.round!==round.value.number){sessionStorage.removeItem(draftKey);return}
  if(!Array.isArray(draft.selected_ids)||draft.selected_ids.some(v=>!round.value!.photo_ids.includes(v)))return
  selected.value=draft.selected_ids;notes.value=draft.notes||{};dirty.value=true;saveError.value=draft.version!==round.value.version
  ElMessage.info(tr(saveError.value?'draftConflict':'draftRestored'))
  if(!saveError.value)timer=setTimeout(()=>{void saveSelection(false)},700)
 }catch{/* 无效或被手动修改的本地草稿不影响服务器记录。 */}
}

/** 同一页面只允许一个选片请求在途；新操作在返回后继续同步，版本冲突保留本地选择供用户检查。 */
async function saveSelection(submit:boolean){
 if(!round.value||saving.value)return
 if(timer)clearTimeout(timer)
 saving.value=true;const ids=[...selected.value];const copy={...notes.value};const r=round.value;dirty.value=false
 try{
  await api.deliverySelect(token.value,code.value,{round:r.number,version:r.version,selected_ids:ids,notes:copy,submit})
  await load(false)
  if(!dirty.value){selected.value=[...(round.value?.selected_ids||[])];notes.value={...(round.value?.notes||{})}}
  saveError.value=false;if(dirty.value)persistDraft();else if(draftKey)sessionStorage.removeItem(draftKey)
 }catch{dirty.value=true;saveError.value=true;persistDraft();fail('selectionError');await load(false)}
 finally{saving.value=false;if(dirty.value&&!saveError.value)timer=setTimeout(()=>{void saveSelection(false)},500)}
}
async function submitSelection(){try{await ElMessageBox.confirm(tr('confirm_submit'),tr('confirm'),{confirmButtonText:tr('submit'),cancelButtonText:tr('cancel')});await saveSelection(true)}catch{/* 取消保留草稿。 */}}
async function download(photo:DeliveryPhoto){busy.value=true;try{const blob=props.public?await api.deliveryPublicFile(token.value,code.value,photo.business_id,true):await api.deliveryFile(id.value,photo.business_id,true);await exportFile(blob,photo.filename)}catch{fail('downloadError')}finally{busy.value=false}}
/** 异步作业明确区分排队、生成和下载；离开页面停止轮询，后台作业保持可恢复。 */
async function downloadZip(kind:string){
 busy.value=true;exportProgress.value=tr('exportQueued');const stamp=generation
 try{
  const files=(detail.value?.photos||[]).filter(p=>p.kind===kind&&(kind==='final'?p.published_batch>0:props.public||selected.value.includes(p.business_id)))
  const budget=isAndroid()?MAX_NATIVE_EXPORT_BYTES-1024*1024:256*1024*1024
  const batches:DeliveryPhoto[][]=[];let current:DeliveryPhoto[]=[];let bytes=0
  for(const photo of files){if(photo.bytes>budget)throw new Error('single_file_too_large');if(current.length&&bytes+photo.bytes>budget){batches.push(current);current=[];bytes=0};current.push(photo);bytes+=photo.bytes+512}
  if(current.length)batches.push(current)
  for(let index=0;index<batches.length;index++){
   const batch=batches[index]!
   let job=await api.deliveryCreateExport(id.value,kind,props.public?token.value:'',code.value,batch.map(p=>p.business_id))
   while(job.status==='pending'||job.status==='running'){
    if(disposed||stamp!==generation)return
    exportProgress.value=tr('exportPart',{part:index+1,parts:batches.length})+' · '+tr('exportProgress',{done:job.progress,total:job.total})
    await new Promise(resolve=>setTimeout(resolve,1500))
    if(disposed||stamp!==generation)return
    job=await api.deliveryExportStatus(id.value,job.business_id,props.public?token.value:'',code.value)
   }
   if(job.status!=='completed')throw new Error('export_failed')
   exportProgress.value=tr('downloading')
   const blob=await api.deliveryExportFile(id.value,job.business_id,props.public?token.value:'',code.value)
   const result=await exportFile(blob,`delivery-${kind}-${index+1}.zip`)
   if(result==='cancelled')break
  }
 }catch{fail('downloadError')}finally{busy.value=false;exportProgress.value=''}
}
async function exportList(){await exportFile(new Blob([selectionCSV(selectedPhotos.value)],{type:'text/csv;charset=utf-8'}),'selection.csv')}
async function exportTXT(){await exportFile(new Blob([selectedPhotos.value.map(v=>v.filename).join('\r\n')],{type:'text/plain;charset=utf-8'}),'selection.txt')}
function matchFiles(event:Event){const input=event.target as HTMLInputElement;matches.value=matchOriginals(selectedPhotos.value,Array.from(input.files||[]));input.value='';matchOpen.value=true}
async function exportLocal(){busy.value=true;try{const items=matches.value.filter(v=>v.file).map(v=>({name:`${v.photo.business_id}/${v.file!.name}`,file:v.file!}));if(isAndroid()&&items.reduce((n,v)=>n+v.file.size,0)>MAX_NATIVE_EXPORT_BYTES-1024*1024)throw new Error('native_export_too_large');await exportFile(await localOriginalZip(items),'selected-originals.zip')}catch{fail('localExportError')}finally{busy.value=false}}
function publicBase():string{const base=portfolioPublicBase(import.meta.env.VITE_PUBLIC_WEB_BASE_URL,import.meta.env.VITE_ANDROID_WEB_UPDATE_URL,location.origin);if(!base)throw new Error('public_url_unavailable');return base}
async function copyLink(){try{const {clipboardAdapter}=await import('../../../../packages/platform-adapters');if(!clipboardAdapter.canWriteText())throw new Error('clipboard');await clipboardAdapter.writeText(link.value);ElMessage.success(tr('copied'))}catch{fail('copyError')}}
async function saveQR(){try{await exportFile(await(await fetch(qr.value)).blob(),'delivery-qr.png')}catch{fail()}}
async function receive(){busy.value=true;try{await api.deliveryReceive(token.value,code.value);await load(false)}catch{fail()}finally{busy.value=false}}
function nextPhoto(delta:number){const list=photos.value;const i=list.findIndex(p=>p.business_id===viewer.value?.business_id);viewer.value=list[(i+delta+list.length)%list.length]}
function selectionLabel(){return round.value?.limit_mode==='unlimited'?tr('selectedCount',{count:number(selected.value.length)}):tr('selectedLimit',{count:number(selected.value.length),limit:number(round.value?.selection_limit||0)})}
</script>

<template>
 <div class="delivery-app">
  <header class="delivery-top"><div class="identity"><el-icon :size="24"><Picture/></el-icon><strong>{{ tr('title') }}</strong><span v-if="task" class="task-title">{{ task.title }}</span></div><div class="top-actions"><ThemeSwitcher/><LanguageSwitcher/><el-button v-if="!props.public&&task" :icon="Setting" :disabled="uploading" @click="openSettings()">{{ tr('settings') }}</el-button></div></header>
  <main id="main-content" class="delivery-main" v-loading="loading">
   <el-alert v-if="error" :title="error" type="error" :closable="false"><el-button @click="load()">{{ tr('retry') }}</el-button></el-alert>
   <form v-if="props.public&&!unlocked" class="access-form" @submit.prevent="load()"><h1>{{ tr('openDelivery') }}</h1><el-input v-model="code" type="password" show-password :placeholder="tr('accessCode')" :aria-label="tr('accessCode')"/><el-button type="primary" native-type="submit" :loading="loading">{{ tr('open') }}</el-button></form>
   <template v-else-if="!task&&!props.public">
    <div class="section-heading"><h1>{{ tr('tasks') }}</h1><span>{{ number(filteredTasks.length) }}</span></div>
    <div class="toolbar"><el-input v-model="search" :prefix-icon="Search" clearable :placeholder="tr('searchTasks')" :aria-label="tr('searchTasks')"/><el-select v-model="taskFilter" clearable :placeholder="tr('allStatuses')" :aria-label="tr('allStatuses')"><el-option v-for="s in ['active','paused','completed','archived','cancelled']" :key="s" :value="s" :label="tr(`status_${s}`)"/></el-select></div>
    <el-empty v-if="!filteredTasks.length&&!loading" :description="tr('noTasks')" />
    <div class="task-grid"><RouterLink v-for="item in filteredTasks" :key="item.business_id" :data-shared-transition="item.business_id" :to="`/delivery/tasks/${item.business_id}`" class="task-card"><div class="task-card-top"><el-tag effect="plain">{{ tr(`stage_${item.stage}`) }}</el-tag><span>{{ tr(`status_${item.status}`) }}</span></div><h2>{{ item.title }}</h2><p>{{ item.customer }}</p><time>{{ formatDate(item.updated_at) }}</time></RouterLink></div>
   </template>
   <template v-else-if="task">
    <div class="task-heading"><div><h1>{{ task.title }}</h1><p v-if="task.description">{{ task.description }}</p></div><el-tag>{{ tr(`status_${task.status}`) }}</el-tag></div>
    <nav v-if="!props.public" class="stage-nav" :aria-label="tr('stages')"><button v-for="(s,i) in stages" :key="s" :class="{active:tab===s,current:currentStage===s}" @click="tab=s"><span class="stage-number">{{ i+1 }}</span><span>{{ tr(`stage_${s}`) }}</span><small v-if="currentStage===s">{{ tr('current') }}</small></button></nav>
    <div v-else class="customer-tabs"><el-button :type="tab==='select'?'primary':'default'" @click="tab='select'">{{ tr('selection') }}</el-button><el-button v-if="availableFinal" :type="tab==='retouch'?'primary':'default'" @click="tab='retouch'">{{ tr('finals') }}</el-button></div>
    <section class="stage-content" @dragover.prevent @drop="dropFiles"><p v-if="exportProgress" role="status" aria-live="polite">{{ exportProgress }}</p>
     <div class="section-heading"><h2>{{ tr(`stage_${tab}`) }}</h2><div v-if="!props.public" class="inline-actions"><el-button :icon="Share" :disabled="busy||uploading" @click="confirmAction('share')">{{ tr('share') }}</el-button><el-button :disabled="busy||uploading||saving" @click="load()">{{ tr('refresh') }}</el-button><el-button @click="historyOpen=true">{{ tr('history') }}</el-button></div></div>
     <template v-if="!props.public">
      <div class="action-bar">
       <template v-if="tab==='upload'"><el-button type="primary" :icon="Upload" :disabled="busy||uploading||task.status!=='active'" @click="chooseUpload('proof')">{{ tr('uploadProof') }}</el-button><el-button :disabled="busy||uploading||task.status!=='active'" @click="chooseDirectory">{{ tr('uploadDirectory') }}</el-button><el-button :disabled="busy||uploading" @click="action('stage','cull');tab='cull'">{{ tr('startCull') }}</el-button><el-button :disabled="busy||uploading" @click="confirmAction('publish')">{{ tr('publishAll') }}</el-button></template>
       <template v-if="tab==='cull'"><el-button :icon="Check" :disabled="!checked.length||busy||uploading" @click="action('mark','keep',{photo_ids:checked})">{{ tr('keep') }}</el-button><el-button :icon="Close" :disabled="!checked.length||busy||uploading" @click="action('mark','exclude',{photo_ids:checked})">{{ tr('exclude') }}</el-button><el-button :disabled="!checked.length||busy||uploading" @click="action('mark','pending',{photo_ids:checked})">{{ tr('resetMark') }}</el-button><el-button type="primary" :disabled="busy||uploading" @click="confirmAction('publish')">{{ tr('publishSelection') }}</el-button></template>
       <template v-if="tab==='select'"><el-tag v-if="round">{{ tr(`round_${round.status}`) }} · {{ selectionLabel() }}</el-tag><el-button v-if="round?.status==='submitted'" type="primary" :disabled="busy||uploading" @click="confirmAction('confirm')">{{ tr('confirmSelection') }}</el-button><el-button v-if="round" :disabled="busy||uploading" @click="confirmAction('reopen')">{{ tr('reopen') }}</el-button><el-button v-if="selected.length" :disabled="busy" :icon="Download" @click="exportList">{{ tr('exportCSV') }}</el-button><el-button v-if="selected.length" :disabled="busy" @click="exportTXT">{{ tr('exportTXT') }}</el-button><el-button v-if="selected.length" :disabled="busy" @click="localPicker?.click()">{{ tr('matchOriginals') }}</el-button><el-button v-if="selected.length" :disabled="busy" @click="downloadZip('proof')">{{ tr('exportOriginals') }}</el-button></template>
       <template v-if="tab==='retouch'"><el-button type="primary" :icon="Upload" :disabled="busy||uploading||task.status!=='active'" @click="chooseUpload('final')">{{ tr('uploadFinal') }}</el-button><el-button :disabled="busy||uploading||!finalCount" @click="confirmAction('publish_final')">{{ tr('publishFinal') }}</el-button><el-button :disabled="busy||!availableFinal" @click="downloadZip('final')">{{ tr('downloadFinals') }}</el-button><el-tag v-if="task.received_at" type="success">{{ tr('received') }}</el-tag></template>
      </div>
      <p v-if="tab==='select'&&round?.notes?.overall" class="customer-note">{{ tr('overallNote') }}: {{ round.notes.overall }}</p><div v-if="tab==='retouch'&&missingFinals.length" class="missing-finals"><strong>{{ tr('missingFinals',{count:missingFinals.length}) }}</strong><span>{{ missingFinals.map(p=>p.filename).join(', ') }}</span></div><div v-if="tab==='retouch'" class="counts">{{ tr('retouchCounts',{selected:number(selected.length),finals:number(finalCount),linked:number((detail?.photos||[]).filter(p=>p.kind==='final'&&p.source_id&&selected.includes(p.source_id)).length)}) }}</div>
     </template>
     <template v-else>
      <div v-if="tab==='select'" class="action-bar"><strong>{{ selectionLabel() }}</strong><el-tag v-if="round">{{ tr(`round_${round.status}`) }}</el-tag><span v-if="round?.deadline">{{ tr('deadline') }} {{ formatDate(round.deadline) }}</span><span v-if="saving">{{ tr('saving') }}</span><el-button v-if="saveError" :disabled="saving" @click="adoptServerSelection">{{ tr('adoptServer') }}</el-button><el-button v-if="saveError" type="danger" :disabled="saving" @click="saveSelection(false)">{{ tr('retrySave') }}</el-button><el-button v-if="canSelect" type="primary" :disabled="saving||busy||saveError" @click="submitSelection">{{ tr('submit') }}</el-button><el-button v-if="task.allow_original" :disabled="busy" @click="downloadZip('proof')">{{ tr('downloadProofs') }}</el-button></div>
      <div v-if="tab==='retouch'" class="action-bar"><el-button v-if="task.allow_download" type="primary" :disabled="busy" :icon="Download" @click="downloadZip('final')">{{ tr('downloadFinals') }}</el-button><el-button :disabled="busy||!!task.received_at" @click="receive">{{ task.received_at?tr('received'):tr('confirmReceive') }}</el-button></div>
     </template>
     <div v-if="queue.length&&!props.public" class="upload-queue"><div class="inline-actions"><strong>{{ tr('uploadProgress',{done:number(queue.filter(q=>q.status==='done').length),total:number(queue.length)}) }}</strong><el-button v-if="uploading" @click="cancelled=true">{{ tr('stopQueue') }}</el-button><el-button v-else @click="retryQueue">{{ tr('retryFailed') }}</el-button></div><ul><li v-for="(q,i) in queue.filter(q=>q.status!=='done')" :key="i"><span>{{ q.file.name }}</span><span>{{ tr(`upload_${q.status}`) }}</span></li></ul></div>
     <div class="toolbar"><el-input v-model="search" :prefix-icon="Search" clearable :placeholder="tr('searchPhotos')" :aria-label="tr('searchPhotos')"/><el-select v-model="photoFilter" clearable :placeholder="tr('allPhotos')" :aria-label="tr('allPhotos')"><el-option value="selected" :label="tr('selectedOnly')"/><template v-if="!props.public"><el-option v-for="m in ['pending','keep','exclude']" :key="m" :value="m" :label="tr(`mark_${m}`)"/></template></el-select><el-select v-model="group" allow-create filterable clearable :placeholder="tr('group')" :aria-label="tr('group')"><el-option v-for="g in groups" :key="g" :value="g" :label="g"/></el-select><el-button v-if="!props.public&&checked.length" :disabled="busy||uploading" @click="action('group',group,{photo_ids:checked})">{{ tr('applyGroup') }}</el-button></div>
     <el-empty v-if="!photos.length&&!loading" :description="tr('noPhotos')"/>
     <div class="photo-grid"><article v-for="photo in visible" :key="photo.business_id" class="photo-card" :class="{selected:selected.includes(photo.business_id),excluded:photo.mark==='exclude'&&!props.public}"><button class="photo-open" :aria-label="tr('viewPhoto',{name:photo.filename})" @click="viewer=photo"><DeliveryImage :task="task.business_id" :photo="photo.business_id" :filename="photo.filename" :token="props.public?token:undefined" :code="code" :retry-label="tr('retry')"/></button><div class="photo-info"><span :title="photo.filename">{{ photo.filename }}</span><el-checkbox v-if="props.public&&tab==='select'" :model-value="selected.includes(photo.business_id)" :disabled="!canSelect||saveError" :aria-label="tr('choosePhoto',{name:photo.filename})" @change="toggle(photo)"/><el-checkbox v-else-if="!props.public" v-model="checked" :value="photo.business_id" :aria-label="tr('choosePhoto',{name:photo.filename})"/></div><div class="photo-meta"><span v-if="!props.public&&photo.kind==='proof'">{{ tr(`mark_${photo.mark}`) }}</span><span v-if="photo.kind==='final'">{{ photo.published_batch?tr('batch',{batch:photo.published_batch}):tr('draft') }}<template v-if="!props.public"> · {{ photo.source_id?tr('linked'):tr('unlinked') }}</template></span><span>{{ photo.group_name }}</span></div></article></div>
     <el-pagination v-if="photos.length>60" v-model:current-page="page" :page-size="60" :total="photos.length" layout="prev, pager, next"/>
     <el-input v-if="props.public&&tab==='select'&&round" v-model="notes.overall" type="textarea" :disabled="!canSelect||saveError" :maxlength="1500" :placeholder="tr('overallNote')" :aria-label="tr('overallNote')" @input="changed"/>
    </section>
   </template>
  </main>
  <input ref="directoryPicker" class="file-picker" type="file" webkitdirectory multiple @change="addFiles"/><input ref="picker" class="file-picker" type="file" accept="image/jpeg,image/png,image/webp,image/gif" multiple @change="addFiles"/><input ref="localPicker" class="file-picker" type="file" webkitdirectory multiple @change="matchFiles"/>
  <el-dialog v-model="settingsOpen" :title="createMode?tr('new'):tr('settings')" width="min(640px,94vw)" :close-on-click-modal="false"><el-form label-position="top" @submit.prevent="saveSettings"><el-form-item :label="tr('name')"><el-input v-model="form.title" maxlength="120"/></el-form-item><el-form-item :label="tr('customer')"><el-input v-model="form.customer" maxlength="120"/></el-form-item><el-form-item :label="tr('description')"><el-input v-model="form.description" type="textarea" maxlength="4000"/></el-form-item><el-form-item :label="tr('internalNote')"><el-input v-model="form.internal_note" type="textarea" maxlength="4000"/></el-form-item><div class="form-row"><el-form-item :label="tr('limitMode')"><el-select v-model="form.limit_mode"><el-option v-for="m in ['unlimited','max','exact']" :key="m" :value="m" :label="tr(`limit_${m}`)"/></el-select></el-form-item><el-form-item v-if="form.limit_mode!=='unlimited'" :label="tr('limit')"><el-input-number v-model="form.selection_limit" :min="1" :max="10000"/></el-form-item></div><el-form-item :label="tr('deadline')"><el-date-picker v-model="form.deadline" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ"/></el-form-item><el-form-item :label="tr('expiry')"><el-date-picker v-model="form.expires_at" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ"/></el-form-item><el-form-item :label="tr('accessCode')"><el-input v-model="form.access_code" type="password" show-password autocomplete="new-password" :placeholder="task?.has_access_code?tr('keepCode'):tr('optionalCode')" maxlength="64"/></el-form-item><el-checkbox v-model="form.clear_access_code">{{ tr('clearCode') }}</el-checkbox><div><el-checkbox v-model="form.allow_original">{{ tr('allowOriginal') }}</el-checkbox></div><div><el-checkbox v-model="form.allow_download">{{ tr('allowDownload') }}</el-checkbox></div></el-form><div v-if="task&&!createMode" class="task-lifecycle"><el-select :model-value="task.status" :disabled="busy" @change="(v:string)=>confirmAction('status',v)"><el-option v-for="s in ['active','paused','completed','archived','cancelled']" :key="s" :value="s" :label="tr(`status_${s}`)"/></el-select><el-button v-if="task.shared" type="danger" plain :disabled="busy" @click="confirmAction('revoke')">{{ tr('revoke') }}</el-button></div><template #footer><el-button @click="settingsOpen=false">{{ tr('cancel') }}</el-button><el-button type="primary" :loading="busy" @click="saveSettings">{{ tr('save') }}</el-button></template></el-dialog>
  <el-dialog v-model="shareOpen" :title="tr('share')" width="min(460px,94vw)"><div class="qr-content"><img v-if="qr" :src="qr" :alt="tr('qr')"/><el-input :model-value="link" readonly :aria-label="tr('shareLink')"/><el-button @click="copyLink">{{ tr('copyLink') }}</el-button><el-button :icon="Download" @click="saveQR">{{ tr('downloadQR') }}</el-button></div></el-dialog>
  <el-dialog v-model="historyOpen" :title="tr('history')" width="min(640px,94vw)"><ul class="history-list"><li v-for="(event,i) in detail?.events" :key="i"><span>{{ tr(`event_${event.action}`) }}</span><time>{{ formatDate(event.at) }}</time></li></ul><h3>{{ tr('roundHistory') }}</h3><div v-for="r in detail?.rounds" :key="r.number" class="history-round"><strong>{{ tr('roundNumber',{number:r.number}) }} · {{ tr(`round_${r.status}`) }}</strong><span>{{ tr('selectedCount',{count:r.selected_ids.length}) }}</span></div></el-dialog>
  <el-dialog :model-value="!!viewer" :title="viewer?.filename" width="min(1280px,96vw)" @close="viewer=undefined"><template v-if="viewer&&task"><div tabindex="0" @keydown.left.prevent="nextPhoto(-1)" @keydown.right.prevent="nextPhoto(1)"><DeliveryImage :key="viewer.business_id" large :task="task.business_id" :photo="viewer.business_id" :filename="viewer.filename" :token="props.public?token:undefined" :code="code" :retry-label="tr('retry')"/></div><div class="viewer-actions"><el-button @click="nextPhoto(-1)">{{ tr('previous') }}</el-button><el-button v-if="props.public&&tab==='select'" type="primary" :disabled="!canSelect||saveError" @click="toggle(viewer)">{{ selected.includes(viewer.business_id)?tr('unselect'):tr('select') }}</el-button><template v-if="!props.public&&viewer.kind==='proof'"><el-button :disabled="busy||uploading" @click="action('mark','keep',{photo_ids:[viewer.business_id]})">{{ tr('keep') }}</el-button><el-button :disabled="busy||uploading" @click="action('mark','exclude',{photo_ids:[viewer.business_id]})">{{ tr('exclude') }}</el-button></template><el-button v-if="!props.public||(viewer.kind==='proof'?task.allow_original:task.allow_download)" :disabled="busy" @click="download(viewer)">{{ tr('download') }}</el-button><el-button @click="nextPhoto(1)">{{ tr('next') }}</el-button></div><p v-if="!props.public&&viewer.kind==='proof'&&round?.notes?.[viewer.business_id]" class="customer-note">{{ round.notes[viewer.business_id] }}</p><el-input v-if="props.public&&viewer.kind==='proof'" v-model="notes[viewer.business_id]" type="textarea" :disabled="!canSelect||saveError" :maxlength="1500" :placeholder="tr('photoNote')" :aria-label="tr('photoNote')" @input="changed"/><el-select v-if="!props.public&&viewer.kind==='final'&&!viewer.published_batch" :model-value="viewer.source_id" filterable clearable :placeholder="tr('linkSource')" :disabled="busy||uploading" @change="(v:string)=>action('link',undefined,{photo_ids:[viewer!.business_id],source_id:v||null})"><el-option v-for="p in detail?.photos.filter(p=>p.kind==='proof')" :key="p.business_id" :value="p.business_id" :label="`${p.group_name} / ${p.filename}`"/></el-select></template></el-dialog>
  <el-dialog v-model="matchOpen" :title="tr('matchOriginals')" width="min(820px,96vw)"><p>{{ tr('matchCounts',{matched:matchedCount,total:matches.length}) }}</p><div v-for="m in matches" :key="m.photo.business_id" class="match-row"><strong>{{ m.photo.filename }}</strong><el-select v-if="m.candidates.length>1" v-model="m.file" value-key="webkitRelativePath" :placeholder="tr('ambiguous')"><el-option v-for="(f,i) in m.candidates" :key="i" :label="f.webkitRelativePath||f.name" :value="f"/></el-select><span v-else>{{ m.file?(m.file.webkitRelativePath||m.file.name):tr('missing') }}</span></div><template #footer><el-button @click="exportList">{{ tr('exportCSV') }}</el-button><el-button type="primary" :disabled="matchedCount!==matches.length||!matchedCount" :loading="busy" @click="exportLocal">{{ tr('exportMatched') }}</el-button></template></el-dialog>
  <CreateFab v-if="!props.public&&!task" :label="tr('new')" standalone @select="openSettings(true)" />
 </div>
</template>

<style scoped>
.customer-note{white-space:pre-wrap;overflow-wrap:anywhere}.missing-finals{display:grid;gap:8px;padding:12px 0;font-size:14px}.missing-finals span{color:var(--el-text-color-secondary);overflow-wrap:anywhere}.delivery-app{min-height:100dvh;background:var(--el-bg-color-page);color:var(--el-text-color-primary)}.delivery-top{display:flex;align-items:center;justify-content:space-between;gap:20px;padding:18px 32px;border-bottom:1px solid var(--el-border-color-lighter);background:var(--el-bg-color)}.identity,.top-actions,.inline-actions,.action-bar,.section-heading,.task-heading,.task-card-top{display:flex;align-items:center;gap:12px}.identity strong{font-size:20px;white-space:nowrap}.identity .el-icon{color:var(--el-color-primary)}.task-title{border-left:1px solid var(--el-border-color);padding-left:16px;max-width:40vw;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.delivery-main{max-width:1520px;padding:28px 32px 48px;margin:auto}.section-heading,.task-heading{justify-content:space-between;margin:0 0 20px}.section-heading h1,.section-heading h2,.task-heading h1{margin:0;font-weight:600}.section-heading h1,.task-heading h1{font-size:26px}.section-heading h2{font-size:20px}.task-heading p{color:var(--el-text-color-secondary);white-space:pre-wrap;max-width:800px}.toolbar{display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin:20px 0}.toolbar .el-input{width:300px}.toolbar .el-select{width:180px}.task-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(280px,1fr));gap:20px}.task-card{display:block;color:inherit;text-decoration:none;background:var(--el-bg-color);border:1px solid var(--el-border-color-lighter);padding:24px;border-radius:10px;transition:border-color .16s}.task-card:hover{border-color:var(--el-color-primary)}.task-card h2{font-size:20px;margin:24px 0 12px}.task-card p,.task-card time{color:var(--el-text-color-secondary);font-size:14px}.task-card-top{justify-content:space-between;font-size:13px}.stage-nav{display:grid;grid-template-columns:repeat(4,1fr);border-bottom:1px solid var(--el-border-color);margin:28px 0;background:var(--el-bg-color);border-radius:8px 8px 0 0}.stage-nav button{border:0;border-bottom:3px solid transparent;background:transparent;color:var(--el-text-color-regular);min-height:66px;display:flex;align-items:center;justify-content:center;gap:10px;cursor:pointer;font:inherit}.stage-nav button.active{border-bottom-color:var(--el-color-primary);color:var(--el-color-primary)}.stage-number{display:grid;place-items:center;width:25px;height:25px;border:1px solid currentColor;border-radius:50%;font-size:13px}.stage-nav small{font-size:12px;color:var(--el-text-color-secondary)}.action-bar{flex-wrap:wrap;margin-bottom:20px}.counts{font-size:14px;color:var(--el-text-color-secondary)}.photo-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(210px,1fr));gap:16px;margin:20px 0}.photo-card{background:var(--el-bg-color);border:2px solid transparent;border-radius:8px;overflow:hidden}.photo-card.selected{border-color:var(--el-color-primary)}.photo-card.excluded{opacity:.65}.photo-open{display:block;width:100%;padding:0;border:0;cursor:pointer;background:transparent}.photo-info{display:flex;align-items:center;justify-content:space-between;gap:8px;padding:4px 12px;font-size:14px}.photo-info>span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.photo-meta{padding:0 12px 12px;color:var(--el-text-color-secondary);display:flex;justify-content:space-between;gap:8px;font-size:12px}.customer-tabs{display:flex;gap:12px;margin:24px 0}.form-row{display:grid;grid-template-columns:1fr 1fr;gap:20px}.qr-content{text-align:center}.qr-content img{display:block;width:min(300px,80vw);margin:auto}.qr-content .el-input{margin:16px 0}.file-picker{display:none}.upload-queue{background:var(--el-bg-color);border:1px solid var(--el-border-color);padding:16px;border-radius:8px}.upload-queue ul{max-height:160px;overflow:auto;padding:0;list-style:none}.upload-queue li,.history-list li,.history-round,.match-row{display:flex;justify-content:space-between;gap:16px;padding:10px 0;border-bottom:1px solid var(--el-border-color-lighter);overflow-wrap:anywhere}.history-list{padding:0;list-style:none}.history-list time{color:var(--el-text-color-secondary);font-size:13px}.task-lifecycle{display:flex;gap:12px;border-top:1px solid var(--el-border-color);padding-top:20px;margin-top:20px}.viewer-actions{display:flex;flex-wrap:wrap;gap:8px;justify-content:center;margin:16px 0}.access-form{max-width:360px;display:grid;gap:20px;margin:12vh auto}.match-row{align-items:center}.match-row .el-select{width:60%}button:focus-visible,.task-card:focus-visible{outline:2px solid var(--el-color-primary);outline-offset:3px}
@media(max-width:768px){.delivery-top{padding:12px 16px;gap:10px}.identity{gap:8px}.identity strong{font-size:18px}.task-title{display:none}.top-actions{gap:4px}.delivery-main{padding:20px 14px 32px}.task-heading h1{font-size:22px}.stage-nav{margin:20px 0;gap:0}.stage-nav button{min-height:76px;flex-direction:column;gap:5px;font-size:13px;padding:8px 2px;position:relative}.stage-nav small{display:none}.stage-number{width:21px;height:21px;font-size:12px}.photo-grid{grid-template-columns:repeat(2,minmax(0,1fr));gap:8px}.toolbar{gap:8px}.toolbar .el-input{width:100%}.toolbar .el-select{flex:1;min-width:110px}.section-heading{align-items:flex-start;gap:12px;flex-direction:column}.section-heading h2{white-space:nowrap}.section-heading .inline-actions{gap:8px}.inline-actions{gap:4px;flex-wrap:wrap}.photo-info{padding:4px 7px;font-size:12px}.photo-meta{padding:0 7px 8px;font-size:11px}.form-row{grid-template-columns:1fr;gap:0}.action-bar{gap:8px}.action-bar .el-button{margin-left:0;min-height:44px}.delivery-app :deep(.el-checkbox){min-height:44px}.delivery-app :deep(.el-dialog__body){overflow-x:hidden}.task-lifecycle{flex-wrap:wrap}.history-list li,.match-row{flex-direction:column;align-items:flex-start}.match-row .el-select{width:100%}.top-actions :deep(.el-button){padding:8px;min-height:40px}.task-grid{grid-template-columns:1fr}}
@media(max-width:360px){.delivery-top{padding:10px 12px;gap:8px}.identity strong{font-size:16px}.top-actions :deep(.theme-switcher){display:none}.top-actions :deep(.language-switcher){width:82px}.top-actions :deep(.el-button){width:44px;min-width:44px;height:44px;padding:0;font-size:0}.top-actions :deep(.el-button .el-icon){font-size:18px}}
</style>
