# 官网协作规范

- 本目录是独立的官网工程，不承载编辑器、账户、上传或业务 API；遵守根 AGENTS.md。
- 首页与下载页使用真实 HTML 多页入口，部署目录为 dist。链接使用 / 与 /download/，当前部署目标为独立域名根路径。
- 文案使用 src/i18n/ 内的 common、home、download 中英文命名空间，修改后运行 pnpm check:i18n；不得将官网文案塞入业务 Web 工程。
- 设计保持淡蓝白、摄影画册排版与清晰留白；图标仅使用 @element-plus/icons-vue。保持键盘焦点、44px 触控目标及减少动态效果支持。
- 页面演示采用本地 CSS 场景与演示文字；不得放入真实客户、账号、私有图片或凭据。增加外部素材必须保留来源和许可证。
- Web 入口、官网域名、客户端下载地址与发布状态使用 .env.example 列出的环境变量，由 src/config.ts 统一读取；不得在源码硬编码地址。修改后重启开发服务或重新构建部署。客户端 available 必须具备经核验的 HTTPS 地址、版本与架构；未发布不添加假按钮、二维码、商店链接或时间承诺。
- 安卓测试包另支持 VITE_ANDROID_BUNDLED_DOWNLOADS 同源托管模式：构建插件必须核验真实 APK 的 SHA-256 并复制到 dist，才向配置模块提供相对下载路径。标为调试签名测试版，不冒充 available 正式版；缺包或校验失败必须阻止构建。部署保持 HTTPS，下载路径不存在时返回 404，不允许 SPA 回退。
- 新增页面同步 HTML 元信息、语言注册、检查脚本与静态构建入口。正式域名未配置时不生成 canonical。
- 交付执行 pnpm build、git diff --check 及受影响的浏览器检查，在根 docs/ 新增当天记录并提交；不提交 dist、node_modules、.env 或测试截图。
