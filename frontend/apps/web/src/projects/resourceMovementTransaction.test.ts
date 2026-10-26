import { describe, expect, it } from 'vitest'
import { Schema } from '@tiptap/pm/model'
import { EditorState } from '@tiptap/pm/state'
import { moveResourceTransaction, moveResourceToPositionTransaction } from './resourceMovementTransaction'

const schema = new Schema({ nodes: {
  doc: { content: 'block+' }, text: {}, paragraph: { group: 'block', content: 'text*' },
  inspirationImage: { group: 'block', atom: true, attrs: { src: {}, width: { default: 30 } } },
  projectAttachment: { group: 'block', atom: true, attrs: { storekey: {} } },
} })
const a = schema.node('inspirationImage', { src: 'inspiration://a', width: 30 })
const b = schema.node('inspirationImage', { src: 'inspiration://b', width: 50 })
const file = schema.node('projectAttachment', { storekey: 'private-fixture' })
const paragraph = schema.node('paragraph', null, schema.text('文字'))

// 使用真实 ProseMirror state 验证交换完整性，防止节点视图旧 props 导致媒体被截断。
describe('媒体相邻排序事务', () => {
  it.each([[a, b], [a, file], [file, paragraph]])('后移和前移互逆并完整保留节点', (first, second) => {
    const state = EditorState.create({ schema, doc: schema.node('doc', null, [first, second]) })
    const next = moveResourceTransaction(state, 0, 1)!
    expect(next.doc.toJSON()).toEqual(schema.node('doc', null, [second, first]).toJSON())
    const restored = moveResourceTransaction(state.apply(next), second.nodeSize, -1)!
    expect(restored.doc.toJSON()).toEqual(state.doc.toJSON())
  })
  it('连续排序可移动到首尾且不丢失任何媒体', () => {
    let state = EditorState.create({ schema, doc: schema.node('doc', null, [a, b, file, paragraph]) })
    state = state.apply(moveResourceTransaction(state, 0, 1)!)
    state = state.apply(moveResourceTransaction(state, b.nodeSize, 1)!)
    state = state.apply(moveResourceTransaction(state, b.nodeSize + file.nodeSize, 1)!)
    expect(state.doc.toJSON()).toEqual(schema.node('doc', null, [b, file, paragraph, a]).toJSON())
    expect(moveResourceTransaction(state, b.nodeSize + file.nodeSize + paragraph.nodeSize, 1)).toBeNull()
    expect(moveResourceTransaction(state, 0, -1)).toBeNull()
  })
})

// 拖动与相邻交换共用文档模型，但允许一次跨过多个块；验证两方向和无效目标。
describe('触屏媒体拖动事务', () => {
  const state = EditorState.create({ schema, doc: schema.node('doc', null, [a, paragraph, b, file]) })
  it('跨多个块拖到末尾并保留所有属性及节点选择', () => {
    const tr = moveResourceToPositionTransaction(state, 0, state.doc.content.size)!
    expect(tr.doc.toJSON()).toEqual(schema.node('doc', null, [paragraph, b, file, a]).toJSON())
    expect(tr.selection.from).toBe(paragraph.nodeSize + b.nodeSize + file.nodeSize)
    const restored = moveResourceToPositionTransaction(state.apply(tr), tr.selection.from, 0)!
    expect(restored.doc.toJSON()).toEqual(state.doc.toJSON())
  })
  it('拒绝拖到自身、文本内部、越界或非媒体来源', () => {
    expect(moveResourceToPositionTransaction(state, 0, 0)).toBeNull()
    expect(moveResourceToPositionTransaction(state, 0, 1)).toBeNull()
    expect(moveResourceToPositionTransaction(state, 0, 2)).toBeNull()
    expect(moveResourceToPositionTransaction(state, 0, -1)).toBeNull()
    expect(moveResourceToPositionTransaction(state, 0, state.doc.content.size + 1)).toBeNull()
    expect(moveResourceToPositionTransaction(state, 1, state.doc.content.size)).toBeNull()
  })
})
