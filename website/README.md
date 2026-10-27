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

客户端发布信息同样使用环境变量，无需修改源码。平台前缀分别是 `VITE_WINDOWS`、`VITE_MACOS`、`VITE_ANDROID`、`VITE_IOS`，每个平台提供 `_STATUS`、`_VERSION`、`_ARCHITECTURE`、`_DOWNLOAD_URL`。真实发布后设置 `_STATUS=available` 并填写其余三项；完整示例见 `.env.example`。配置不完整时不显示下载链接。未配置正式发布信息时，通用卡片显示“即将推出”；安卓随站测试包模式见下文，小程序仍为“规划中”。

## 静态部署

将 `dist/` 全部内容放到独立域名根目录，启用目录默认文件 `index.html`，使 `/download/` 返回 `download/index.html`。无需 SPA 回退，也不需要后端代理。不要只上传首页 HTML。当前不支持部署到 URL 子目录。

正式发布前配置域名与 Web 地址，重新构建并检查两页、语言切换、下载状态及缓存更新。本次仅交付本地工程，不执行发布。

设计与维护说明见 [官网设计与维护](../docs/website-design.md)，协作要求见 [AGENTS.md](AGENTS.md)。第三方许可随 `public/licenses/` 一并打包。

Vite 在启动开发服务或构建时读取环境变量；更改后需要重启开发服务或重新构建并部署。服务器上只修改环境变量不会改变已经构建的静态文件。CI 可直接注入同名变量，优先于 .env 文件。本机 .env 被 Git 忽略，不提交个人配置。

## 安卓测试包与本批部署顺序

官网已支持随站点托管安卓调试签名测试包。启用 `.env` 的以下公开配置：

```dotenv
VITE_ANDROID_BUNDLED_DOWNLOADS=true
VITE_ANDROID_ASSET_BASE=/releases/android
```

先在 `frontend/apps/android/` 执行 `npm ci`、`npm run build`，得到架构 APK 与 `.sha256`（官网仅分发 ARM 64 位和 32 位）；已有本批完整产物时无需重复打包。随后在 `website/` 执行 `pnpm build`。构建插件读取安卓版本，对每个源 APK 计算 SHA-256 并与校验文件比较，复制到 `dist/releases/android/` 后再次校验；缺失或不一致时构建失败。页面从同一元数据生成版本、架构、大小与校验入口，不使用猜测的公网地址。生成的 `release.json` 也随静态文件部署。

本模式独立于 `VITE_ANDROID_STATUS=available` 的正式发布配置，明确标为测试版，开启时替换普通 Android 状态卡。后续采用正式下载地址时先关闭 `VITE_ANDROID_BUNDLED_DOWNLOADS`，再配置正式发布四项。关闭该开关也可构建不带本地 APK 的官网；设置后须重新构建。不要只上传首页 HTML，也不要遗漏 `releases/`。APK 和 dist 均被 Git 忽略，不提交安装包。

部署时将官网 `dist/` 的**全部内容**上传到官网 HTTPS 根目录，下载页面为 `/download/`。常用手机选择 ARM64，旧 32 位手机选 ARMv7，x86_64 用于模拟器。Nginx 配置示例见 `deploy/nginx-downloads.conf.example`：APK 使用正确 MIME，缺失文件返回 404，禁止回退成首页 HTML。若修改下载基础路径，需同步修改该 location。使用 CDN 时同步发布完整文件后再刷新页面缓存。

业务 Web 与官网分别部署：`frontend/apps/web/dist/` 上传到现有 Web 应用站点；`website/dist/` 上传到官网站点。两份 dist 不要互相覆盖。业务 Web 如需同步此前安卓适配相关的共享前端改动，可在 `frontend/apps/web/` 执行普通 `pnpm build` 后部署；Android 构建使用的专用目标不代替正常 Web 构建。本批官网修改不影响 APK 内置页面，已有安卓包无需为下载入口重新构建；后端无改动，不需要重建或重启。

上传后检查：`/`、`/download/`、三个 APK 的实际下载、校验文件，以及不存在的 APK 返回 404。服务器文件已上传不代表下载验收完成；本批只完成本地构建与预览，未连接生产服务器部署。

部署后可执行 `pnpm check:downloads https://你的官网域名/releases/android/release.json` 验证完整文件字节、MIME、SHA-256、校验文件和缺包 404；该检查会读取两份完整安装包，产生相应下载流量。

### 安卓每小时版本检查

`VITE_SITE_URL` 当前配置为 `https://www.016imcn.love`。同源 APK 构建插件在 `releases/android/release.json` 中自动写入 `schema: 1`、`versionCode`、版本、渠道和已校验下载文件，兼容既有 `version/channel/minAndroid/variants` 字段。原生客户端比较安装版本号，并将“升级”固定跳转到官网下载页，不使用清单携带的任意外链。

发布顺序为：递增 Android 壳版本和安装版本号 → 构建 APK → 构建/部署官网，保留 `releases/android/` 完整文件。版本记录建议 `Cache-Control: no-cache`，不可长期 immutable 缓存；不要只修改 JSON 制造不存在的版本。现有同源下载为调试签名测试包，不作为正式商店版本。
