import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import LedgerAnalytics from './LedgerAnalytics.vue'
import en from '../i18n/ledger.en-US.json'
import zh from '../i18n/ledger.zh-CN.json'
import type { LedgerRecord } from '../api/types'

/** 使用服务端渲染验证实际面板，覆盖历史年度、草稿排除及零收入边界。 */
async function render(records: Partial<LedgerRecord>[], language = 'en-US') {
 const app = createSSRApp(LedgerAnalytics, { month: '2026-10', records: records.map((r,i) => ({business_id:String(i),date:'2026-10-14',type:'income',amount:'100',category:'Portrait',flag:'',status:'complete',...r})) as LedgerRecord[] })
 app.use(createI18n({legacy:false,locale:language,messages:{'en-US':{ledger:en},'zh-CN':{ledger:zh}}}))
 return renderToString(app)
}
describe('收支扩展分析',()=>{
 it('跨年汇总并保留同一天多笔收入，排除草稿',async()=>{
 const html=await render([{amount:'100'},{amount:'200'},{type:'expense',amount:'50'},{date:'2025-03-01',amount:'80'},{amount:'9999',status:'draft'}])
 expect(html).toContain('2025');expect(html).toContain('2025-03');expect(html).toContain('300.00');expect(html).toContain('250.00');expect(html).not.toContain('9,999');expect(html).toContain('83.3%')
 })
 it('只有支出时显示赤字且不产生无穷比例',async()=>{
 const html=await render([{type:'expense',amount:'150'}])
 expect(html).toContain('No income; ratio unavailable');expect(html).toContain('-');expect(html).not.toContain('Infinity');expect(html).not.toContain('NaN')
 })
 it('空数据中英文都有明确提示且键集合相同',async()=>{
 expect(await render([])).toContain('No completed records to analyze')
 expect(await render([],'zh-CN')).toContain('暂无可分析的正式记录')
 const keys=(value:object,prefix=''):string[]=>Object.entries(value).flatMap(([key,v])=>v&&typeof v==='object'?keys(v,`${prefix}${key}.`):[`${prefix}${key}`])
 expect(keys(zh).sort()).toEqual(keys(en).sort())
 })
})
