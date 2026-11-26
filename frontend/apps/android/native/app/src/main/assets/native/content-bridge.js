/* Gecko隔离世界接收消息；Java再次核对扩展ID、会话、顶层及精确本机来源。 */
(() => {
  if (window !== window.top || location.hostname !== '127.0.0.1') return;
  let port;
  function connect() {
    if (!port) {
      port = browser.runtime.connectNative('photo_nini');
      port.onMessage.addListener(message => window.postMessage({ channel: 'photo-nini-native-response', event: message }, location.origin));
    }
    return port;
  }
  window.addEventListener('message', async event => {
    if (event.source !== window || event.origin !== location.origin) return;
    const data = event.data;
    if (data?.channel === 'photo-nini-native-connect') {
      connect();
      return;
    }
    if (data?.channel === 'photo-nini-native-event') {
      if (data.type !== 'pageReady') return;
      connect().postMessage({ type: 'pageReady' });
      return;
    }
    if (data?.channel !== 'photo-nini-native-request' || !Number.isSafeInteger(data.id)) return;
    if (!['exportFile', 'setTheme', 'minimizeApp', 'independentNotificationSessions', 'startNotificationMonitor', 'stopNotificationMonitor', 'consumeNotificationOpen'].includes(data.message?.method)) return;
    try {
      const value = await browser.runtime.sendNativeMessage('photo_nini', data.message);
      window.postMessage({ channel: 'photo-nini-native-response', id: data.id, value }, location.origin);
    } catch (error) {
      const raw = error instanceof Error ? error.message : '';
      const code = raw.replace(/^Error:\s*/, '');
      window.postMessage({ channel: 'photo-nini-native-response', id: data.id, error: /^[a-z0-9_]+$/.test(code) ? code : 'native_failed' }, location.origin);
    }
  });
})();
