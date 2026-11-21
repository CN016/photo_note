# LightNote 品牌与 Android 后台恢复优化

## 改动目的

将用户可见产品名统一为 `LightNote`，更新业务 Web、官网、Android 壳、通知文案和分发文件名；同时修复 Android 应用从桌面重新进入时长时间停留在空白页面或启动等待页的问题。应用 ID、存储键、原生桥接 ID 和数据库名称保持不变，避免破坏已有安装升级、登录态与数据兼容。

## 主要内容

- 业务 Web 与官网的中文、英文品牌名、标题、登录页、启动页、服务状态、版权和下载文案统一为 `LightNote`。
- 后端通知与 webhook 测试消息同步使用 `LightNote`，后端版本在并行版本 `4.12.0` 之上递增为 `4.12.1`。
- Web 与官网新增统一的 `Ln` 图标资产、favicon 和 Apple Touch Icon；官网头部、页脚与业务 Web 品牌入口使用同一图标。
- Android 图标使用纯色背景 `#001D34` 和大写 `L`、小写 `n` 的 `#2FA3F7` 几何字形，采用高识别度创意应用图标风格；重新生成各密度、自适应、单色和启动屏资源。
- Android 壳版本更新为 `1.4.1` / `versionCode 12`，显示名改为 `LightNote`；APK 文件名改为 `lightnote-<version>-<abi>-debug.apk`，应用 ID 保持 `love.imcn016.photo`。
- Web 包启动改为本地优先：`prepareLocal` 只校验本地稳定包或上次完整下载的 ready 包，不访问网络；远端清单检查和下载在页面建立原生连接后后台执行。
- 后台下载的新 Web 包只标记为 ready，在下一次冷启动切换并完成挂载确认，不再阻塞当前启动，也不会自动打断正在编辑的页面。
- Android `onStart`、`onResume` 和 `onPostResume` 补齐 Gecko 激活及首帧重绘；已连接页面从后台返回时隐藏原生启动页。
- 原生启动页不再因 Web 端口早连而立即隐藏：共享 Web 在 Vue 挂载并完成两次动画帧后发送 `pageReady`，原生层收到明确首帧信号后才撤掉启动页。

## 验证方式及结果

- 业务 Web：`pnpm test` 通过，34 个测试文件、141 项测试；`pnpm typecheck` 通过。
- 官网：`pnpm typecheck`、`pnpm check:i18n`、配置检查和 `pnpm build` 通过。
- 后端：`go test ./internal/service ./internal/httpapi` 通过。
- Android：`testDebugUnitTest`、`lintDebug`、`assembleDebug` 通过；API 28 模拟器执行 `connectedDebugAndroidTest`，29 项通过。
- Android 新增测试验证本地启动不请求远端清单，并验证后台下载包只在下次本地启动切换。
- API 28 模拟器实测：后台 15 秒后恢复 `TotalTime` 为 169ms，页面内容保持可见；强制回收进程后启动时持续显示品牌启动页，直到共享 Web 提交首帧后才退场，不再出现无反馈纯白屏。
- 官网在 Chromium 中检查中英文首页、下载页、`LightNote` 品牌与 Android 下载入口；390px 视口无横向溢出，axe WCAG 2A/2AA 自动检查 0 违规。

## 尚未验证与已知限制

- 本机没有 Android release keystore 和密码环境，因此本批 APK 为调试签名测试包，不能直接覆盖生产签名安装包；正式发布前需要使用既有发布签名重新构建。
- 未执行生产 Web/官网部署、CDN 刷新、真实账号登录和真实后端数据验收。
- iOS、PC 和微信小程序目录当前只有占位文件，没有可构建应用；本批未生成这些平台安装包。
- Android 冷启动仍需要启动 Gecko 内核，低性能设备上会显示品牌启动页；本次修复的是无反馈白屏和联网更新阻塞首屏，不代表冷启动可以瞬时完成。
