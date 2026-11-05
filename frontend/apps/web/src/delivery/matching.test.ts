import { describe,expect,it } from 'vitest'
import { matchOriginals,selectionCSV } from './matching'
import { localOriginalZip } from './localZip'
describe('交付原片匹配',()=>{
 it('完整名优先，扩展名不同按主名匹配，歧义不自动选择',()=>{
  const files=[new File(['a'],'A.JPG'),new File(['b'],'A.ARW'),new File(['c'],'B.CR3'),new File(['d'],'C.ARW'),new File(['e'],'C.CR3')]
  const matches=matchOriginals(['A.JPG','B.jpg','C.jpg','D.jpg'].map((filename,i)=>({business_id:String(i),filename})),files)
  expect(matches[0]!.file?.name).toBe('A.JPG');expect(matches[1]!.file?.name).toBe('B.CR3');expect(matches[2]!.file).toBeUndefined();expect(matches[2]!.candidates).toHaveLength(2);expect(matches[3]!.candidates).toHaveLength(0)
 })
 it('清单保留逗号和引号并防止表格公式',()=>{expect(selectionCSV([{business_id:'id',filename:'=危险,"图".jpg',group_name:'组'}])).toContain('"\'=危险,""图"".jpg"')})
 it('本地 ZIP 中央目录与文件内容一致',async()=>{
  const blob=await localOriginalZip([{name:'id/a.jpg',file:new File(['original'],'a.jpg')}]);const bytes=new Uint8Array(await blob.arrayBuffer());const view=new DataView(bytes.buffer)
  expect(view.getUint32(0,true)).toBe(0x04034b50);expect(new TextDecoder().decode(bytes)).toContain('original');expect(view.getUint32(bytes.length-22,true)).toBe(0x06054b50);expect(view.getUint16(bytes.length-12,true)).toBe(1)
 })
})
