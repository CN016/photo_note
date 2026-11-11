import { describe, expect, it } from 'vitest'
import { imageNote } from './imageNote'
import { parseEditorDocument } from '../../../../packages/editor-core/src'

describe('整图备注兼容性', () => {
  it('旧数据、空白与异常类型不生成列表备注', () => {
    for (const data of [undefined, {}, { image_note: '  ' }, { image_note: 8 }, { image_note: { text: '不可信类型' } }]) expect(imageNote(data)).toBe('')
  })
  it('旧文档解析保留备注和其他扩展字段，不影响锚点备注', () => {
    const document = parseEditorDocument({ schema_version: 1, image_note: '  逆光 人像  ', custom: { keep: true }, anchors: [{ id: 'a', x: .5, y: .5, note: '主体' }] })
    expect(imageNote(document)).toBe('逆光 人像')
    expect(document.custom).toEqual({ keep: true })
    expect(document.anchors[0]?.note).toBe('主体')
    expect(JSON.parse(JSON.stringify(document)).image_note).toBe('  逆光 人像  ')
  })
})
