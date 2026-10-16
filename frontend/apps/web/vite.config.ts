import { defineConfig } from 'vitest/config'
import { loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import { fileURLToPath } from 'node:url'

const heicPackage = new URL('./node_modules/@su-engineering/heic/dist/', import.meta.url)

/** 开发时将同源 API 路径代理到本机后端，避免依赖尚未配置的跨域响应头。 */
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const apiTarget = env.VITE_DEV_API_PROXY_TARGET || env.VITE_API_BASE_URL || 'http://127.0.0.1:8089'
  return {
    plugins: [
      vue(),
      AutoImport({ resolvers: [ElementPlusResolver()] }),
      Components({ resolvers: [ElementPlusResolver({ importStyle: 'css' })] }),
    ],
    resolve: {
      alias: [
        { find: /^exifr$/, replacement: fileURLToPath(new URL('./node_modules/exifr/dist/full.esm.mjs', import.meta.url)) },
        { find: /^@su-engineering\/heic\/wasm$/, replacement: fileURLToPath(new URL('wasm.js', heicPackage)) },
        { find: /^@su-engineering\/heic$/, replacement: fileURLToPath(new URL('index.js', heicPackage)) },
      ],
    },
    server: {
      host: '0.0.0.0',
      port: 5173,
      proxy: {
        '/api/v1': { target: apiTarget, changeOrigin: true },
        '/api/v2': { target: apiTarget, changeOrigin: true },
        '/healthz': { target: apiTarget, changeOrigin: true },
      },
    },
    test: { environment: 'node', include: ['src/**/*.test.ts', '../../packages/**/*.test.ts'] },
  }
})
