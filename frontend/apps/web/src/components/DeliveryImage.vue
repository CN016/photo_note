<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { loadDeliveryImage } from '../images/detailImages'
const props = defineProps<{ task: string; photo: string; filename: string; token?: string; code?: string; large?: boolean; retryLabel: string }>()
const root = ref<HTMLElement>(); const src=ref(''); const failed=ref(false)
let generation=0
/** 列表主动预取每张完整预览，大图复用同一请求；公私入口继续独立授权，迟到结果不串图。 */
async function load(){
 const stamp=++generation;failed.value=false
 try{const detail=loadDeliveryImage(props.task,props.photo,props.token,props.code);void detail.catch(()=>{})
  const blob=props.large ? await detail : await loadDeliveryImage(props.task,props.photo,props.token,props.code,true)
  if(stamp!==generation)return; if(src.value)URL.revokeObjectURL(src.value);src.value=URL.createObjectURL(blob)
 }catch{if(stamp===generation)failed.value=true}
}
onMounted(()=>{void load()})
watch(()=>[props.task,props.photo,props.token,props.code,props.large],()=>{generation++;if(src.value)URL.revokeObjectURL(src.value);src.value='';void load()})
onBeforeUnmount(()=>{generation++;if(src.value)URL.revokeObjectURL(src.value)})
</script>
<template><div ref="root" class="delivery-image" :class="{large}"><img v-if="src" :src="src" :alt="filename" decoding="async"/><el-button v-else-if="failed" @click.stop="load">{{ retryLabel }}</el-button><span v-else aria-hidden="true"></span></div></template>
<style scoped>.delivery-image{width:100%;aspect-ratio:4/3;display:grid;place-items:center;background:var(--el-fill-color-light);overflow:hidden}.delivery-image img{width:100%;height:100%;object-fit:contain}.delivery-image.large{aspect-ratio:auto;height:65dvh;background:#16191d}.delivery-image.large img{max-height:65dvh}.delivery-image :deep(.el-skeleton){width:60%;}</style>
