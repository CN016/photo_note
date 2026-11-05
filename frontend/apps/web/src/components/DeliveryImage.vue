<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from '../api/client'
const props = defineProps<{ task: string; photo: string; filename: string; token?: string; code?: string; large?: boolean; retryLabel: string }>()
const root = ref<HTMLElement>(); const src=ref(''); const failed=ref(false)
let observer: IntersectionObserver | undefined; let generation=0; let visible=false
/** 仅在进入视口时取私有预览，卸载释放对象 URL，迟到响应不可写入另一张照片。 */
async function load(){
 const stamp=++generation;failed.value=false
 try{const blob=props.token ? await api.deliveryPublicFile(props.token,props.code||'',props.photo,false,!props.large) : await api.deliveryFile(props.task,props.photo,false,!props.large)
  if(stamp!==generation)return; if(src.value)URL.revokeObjectURL(src.value);src.value=URL.createObjectURL(blob)
 }catch{if(stamp===generation)failed.value=true}
}
onMounted(()=>{observer=new IntersectionObserver(entries=>{if(entries.some(v=>v.isIntersecting)){visible=true;observer?.disconnect();void load()}},{rootMargin:'240px'});if(root.value)observer.observe(root.value)})
watch(()=>[props.task,props.photo,props.token,props.code],()=>{generation++;if(src.value)URL.revokeObjectURL(src.value);src.value='';if(visible)void load()})
onBeforeUnmount(()=>{generation++;observer?.disconnect();if(src.value)URL.revokeObjectURL(src.value)})
</script>
<template><div ref="root" class="delivery-image" :class="{large}"><img v-if="src" :src="src" :alt="filename" decoding="async"/><el-button v-else-if="failed" @click.stop="load">{{ retryLabel }}</el-button><el-skeleton v-else animated :rows="1" /></div></template>
<style scoped>.delivery-image{width:100%;aspect-ratio:4/3;display:grid;place-items:center;background:var(--el-fill-color-light);overflow:hidden}.delivery-image img{width:100%;height:100%;object-fit:contain}.delivery-image.large{aspect-ratio:auto;height:65dvh;background:#16191d}.delivery-image.large img{max-height:65dvh}.delivery-image :deep(.el-skeleton){width:60%;}</style>
