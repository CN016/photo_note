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

API 地址读取现有 Web 的 `.env` / `.env.production` 配置或 `VITE_API_BASE_URL` 环境变量，必须使用 HTTPS。本地配置、SDK 路径、安装包、日志、密钥及缓存不提交。前端更新和内核更新均须重新构建 APK，不增加热更新或内核运行时下载。

## 内置浏览器与安全边界

- [GeckoView](https://mozilla.github.io/geckoview/) 是随 APK 发布的独立内核。Android 9 即使自带 WebView 69，也不要求用户更新该组件。包内包含 ARM64、ARMv7、x86_64 原生库，分别输出三种架构的独立安装包，均包含完整内核；普通手机优先使用 ARM64 包，旧 32 位手机使用 ARMv7，模拟器使用 x86_64。
- 共享 Web 作为固定 ID 的内置扩展页运行；仅向构建时配置的 HTTPS API 主机授予网络权限。扩展页面通过原生消息调用文件和主题能力，原生端验证扩展 ID、当前会话、顶层 frame 与来源前缀；不信任普通网页传入的身份字段。
- 访问 API 使用 Gecko 的标准 fetch/XHR 与扩展主机权限，不修改既有服务授权、租户逻辑或后端 CORS。生产 OPTIONS 曾返回 405，扩展主机授权使安卓访问无需依赖 Web 页面的跨域预检配置。完整登录、上传下载与空间切换仍需账号验收。
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
