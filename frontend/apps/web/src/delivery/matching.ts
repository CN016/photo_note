import type { DeliveryPhoto } from '../api/delivery'
/** 文件名匹配不猜测冲突；完整名优先，去扩展名用于 JPG 对 RAW，同名目录必须人工选定。 */
export function matchOriginals(photos: (Pick<DeliveryPhoto,'business_id'|'filename'> & {group_name?:string})[], files: File[]) {
 const normalize=(s:string)=>s.normalize('NFC').toLocaleLowerCase('en-US')
 const stem=(s:string)=>normalize(s.replace(/\.[^.]+$/,''))
 return photos.map(photo=>{
  const exact=files.filter(f=>normalize(f.name)===normalize(photo.filename))
  const grouped=photo.group_name?exact.filter(f=>normalize(f.webkitRelativePath).endsWith(normalize(`${photo.group_name}/${photo.filename}`))):[]
  const candidates=grouped.length ? grouped : exact.length ? exact : files.filter(f=>stem(f.name)===stem(photo.filename))
  return {photo,candidates,file:candidates.length===1?candidates[0]:undefined}
 })
}

/** 导出清单使用 RFC4180 引号并防止表格公式执行，不把文件名当作命令行参数。 */
export function selectionCSV(photos: Pick<DeliveryPhoto,'business_id'|'filename'|'group_name'>[]) {
 const cell=(s:string)=>'"'+(/^[=+\-@\t\r]/.test(s)?"'"+s:s).replaceAll('"','""')+'"'
 return '\ufeff'+['business_id,filename,group',...photos.map(p=>[p.business_id,p.filename,p.group_name].map(cell).join(','))].join('\r\n')
}
