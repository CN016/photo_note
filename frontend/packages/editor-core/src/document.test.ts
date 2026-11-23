import { describe, expect, it } from 'vitest'
import { anchorToView, clampAnchorLabelScale, createEditorDocument, parseEditorDocument, viewToSource } from './document'
import { DocumentHistory } from './history'

describe('editor document geometry', () => {
  it('maps source anchors through rotation and returns to their original position', () => {
    const doc = { ...createEditorDocument(), rotation: 90 as const }
    const anchor = { x: 0.2, y: 0.7 }
    const view = anchorToView(anchor, doc)
    expect(view.x).toBeCloseTo(0.3)
    expect(view.y).toBeCloseTo(0.2)
    expect(view.visible).toBe(true)
    expect(viewToSource(view, doc).x).toBeCloseTo(anchor.x)
    expect(viewToSource(view, doc).y).toBeCloseTo(anchor.y)
  })

  it('hides anchors outside the stored source-space crop and clamps invalid documents', () => {
    const doc = { ...createEditorDocument(), crop: { x: 0.25, y: 0.25, width: 0.5, height: 0.5 } }
    expect(anchorToView({ x: 0.1, y: 0.1 }, doc).visible).toBe(false)
    expect(parseEditorDocument({ rotation: 45, rgb: { red: 1000 }, anchors: [{ x: 2, y: -1, note: ' detail ' }] })).toMatchObject({
      rotation: 0, rgb: { red: 100 }, anchors: [{ x: 1, y: 0, note: ' detail ' }],
    })
  })

  it('reads anchor label scale within a readable range and leaves legacy anchors untouched', () => {
    const parsed = parseEditorDocument({
      anchors: [
        { id: 'a', x: 0.5, y: 0.5, note: '主体', label_scale: 1.37 },
        { id: 'b', x: 0.5, y: 0.5, note: '背景', label_scale: 9 },
        { id: 'c', x: 0.5, y: 0.5, note: '旧数据' },
        { id: 'd', x: 0.5, y: 0.5, note: '异常值', label_scale: 'huge' },
      ],
    })
    expect(parsed.anchors[0]?.label_scale).toBe(1.35)
    expect(parsed.anchors[1]?.label_scale).toBe(2)
    expect(parsed.anchors[2]).not.toHaveProperty('label_scale')
    expect(parsed.anchors[3]).not.toHaveProperty('label_scale')
    expect(clampAnchorLabelScale(0.2)).toBe(0.75)
    expect(clampAnchorLabelScale(Number.NaN)).toBe(1)
  })
})

describe('document history', () => {
  it('undoes and redoes metadata edits without copying image pixels', () => {
    const history = new DocumentHistory(createEditorDocument())
    history.commit({ ...history.value, rotation: 90 })
    expect(history.undo().rotation).toBe(0)
    expect(history.redo().rotation).toBe(90)
  })
})
