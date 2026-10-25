import { Extension, Node, mergeAttributes } from '@tiptap/core'
import { VueNodeViewRenderer } from '@tiptap/vue-3'
import InspirationImageNodeView from './views/InspirationImageNodeView.vue'
import ProjectAttachmentNodeView from './views/ProjectAttachmentNodeView.vue'

/** 仅允许常用像素字号，JSON 结构序列化时保留字号而不保存任意 HTML。 */
export const FontSize = Extension.create({
  name: 'fontSize',
  addGlobalAttributes() {
    return [{ types: ['textStyle'], attributes: {
      fontSize: {
        default: null,
        parseHTML: element => element.style.fontSize || null,
        renderHTML: attributes => {
          const value = attributes.fontSize
          return typeof value === 'string' && /^(8|9|1[0-9]|2[0-4]|3[0-6]|4[8])px$/.test(value) ? { style: `font-size: ${value}` } : {}
        },
      },
    } }]
  },
})

/** 文档只保存 inspiration://业务 UUID；客户端通过 API 异步读取图片，不嵌入文件地址。 */
export const InspirationImage = Node.create({
  name: 'inspirationImage',
  group: 'block mediaBlock',
  atom: true,
  draggable: true,
  selectable: true,
  addAttributes() { return { src: { default: null }, width: { default: 100 }, layout: { default: 'original' } } },
  parseHTML() { return [{ tag: 'figure[data-inspiration]' }] },
  renderHTML({ HTMLAttributes }) {
    return ['figure', mergeAttributes({ 'data-inspiration': HTMLAttributes.src, 'data-width': HTMLAttributes.width, 'data-layout': HTMLAttributes.layout }, HTMLAttributes)]
  },
  addNodeView() { return VueNodeViewRenderer(InspirationImageNodeView) },
})

/** 非灵感图资源作为受控附件节点保存 storekey，下载和预览仍需项目接口授权。 */
export const ProjectAttachment = Node.create({
  name: 'projectAttachment',
  group: 'block mediaBlock',
  atom: true,
  draggable: true,
  selectable: true,
  addAttributes() {
    return {
      storekey: { default: null }, fileName: { default: '' }, contentType: { default: '' },
      fileSize: { default: 0 }, width: { default: 100 }, layout: { default: 'original' },
    }
  },
  parseHTML() { return [{ tag: 'figure[data-project-attachment]' }] },
  renderHTML({ HTMLAttributes }) {
    return ['figure', mergeAttributes({
      'data-project-attachment': 'true', 'data-storekey': HTMLAttributes.storekey,
      'data-filename': HTMLAttributes.fileName, 'data-content-type': HTMLAttributes.contentType,
      'data-width': HTMLAttributes.width, 'data-layout': HTMLAttributes.layout,
    }, HTMLAttributes)]
  },
  addNodeView() { return VueNodeViewRenderer(ProjectAttachmentNodeView) },
})
