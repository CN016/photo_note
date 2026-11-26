import { describe, expect, it } from 'vitest'
import { createEditorDocument } from '../../../../packages/editor-core/src'
import { exportSourceKey, hasExportVisualChanges, originalExportFormat } from './exportImage'

describe('原图导出决策', () => {
  it('无编辑时直接复用原图字节', () => {
    expect(hasExportVisualChanges(createEditorDocument(), false)).toBe(false)
  })

  it('旋转、裁切、调色、锚点和水印任一存在时都需要重新渲染', () => {
    expect(hasExportVisualChanges({ ...createEditorDocument(), rotation: 90 }, false)).toBe(true)
    expect(hasExportVisualChanges({ ...createEditorDocument(), crop: { x: .1, y: 0, width: .9, height: 1 } }, false)).toBe(true)
    expect(hasExportVisualChanges({ ...createEditorDocument(), rgb: { red: 1, green: 0, blue: 0 } }, false)).toBe(true)
    expect(hasExportVisualChanges({ ...createEditorDocument(), anchors: [{ id: 'a', x: .5, y: .5, note: '主体' }] }, false)).toBe(true)
    expect(hasExportVisualChanges(createEditorDocument(), true)).toBe(true)
  })

  it('优先使用独立原图 key，缺失时回退工作图', () => {
    expect(exportSourceKey({ ...createEditorDocument(), source_image_key: ' local_original.jpg ' }, 'local_work.jpg')).toBe('local_original.jpg')
    expect(exportSourceKey(createEditorDocument(), 'local_work.jpg')).toBe('local_work.jpg')
  })

  it('直出原图保留原始格式，文件名无扩展名时按 MIME 回退', () => {
    expect(originalExportFormat(new Blob([], { type: 'image/png' }), 'photo.jpg')).toEqual({ mime: 'image/png', extension: 'png' })
    expect(originalExportFormat(new Blob([], { type: 'image/heic' }), 'photo')).toEqual({ mime: 'image/heic', extension: 'heic' })
  })
})
