// 仅在首次升级时从原扩展来源迁移本应用偏好及登录状态，不上传、不记录内容。
const values = {};
for (let index = 0; index < localStorage.length; index++) {
  const key = localStorage.key(index);
  if (key?.startsWith('photo-nini.') || key?.startsWith('photo-nini-')) values[key] = localStorage.getItem(key);
}
browser.runtime.sendNativeMessage('photo_nini', { method: 'migrateLegacyStorage', options: values }).catch(() => {});
