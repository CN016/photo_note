import { describe, expect, it } from 'vitest'
import { addDateKey, chinaDateKey, clampScheduleDays, monthCalendarKeys, mondayOf, toChinaTimestamp } from './scheduleUtils'

describe('schedule date helpers', () => {
  it('keeps date arithmetic independent from browser timezone', () => {
    expect(addDateKey('2026-10-31', 1)).toBe('2026-11-01')
    expect(mondayOf('2026-10-18')).toBe('2026-10-12')
    expect(chinaDateKey(new Date('2026-10-14T17:00:00.000Z'))).toBe('2026-10-15')
  })

  it('builds a six-week Monday-first month calendar', () => {
    const days = monthCalendarKeys('2026-10')
    expect(days).toHaveLength(42)
    expect(days[0]).toBe('2026-09-28')
    expect(days[3]).toBe('2026-10-01')
    expect(days[41]).toBe('2026-11-08')
  })

  it('bounds custom calendar ranges and marks inputs as China Standard Time', () => {
    expect(clampScheduleDays(0)).toBe(1)
    expect(clampScheduleDays(120)).toBe(90)
    expect(toChinaTimestamp('2026-10-15T09:30')).toBe('2026-10-15T09:30:00+08:00')
  })
})
