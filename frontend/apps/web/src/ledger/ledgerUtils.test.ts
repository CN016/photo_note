import { describe, expect, it } from 'vitest'
import { amountToFen, parseLedgerCSV, parseLedgerJSON } from './ledgerUtils'

describe('账本金额和导入解析', () => {
  it('使用整数分解析非负两位小数金额', () => {
    expect(amountToFen('0')).toBe(0)
    expect(amountToFen('12.3')).toBe(1230)
    expect(amountToFen('12.30')).toBe(1230)
    expect(amountToFen('-1')).toBeNull()
    expect(amountToFen('1.001')).toBeNull()
  })

  it('将旧营收日历 JSON 映射为收入并保留日期和可识别字段', () => {
    const parsed = parseLedgerJSON(JSON.stringify({
      '2026-10-14': [
        { id: 'old-id', amount: 128.5, category: '门店', method: '微信支付', time: '09:20', note: '早市' },
      ],
    }))
    expect(parsed.invalid).toEqual([])
    expect(parsed.records).toEqual([{
      date: '2026-10-14', time: '09:20', type: 'income', amount: '128.5', category: '门店',
      payment_method: 'wechat', flag: '', note: '早市',
    }])
  })

  it('兼容中文 CSV 表头、带逗号备注和支出类型', () => {
    const parsed = parseLedgerCSV('日期,时间,类型,金额,分类,支付方式,标记,备注\r\n2026-10-15,18:05,支出,32.80,采购,现金,待核对,"纸张,墨水"')
    expect(parsed.invalid).toEqual([])
    expect(parsed.records[0]).toMatchObject({
      date: '2026-10-15', time: '18:05', type: 'expense', amount: '32.80', category: '采购',
      payment_method: 'cash', flag: 'pending_check', note: '纸张,墨水',
    })
  })

  it('拒绝无效日期和超过两位小数的导入行', () => {
    const parsed = parseLedgerJSON(JSON.stringify([
      { date: '2026-02-30', amount: '1.00' },
      { date: '2026-02-28', amount: '1.001' },
    ]))
    expect(parsed.records).toHaveLength(0)
    expect(parsed.invalid).toHaveLength(2)
  })
})
