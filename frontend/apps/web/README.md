# 取景台 Web

Vue 3 + TypeScript + Vite 的共享 Web 前端。后端主机必须通过构建环境变量 `VITE_API_BASE_URL` 提供，Vite 会在构建时将其写入静态资源；该值只包含协议、主机和可选端口，不包含 `/api/v1` 等接口路径。

```powershell
Copy-Item .env.example .env.local
# VITE_API_BASE_URL 用于配置 Vite 开发代理目标；如需单独指定代理目标，可设置 VITE_DEV_API_PROXY_TARGET
pnpm install
pnpm dev
```

生产构建示例：

```powershell
$env:VITE_API_BASE_URL = 'https://api.example.com'
pnpm build
```

`VITE_API_BASE_URL` 必须是 `http://` 或 `https://` 服务主机源，不能包含 API 路径、查询参数或片段；不要在路径中包含 token 或其他秘密。前端 API 客户端在代码中拼接 `/api/v1`，健康检查在服务主机根路径请求 `/healthz`。开发环境使用 Vite 代理转发这两个路径，代理目标默认取 `VITE_API_BASE_URL`，也可由 `VITE_DEV_API_PROXY_TARGET` 覆盖。生产环境的同源反向代理需要将 `/api/v1` 和 `/healthz` 转发到后端；跨域直连要求后端配置允许前端来源访问 API 和 `/healthz` 的 CORS 响应头。构建产物不会在运行时再次读取主机环境变量，变更 API 主机后需要重新构建。

本项目按需加载 HEIC WASM 回退解码器；运行时第三方许可信息见 `public/licenses/THIRD_PARTY_NOTICES.txt`。
