/* 固定APK桥接客户端：只在本机顶层页面工作，不向外部页面开放系统能力。 */
(() => {
  if (window !== window.top || location.hostname !== '127.0.0.1') return;
  let sequence = 0;
  const pending = new Map();
  const listeners = new Set();
  window.addEventListener('message', event => {
    if (event.source !== window || event.origin !== location.origin || event.data?.channel !== 'photo-nini-native-response') return;
    const data = event.data;
    if (data.event) { for (const callback of listeners) callback(data.event); return; }
    const task = pending.get(data.id);
    if (!task) return;
    pending.delete(data.id); clearTimeout(task.timer);
    if (data.error) task.reject(new Error(data.error)); else task.resolve(data.value);
  });
  const runtime = {
    id: 'photo-nini@imcn016.love',
    sendNativeMessage(app, message) {
      if (app !== 'photo_nini' || !['exportFile', 'setTheme', 'minimizeApp'].includes(message?.method)) return Promise.reject(new Error('unsupported_method'));
      if (pending.size >= 8) return Promise.reject(new Error('native_busy'));
      return new Promise((resolve, reject) => {
        const id = ++sequence;
        const timer = setTimeout(() => { pending.delete(id); reject(new Error('native_timeout')); }, 300000);
        pending.set(id, { resolve, reject, timer });
        window.postMessage({ channel: 'photo-nini-native-request', id, message }, location.origin);
      });
    },
    connectNative(app) {
      if (app !== 'photo_nini') throw new Error('unsupported_method');
      window.postMessage({ channel: 'photo-nini-native-connect' }, location.origin);
      return { onMessage: { addListener(callback) { listeners.add(callback); } } };
    },
  };
  Object.defineProperty(window, 'browser', { value: Object.freeze({ runtime: Object.freeze(runtime) }), configurable: false });
})();
