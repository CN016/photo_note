import { describe, expect, it } from 'vitest'
import type { ProjectShooting } from '../api/types'
import {
  deliveryPrefill,
  hasCompleteShooting,
  projectNextActionKey,
  scheduleTitlePreview,
  shootingTimeLabel,
} from './workflowPresentation'

const shooting: ProjectShooting = {
  start_at: '2026-10-22T01:00:00Z',
  end_at: '2026-10-22T03:00:00Z',
  location: ' 江边公园 ',
  person: ' 九九 ',
  kind: 'photo',
}

describe('策划工作流展示', () => {
  it('只有时间、地点、人物和类型齐全时才允许自动建档期', () => {
    expect(hasCompleteShooting(shooting)).toBe(true)
    expect(hasCompleteShooting({ ...shooting, end_at: null })).toBe(false)
    expect(hasCompleteShooting({ ...shooting, location: ' ' })).toBe(false)
  })

  it('按状态给出明确的下一步入口', () => {
    expect(projectNextActionKey('unfinished')).toBe('projects.nextAction.execute')
    expect(projectNextActionKey('executed')).toBe('projects.nextAction.writeReview')
    expect(projectNextActionKey('completed')).toBe('projects.nextAction.openReview')
  })

  it('在北京时间下展示拍摄时段', () => {
    expect(shootingTimeLabel(shooting, 'zh-CN')).toContain('10月22日')
    expect(shootingTimeLabel(shooting, 'zh-CN')).toContain('09:00')
    expect(shootingTimeLabel(shooting, 'zh-CN')).toContain('11:00')
  })

  it('档期预览复用日期、人物和拍摄类型', () => {
    expect(scheduleTitlePreview(shooting, 'zh-CN', '照片')).toBe('2026年10月22日九九照片')
  })

  it('进入选片时只带入标题、人物称呼和拍摄摘要', () => {
    expect(deliveryPrefill(' 江边写真 ', shooting)).toEqual({
      title: '江边写真',
      customer: '九九',
      description: expect.stringContaining('江边公园'),
    })
  })
})
