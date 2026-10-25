import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import { fileURLToPath } from "node:url";

// 两个真实 HTML 入口保证普通静态服务器可直接访问下载页，无需 SPA 回退。
export default defineConfig({
  plugins: [vue()],
  build: {
    rollupOptions: {
      input: {
        home: fileURLToPath(new URL("./index.html", import.meta.url)),
        download: fileURLToPath(
          new URL("./download/index.html", import.meta.url),
        ),
      },
    },
  },
});
