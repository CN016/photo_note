// 仅调试包使用合成触控验证真实生产主体组件，不读取账号或表单内容。
(async () => {
  for (let i = 0; i < 150; i++) {
    const v = document.querySelector('.content-zoom-viewport');
    if (!v || !document.documentElement.classList.contains('native-content-zoom')) { await new Promise(r => setTimeout(r, 200)); continue; }
    const surface = v.querySelector('.content-zoom-surface');
    const main = v.querySelector('main');
    if (!main || !surface || !surface.style.width) { await new Promise(r => setTimeout(r, 200)); continue; }
    const header = document.createElement('div'), footer = document.createElement('div');
    header.style.cssText = 'height:60px;width:100%;position:fixed;top:0;left:0;pointer-events:none';
    footer.style.cssText = 'height:64px;width:100%;position:fixed;bottom:0;left:0;pointer-events:none';
    document.body.append(header, footer);
    const dimensions = element => { const r = element.getBoundingClientRect(); return { width:r.width, height:r.height }; };
    const events = [];
    const record = e => { if(events.length < 40) events.push({type:e.type,count:e.touches?.length,pointer:e.pointerType,target:e.target.tagName,x:e.clientX ?? e.touches?.[0]?.clientX,y:e.clientY ?? e.touches?.[0]?.clientY}); };
    for(const type of ['touchstart','touchmove','touchend','touchcancel','pointerdown','pointermove','pointerup','pointercancel']) document.addEventListener(type,record,true);
    const before = { header: dimensions(header), footer: dimensions(footer), main: dimensions(main) };
    await browser.runtime.sendNativeMessage('zoom_test',{type:'ready'});
    // 由 UIAutomator 注入真正的安卓双指触控，页面脚本只记录布局，不自行触发事件。
    await new Promise(resolve => {
      const listener = () => { if (Number(surface.style.zoom || 1) > 1.1) { v.removeEventListener('touchend', listener); resolve(); } };
      v.addEventListener('touchend', listener);
      setTimeout(() => { v.removeEventListener('touchend', listener); resolve(); }, 15000);
    });
    await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));
    await browser.runtime.sendNativeMessage('zoom_test',{ type:'result', events, viewportRect:dimensions(v), supported:CSS.supports('zoom','2'), computed:getComputedStyle(surface).zoom, surfaceWidth:surface.getBoundingClientRect().width, firstChild:dimensions(main.firstElementChild), before, after:{header:dimensions(header),footer:dimensions(footer),main:dimensions(main)}, scale:visualViewport.scale, zoom:surface.style.zoom, pageWidth:document.documentElement.scrollWidth, viewportWidth:innerWidth });
    for(const type of ['touchstart','touchmove','touchend','touchcancel','pointerdown','pointermove','pointerup','pointercancel']) document.removeEventListener(type,record,true);
    header.remove();footer.remove();return;
  }
  await browser.runtime.sendNativeMessage('zoom_test',{error:'production_content_not_ready'});
})()
