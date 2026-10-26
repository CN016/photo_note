<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { LedgerRecord } from '../api/types'
import { amountToFen, formatFen } from './ledgerUtils'
/** 使用父页面已授权并筛选的正式记录；历史分析不改变保存及草稿语义。 */
const props = defineProps<{ records: LedgerRecord[]; month: string }>()
const emit = defineEmits<{ month: [value: string] }>()
const { t, locale } = useI18n()
const money = (n:number) => formatFen(n,locale.value)
const percent = (n:number) => new Intl.NumberFormat(locale.value,{style:'percent',maximumFractionDigits:1}).format(n)
/** 按整数分独立累计收入与支出，不用净额代替流水。 */
function group(rows:LedgerRecord[], key:(r:LedgerRecord)=>string) {
 const map = new Map<string,{key:string;income:number;expense:number;net:number;count:number}>()
 for(const r of rows){if(r.status!=='complete')continue;const k=key(r);const item=map.get(k)??{key:k,income:0,expense:0,net:0,count:0};item[r.type]+=amountToFen(r.amount)??0;item.net=item.income-item.expense;item.count++;map.set(k,item)}
 return [...map.values()]
}
const rows = computed(()=>props.records.filter(r=>r.date.startsWith(props.month)))
const total = computed(()=>group(rows.value,()=> 'total')[0]??{income:0,expense:0,net:0,count:0})
const days = computed(()=>group(rows.value,r=>r.date))
const best = computed(()=>[...days.value].sort((a,b)=>b.income-a.income)[0])
const years = computed(()=>group(props.records,r=>r.date.slice(0,4)).sort((a,b)=>a.key.localeCompare(b.key)))
const ranking = computed(()=>group(props.records,r=>r.date.slice(0,7)).sort((a,b)=>b.net-a.net||a.key.localeCompare(b.key)).slice(0,10))
const flags = computed(()=>group(rows.value,r=>r.flag||'ordinary'))
const metrics = computed(()=>{
 const [y,m]=props.month.split('-').map(Number)
 const previous=new Date(y,m-2,1);const key=`${previous.getFullYear()}-${String(previous.getMonth()+1).padStart(2,'0')}`
 const prior=group(props.records.filter(r=>r.date.startsWith(key)),()=> 'total')[0]?.income??0
 return [
 {label:'averageTicket',value:money(total.value.count?Math.round((total.value.income+total.value.expense)/total.value.count):0)},
 {label:'bestDay',value:best.value?`${best.value.key} · ${money(best.value.income)}`:'—'},
 {label:'margin',value:total.value.income?percent(total.value.net/total.value.income):t('ledger.analytics.noBase')},
 {label:'coverage',value:percent(days.value.length/new Date(y,m,0).getDate())},
 {label:'growth',value:prior?percent((total.value.income-prior)/prior):t('ledger.analytics.newBase')}]
})
/** 每天保留累计结余，包括无记录日；精确值通过可展开表格读取。 */
const cumulative=computed(()=>{const [y,m]=props.month.split('-').map(Number);let sum=0;return Array.from({length:new Date(y,m,0).getDate()},(_,i)=>{const key=`${props.month}-${String(i+1).padStart(2,'0')}`;sum+=days.value.find(d=>d.key===key)?.net??0;return{key,net:sum}})})
const insights=computed(()=>{
 if(!rows.value.length)return []
 const lines:string[]=[]
 for(const type of ['income','expense'] as const){const top=group(rows.value.filter(r=>r.type===type),r=>r.category||t('ledger.record.ordinary')).sort((a,b)=>b[type]-a[type])[0];if(top&&total.value[type])lines.push(t(`ledger.analytics.${type}Share`,{name:top.key,share:percent(top[type]/total.value[type])}))}
 const avg=days.value.length?total.value.income/days.value.length:0
 lines.push(t('ledger.analytics.aboveAverage',{count:days.value.filter(d=>d.income>=avg).length,amount:money(Math.round(avg))}))
 if(total.value.income)lines.push(t('ledger.analytics.expenseRatio',{share:percent(total.value.expense/total.value.income)}))
 return lines
})
/** 条形按绝对规模绘制，负数同时显示符号与赤字文字。 */
function width(n:number,values:number[]){return `${Math.abs(n)/Math.max(1,...values.map(Math.abs))*100}%`}
</script>
<template>
<div class="analytics"><p class="scope">{{ t('ledger.analytics.scope') }}</p><div class="metrics"><article v-for="item in metrics" :key="item.label"><span>{{ t(`ledger.analytics.${item.label}`) }}</span><strong>{{ item.value }}</strong></article></div><div class="panels">
<article><h3>{{ t('ledger.analytics.yearComparison') }}</h3><div class="scroll"><table><thead><tr><th></th><th>{{ t('ledger.filters.income') }}</th><th>{{ t('ledger.filters.expense') }}</th><th>{{ t('ledger.analytics.net') }}</th></tr></thead><tbody><tr v-for="year in years" :key="year.key"><th>{{ year.key }}</th><td>{{ money(year.income) }}</td><td>{{ money(year.expense) }}</td><td>{{ money(year.net) }}</td></tr></tbody></table></div><p v-if="!years.length">{{ t('ledger.analytics.empty') }}</p></article>
<article><h3>{{ t('ledger.analytics.ranking') }}</h3><button v-for="row in ranking" :key="row.key" type="button" class="rank" @click="emit('month',row.key)"><span>{{ row.key }}</span><span class="track"><i :style="{width:width(row.net,ranking.map(r=>r.net))}"/></span><b>{{ money(row.net) }}</b></button><p v-if="!ranking.length">{{ t('ledger.analytics.empty') }}</p></article>
<article><h3>{{ t('ledger.analytics.cumulative') }}</h3><div class="cumulative"><div v-for="row in cumulative" :key="row.key" :title="`${row.key} · ${money(row.net)}`"><i :class="{loss:row.net<0}" :style="{height:width(row.net,cumulative.map(r=>r.net))}"/><small>{{ Number(row.key.slice(-2)) }}</small></div></div><details><summary>{{ t('ledger.analytics.amount') }}</summary><table><tbody><tr v-for="row in cumulative" :key="row.key"><th>{{ row.key }}</th><td>{{ money(row.net) }}</td></tr></tbody></table></details></article>
<article><h3>{{ t('ledger.analytics.insights') }}</h3><p v-for="line in insights" :key="line">{{ line }}</p><p v-if="!insights.length">{{ t('ledger.analytics.empty') }}</p><h3>{{ t('ledger.analytics.flags') }}</h3><p v-for="flag in flags" :key="flag.key">{{ t(`ledger.record.${flag.key==='pending_check'?'pendingCheck':flag.key}`) }} · {{ t('ledger.calendar.records',{count:flag.count}) }} · +{{ money(flag.income) }} / −{{ money(flag.expense) }}</p></article>
</div></div>
</template>
<style scoped>
.analytics{margin-top:18px}.scope{font-size:12px;color:var(--muted);line-height:1.7}.metrics{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:10px}.metrics article,.panels article{padding:18px;background:var(--surface);border:1px solid var(--line);border-radius:var(--radius-card);min-width:0}.metrics span{display:block;color:var(--muted);font-size:12px}.metrics strong{display:block;margin-top:10px;font-size:16px;overflow-wrap:anywhere}.panels{display:grid;grid-template-columns:1fr 1fr;gap:13px;margin-top:13px}.panels h3{font-size:14px;margin:0 0 14px}.panels p{font-size:12px;line-height:1.8;color:var(--ink-soft)}table{width:100%;border-collapse:collapse;font-size:12px}td,th{padding:10px 5px;border-bottom:1px solid var(--line);text-align:right;white-space:nowrap}th:first-child{text-align:left}.scroll{overflow:auto}.rank{display:flex;align-items:center;gap:12px;width:100%;min-height:44px;border:0;background:transparent;color:var(--ink-soft);cursor:pointer;font-size:12px}.track{flex:1;height:6px;background:var(--surface-muted);border-radius:5px}.track i{display:block;height:100%;background:var(--accent);border-radius:5px}.rank:focus-visible{outline:2px solid var(--accent)}.cumulative{display:flex;gap:3px;height:170px;padding-bottom:24px}.cumulative>div{display:flex;position:relative;flex:1;align-items:flex-end;min-width:0}.cumulative i{width:100%;background:var(--success);min-height:1px}.cumulative i.loss{background:var(--danger)}.cumulative small{position:absolute;bottom:-20px;font-size:8px;color:var(--muted)}summary{font-size:12px;cursor:pointer;color:var(--accent)}@media(max-width:700px){.panels{grid-template-columns:1fr}.metrics{grid-template-columns:1fr 1fr}.metrics article{padding:13px}.metrics strong{font-size:13px}}
</style>
