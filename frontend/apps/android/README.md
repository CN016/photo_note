# 取景台 Android：随包内置 GeckoView

应用名称：取景台；应用 ID：`love.imcn016.photo`；最低 Android 9 / API 28；目标 SDK 36；编译 SDK 37.2。

原计划的 `love.016imcn.photo` 因包名段以数字开头未通过校验，首次建包时改为上述有效标识。用户要求消除旧系统 WebView 的运行依赖后，最终采用 APK 内置 GeckoView，不使用 Capacitor 或系统 WebView 作为运行内核。

## 环境与构建

- Node.js 22+、npm 10+、pnpm。前端依赖使用已有 pnpm 锁文件。
- JDK 21，Windows 优先探测 Android Studio 标准安装位置的 JBR；其他位置设置 `PHOTO_NINI_JAVA_HOME`。
- Android SDK 37.2 / Build Tools 36.0.0，设置 `ANDROID_HOME`；Windows 默认探测 `%LOCALAPPDATA%/Android/Sdk`。
- GeckoView `158.0.20261021001945`、AGP `9.3.1`、Gradle `9.5.0`，依赖解析结果和 npm 锁文件随仓库提交。Wrapper 校验官方 Gradle 发行包 SHA-256。

在 `frontend/apps/web/` 执行 `pnpm install --frozen-lockfile`，在本目录执行：

```powershell
npm ci
npm run build
```

流程：检查工具链 → 构建共享 Web → 同步为内置扩展资源 → `assembleDebug` 和 `lintDebug` → 输出 `artifacts/photo-nini-<version>-<abi>-debug.apk` 与 `.sha256`。任一步失败即停止。`npm run icons` 重新生成图标；Android Studio 可直接打开 `native/` 工程。

API 地址读取现有 Web 的 `.env` / `.env.production` 配置或 `VITE_API_BASE_URL` 环境变量，必须使用 HTTPS。本地配置、SDK 路径、安装包、日志、密钥及缓存不提交。从1.1.0开始支持签名Web资源更新；首次启用及内核/原生桥接升级仍需构建APK，Gecko内核始终随APK交付。动态更新详见 [发布与更新说明](../../../docs/android-web-updates.md)。

## 内置浏览器与安全边界

- [GeckoView](https://mozilla.github.io/geckoview/) 是随 APK 发布的独立内核。Android 9 即使自带 WebView 69，也不要求用户更新该组件。包内包含 ARM64、ARMv7、x86_64 原生库，分别输出三种架构的独立安装包，均包含完整内核；普通手机优先使用 ARM64 包，旧 32 位手机使用 ARMv7，模拟器使用 x86_64。
- 固定ID的内置扩展只负责桥接和旧来源数据迁移。共享Web由应用私有资源服务在127.0.0.1提供；固定端口保持来源，随机HttpOnly凭据限制读取，Java再次验证扩展ID、当前会话、顶层及精确来源。外部网页不能进入此可信容器。
- 安卓API经本机同源入口流式转发到APK固定HTTPS后端，保留已有路径前缀；不转发本机Cookie，不允许客户端指定任意上游或跟随重定向。后端鉴权、租户规则及CORS保持不变。外网明文访问不在资源CSP允许范围内；Android网络安全配置只允许127.0.0.1明文。本机TCP服务不监听局域网。完整真实账号业务仍需验收。
- 外部页面交给系统浏览器，不允许进入带原生权限的容器。不开放原生网络或文件任意路径接口，禁用远程调试、浏览器控制台输出和系统备份/迁移。
- Gecko 内核从 Mozilla 官方 Maven 获取，未修改第三方二进制；MPL-2.0、源码定位与第三方声明包含在 `native/app/src/main/assets/licenses/`。升级内核时需同步依赖锁、许可证、功能及最低系统回归。

## 文件与系统交互

文件导出使用 SAF 系统创建文件或只读 URI 分享，不申请全盘存储权限、不覆盖原始素材。原生导出限制单文件 32 MiB，避免 Base64 桥接导致不可控内存峰值；超限提示缩小图片尺寸。`shared` 仅表示系统分享面板已打开，不声称接收方送达。

分享缓存按独立批次保存，后续导出时清理超过一天的旧批次；也可由系统回收或在系统设置清理缓存。没有后续导出时不保证一天后主动删除。

返回键优先关闭浮层和输入焦点，再经过路由守卫，首页回到系统。系统栏与输入法由原生容器避让，主题跟随 Web 偏好。文件输入通过系统选择器，取消不会作为上传失败；本批未实现拍照和目录选择。

## 测试与正式签名

前端在 Web 目录执行 `pnpm typecheck`、`pnpm test`。原生测试使用独立模拟器：

```powershell
cd native
./gradlew.bat :app:connectedDebugAndroidTest
```

原生测试只使用合成 JSON、CSV 和 4096×4096 画布，不需要生产账号。测试专用扩展仅存在于调试构建，其 ID 与生产扩展不同，不能获得生产桥接授权。系统文件目录可能创建 `photo-nini-smoke.json`。

正式构建：`npm run build -- --release`；需外部提供 `PHOTO_NINI_KEYSTORE`、`PHOTO_NINI_STORE_PASSWORD`、`PHOTO_NINI_KEY_ALIAS`、`PHOTO_NINI_KEY_PASSWORD`。不生成生产密钥、不复用调试签名，不包含商店上架流程。版本来源为 `package.json` 的 `version` 和 `androidVersionCode`，后续覆盖升级必须递增安装版本。

AGP 9.3.1 会提示其测试范围止于 SDK 37.1，本批以 Gecko 要求的 SDK 37.2 实际完成构建和 Lint，未隐藏该提示。

本批实际验证结果、工具链提示及未覆盖流程见 `docs/20261025-安卓工程与打包.md`。

## APK 新版本提醒

Android 1.2.0 起，应用首次打开及在前台每小时检查官网下载版本；后台使用系统 JobScheduler 的一小时周期（五分钟弹性窗口）保存检查结果，回到前台后显示原生更新弹窗。省电、无网络、强制停止或厂商限制可能延后后台检查；版本检查本身不申请精确闹钟或悬浮窗权限；1.3.0 新增消息通知权限，见下节。

官网配置读取 `website/.env` / `.env.production` 的 `VITE_SITE_URL` 与 `VITE_ANDROID_ASSET_BASE`。当前官网为 `https://www.016imcn.love`，版本记录为 `/releases/android/release.json`，升级按钮通过外部浏览器打开 `/download/`。可用构建环境变量 `PHOTO_NINI_APK_RELEASE_URL`、`PHOTO_NINI_DOWNLOAD_PAGE_URL` 覆盖同官网源下的路径；必须为无凭据 HTTPS 地址。

APK 提醒按 `versionCode` 比较，并检查 debug/release 渠道。同一版本一小时内只提示一次；“稍后再说”不升级、不退出登录，“前往官网下载”只打开外部网页，不自动下载或安装。版本检查失败不阻碍编辑或清理登录数据。Web 签名热更新仍使用原有独立机制，不因为 Web 页面更新而要求重新安装 APK。

发新版时同时递增壳 `package.json` 的 `version` 和 `androidVersionCode`，先构建 APK，再构建官网；官网插件自动生成 `schema/version/versionCode/channel`，不能手工只改清单而不更新安装包。版本记录应使用 `Cache-Control: no-cache`，路径缺失应返回 404，不回退 HTML。

## 系统通知与后台消息监听（1.3.0）

登录后默认启用系统通知和原生 HTTP 监听，不增加应用配置。Android 13+ 首次登录申请通知权限；拒绝后不反复弹窗，可在系统设置重新开启。原生前台服务使用低重要性的“消息监听”常驻通知，每 30 秒检查现有 `/api/v1/notifications`，网络失败按 60、120、240、300 秒退避。首次建立基线不重放历史；后续变化合并为“你有新消息”，点击进入受登录保护的消息中心。通知不包含站内正文、图片或内部诊断。

1.4.0 起恢复历史 Web HTTP 自动登录：网页直接调用 `/auth/refresh`，原生代理沿用原有 Web 加密凭据文件，不等待通知桥接。业务页面加载后异步申请 `/auth/notification-session`，原生通知在独立线程、Keystore 别名、文件及刷新锁中管理独立 token。通知失败不刷新或清除 Web 凭据；Web 退出和空间切换会使关联通知会话失效。通知 token 仅可读取通知、未读数和退出自身，不可访问普通业务接口。

后台使用 dataSync 前台服务，不属于远程厂商推送，不保证永久在线或严格的 30 秒时效。Android 15+（当前 target 36）后台累计同步服务受 24 小时内 6 小时时限约束；到期立即停止，不循环重启；再次打开应用并保持有效登录时尝试恢复。省电、Doze、厂商策略、断网、系统回收、强制停止或用户停止服务均可能中断/延迟监听。没有开机自动启动通知监听、精确闹钟、唤醒锁或绕过电池限制。新原生能力必须安装 1.3.0 APK，单独部署 Web 热更新不能升级旧壳。

## 启动恢复与进入时检查（1.3.1）

每次从后台进入应用，原生版本检测会异步主动读取官网清单，不再被小时检查缓存跳过；已有请求在途时合并，前台小时检查和后台周期任务继续保留。同一版本一小时内不重复弹窗，“稍后再说”不会因重新检查而失效。检查不阻塞Web启动、登录或编辑，不自动下载/安装APK。

Web 首个路由等待健康检查或鉴权时显示恢复状态。1.4.0 已移除通知认证桥接对 Web 登录的依赖；保留历史 HTTP 自动刷新与原有凭据恢复路径，通知不参与首次路由鉴权。进程终止后 Access Token 不落盘，联网恢复仍取决于正常后端响应。

本批签名 Web 更新要求 `minShellCode=11`，旧 1.3.x 壳必须安装 1.4.0 APK；不能把新 Web 单独下发到仍共用凭据的旧壳。先部署 4.10.0 API，再部署 Web 与官网安装包。已有凭据如已被旧版冲突轮换撤销，需要重新登录一次，不能恢复已被服务端撤销的 token。
