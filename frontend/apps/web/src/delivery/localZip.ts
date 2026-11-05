/** 本地原片导出为无压缩 ZIP，保留用户选定文件；限制 256MiB 防止移动浏览器内存峰值。
 * 不复制到用户目录、不修改原件。超出上限时仍可导出匹配清单，交给本地工具处理。
 */
export async function localOriginalZip(items: {name:string;file:File}[]):Promise<Blob>{
 if(items.reduce((n,v)=>n+v.file.size,0)>256*1024*1024)throw new Error('local_export_too_large')
 const table=new Uint32Array(256)
 for(let n=0;n<256;n++){let c=n;for(let i=0;i<8;i++)c=(c&1)?0xedb88320^(c>>>1):c>>>1;table[n]=c>>>0}
 const chunks:BlobPart[]=[];const central:Uint8Array<ArrayBuffer>[]=[];let offset=0
 const encoder=new TextEncoder()
 for(const item of items){
  const name=encoder.encode(item.name.replaceAll('\\','/').split('/').filter(p=>p&&p!=='.'&&p!=='..').join('/'))
  const bytes=new Uint8Array(await item.file.arrayBuffer());let crc=0xffffffff
  for(const b of bytes)crc=table[(crc^b)&255]!^(crc>>>8);crc=(crc^0xffffffff)>>>0
  const local=new Uint8Array(30+name.length);const lv=new DataView(local.buffer)
  lv.setUint32(0,0x04034b50,true);lv.setUint16(4,20,true);lv.setUint16(6,0x800,true);lv.setUint32(14,crc,true);lv.setUint32(18,bytes.length,true);lv.setUint32(22,bytes.length,true);lv.setUint16(26,name.length,true);local.set(name,30)
  const cd=new Uint8Array(46+name.length);const cv=new DataView(cd.buffer)
  cv.setUint32(0,0x02014b50,true);cv.setUint16(4,20,true);cv.setUint16(6,20,true);cv.setUint16(8,0x800,true);cv.setUint32(16,crc,true);cv.setUint32(20,bytes.length,true);cv.setUint32(24,bytes.length,true);cv.setUint16(28,name.length,true);cv.setUint32(42,offset,true);cd.set(name,46)
  chunks.push(local,item.file);central.push(cd);offset+=local.length+bytes.length
 }
 const length=central.reduce((n,v)=>n+v.length,0);const end=new Uint8Array(22);const ev=new DataView(end.buffer)
 ev.setUint32(0,0x06054b50,true);ev.setUint16(8,items.length,true);ev.setUint16(10,items.length,true);ev.setUint32(12,length,true);ev.setUint32(16,offset,true)
 return new Blob([...chunks,...central,end],{type:'application/zip'})
}
