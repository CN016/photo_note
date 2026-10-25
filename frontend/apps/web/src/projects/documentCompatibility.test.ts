import { describe, expect, it } from 'vitest'
import { Schema } from '@tiptap/pm/model'
import { isDocumentCompatible } from './documentCompatibility'

// 只构建文档契约涉及的最小 schema，验证拒绝丢失内容，而不是依赖浏览器渲染。
const schema = new Schema({
  nodes: {
    doc: { content: 'block+' },
    paragraph: { group: 'block', content: 'text*' },
    text: {},
    inspirationImage: { group: 'block', atom: true, attrs: { src: {}, width: { default: 100 }, layout: { default: 'original' } } },
    projectAttachment: { group: 'block', atom: true, attrs: { storekey: {}, fileName: { default: '' } } },
    taskList: { group: 'block', content: 'taskItem+' },
    taskItem: { content: 'paragraph+', attrs: { checked: { default: false } } },
  },
  marks: { italic: {}, textStyle: { attrs: { fontSize: { default: null } } } },
})
const document = { type: 'doc', content: [
  { type: 'paragraph', content: [{ type: 'text', text: '中文', marks: [{ type: 'italic' }, { type: 'textStyle', attrs: { fontSize: '36px' } }] }] },
  { type: 'inspirationImage', attrs: { src: 'inspiration://fixture', width: 30, layout: 'small' } },
  { type: 'projectAttachment', attrs: { storekey: 'local_fixture', fileName: 'fixture.pdf' } },
  { type: 'taskList', content: [{ type: 'taskItem', attrs: { checked: true }, content: [{ type: 'paragraph' }] }] },
] }

describe('项目正文兼容边界', () => {
  it('保留媒体引用、宽度、文字标记与勾选状态并可重复序列化', () => {
    expect(isDocumentCompatible(document, schema)).toBe(true)
    expect(schema.nodeFromJSON(document).toJSON()).toEqual(document)
  })
  it.each([
    { type: 'unsupportedNode' },
    { type: 'paragraph', attrs: { unsupportedAttribute: 'value' } },
    { type: 'paragraph', content: [{ type: 'text', text: 'text', marks: [{ type: 'unsupportedMark' }] }] },
    { type: 'paragraph', content: [{ type: 'text', text: 'text', marks: [{ type: 'italic', attrs: { unsupported: true } }] }] },
    { type: 'taskList', content: [{ type: 'paragraph' }] },
  ])('未知内容或非法嵌套时拒绝编辑：%j', node => {
    expect(isDocumentCompatible({ type: 'doc', content: [node] }, schema)).toBe(false)
  })
})
