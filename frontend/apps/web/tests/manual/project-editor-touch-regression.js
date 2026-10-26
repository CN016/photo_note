// 浏览器打开同目录 HTML 后执行此脚本；覆盖重复图片拖动、附件拖动与取消。
(async()=>{
 const run=async(selector,text,after,cancel=false)=>{
  const source=document.querySelector(selector);const start=source.getBoundingClientRect();
  const target=Array.from(document.querySelectorAll('.ProseMirror > p')).find(e=>e.textContent===text).getBoundingClientRect();
  const send=(type,x,y)=>{const touch=new Touch({identifier:82,target:source,clientX:x,clientY:y,pageX:x+scrollX,pageY:y+scrollY});(type==='touchstart'?source:document).dispatchEvent(new TouchEvent(type,{bubbles:true,cancelable:true,touches:type==='touchend'||type==='touchcancel'?[]:[touch],changedTouches:[touch]}))};
  send('touchstart',start.left+10,start.top+10);await new Promise(r=>setTimeout(r,500));
  send('touchmove',target.left+50,after?target.bottom-1:target.top+1);await new Promise(r=>setTimeout(r,750));
  const active=source.classList.contains('is-touch-dragging');const preview=document.querySelector('.project-resource-drop-indicator')?.hidden===false;
  send(cancel?'touchcancel':'touchend',target.left+50,after?target.bottom-1:target.top+1);
  await new Promise(r=>setTimeout(r,50));return {active,preview,cleaned:!document.querySelector('.project-resource-drag-ghost')};
 };
 const initialFocus=window.focusEvents;
 const results=[];
 results.push(await run('.inspiration-node','Before media',false));
 results.push(await run('.inspiration-node','Last paragraph',true));
 results.push(await run('.attachment-node','Before media',false));
 const before=JSON.stringify(window.testEditor.getJSON());
 results.push(await run('.attachment-node','Last paragraph',true,true));
 if(results.some(r=>!r.active||!r.preview||!r.cleaned)) throw new Error('媒体长按、落点预览或清理回归失败');
 if(before!==JSON.stringify(window.testEditor.getJSON())) throw new Error('取消拖动改变了文档');
 if(window.focusEvents!==initialFocus) throw new Error('拖动使正文获得了焦点');
 return {results,cancelPreserved:before===JSON.stringify(window.testEditor.getJSON()),focusEvents:window.focusEvents,order:window.testEditor.getJSON().content.map(n=>n.type)};
})()