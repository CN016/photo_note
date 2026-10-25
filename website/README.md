# 取景台官网

独立的 Vue 3 + TypeScript + Vite 静态多页工程：首页 `/`，下载页 `/download/`。不需要后端、数据库或登录。

## 开发与构建

要求 Node.js 22.12+（或 20.19+）与 pnpm。进入 website 后运行：

```powershell
pnpm install --frozen-lockfile
pnpm dev
pnpm build
pnpm preview
```

开发默认 5173，生产预览默认 4173，端口被占用时以终端实际输出为准。`build` 包含 TypeScript 与中英文键检查。可分别运行 `pnpm typecheck`、`pnpm check:i18n`。

## 公开入口配置

复制 `.env.example` 为 `.env`（`Copy-Item .env.example .env`），其中 `VITE_WEB_APP_URL=https://photo.016imcn.love` 为用户提供的 Web 入口；不填或地址无效时显示“入口待开放”。`VITE_SITE_URL` 为正式官网地址，用于 canonical；不填则不输出。仅接受不带用户名密码的 HTTPS 地址。构建变量公开可见，禁止存放秘密；修改后重新构建。

客户端发布信息同样使用环境变量，无需修改源码。平台前缀分别是 `VITE_WINDOWS`、`VITE_MACOS`、`VITE_ANDROID`、`VITE_IOS`，每个平台提供 `_STATUS`、`_VERSION`、`_ARCHITECTURE`、`_DOWNLOAD_URL`。真实发布后设置 `_STATUS=available` 并填写其余三项；完整示例见 `.env.example`。配置不完整时不显示下载链接。首版四个平台均为“即将推出”，小程序为“规划中”。

## 静态部署

将 `dist/` 全部内容放到独立域名根目录，启用目录默认文件 `index.html`，使 `/download/` 返回 `download/index.html`。无需 SPA 回退，也不需要后端代理。不要只上传首页 HTML。当前不支持部署到 URL 子目录。

正式发布前配置域名与 Web 地址，重新构建并检查两页、语言切换、下载状态及缓存更新。本次仅交付本地工程，不执行发布。

设计与维护说明见 [官网设计与维护](../docs/website-design.md)，协作要求见 [AGENTS.md](AGENTS.md)。第三方许可随 `public/licenses/` 一并打包。

Vite 在启动开发服务或构建时读取环境变量；更改后需要重启开发服务或重新构建并部署。服务器上只修改环境变量不会改变已经构建的静态文件。CI 可直接注入同名变量，优先于 .env 文件。本机 .env 被 Git 忽略，不提交个人配置。
