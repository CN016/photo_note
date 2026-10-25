// 仅调试构建包含的合成页面，不访问生产账号，也没有生产原生桥接授权。
const report = data => browser.runtime.sendNativeMessage('engine_test', data)
document.getElementById('canvas').addEventListener('click', async () => {
  const canvas = document.createElement('canvas')
  canvas.width = canvas.height = 4096
  const context = canvas.getContext('2d')
  context.fillStyle = '#2675e8'
  context.fillRect(0, 0, 4096, 4096)
  const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'))
  const bitmap = await createImageBitmap(blob)
  const result = { type: 'canvas', width: bitmap.width, height: bitmap.height, bytes: blob.size }
  bitmap.close()
  canvas.width = canvas.height = 0
  await report(result)
})
report({ type: 'ready', userAgent: navigator.userAgent })
