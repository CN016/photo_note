import { describe, expect, it } from 'vitest'
import { Schema } from '@tiptap/pm/model'
import { EditorState } from '@tiptap/pm/state'
import { moveResourceTransaction } from './resourceMovementTransaction'

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
