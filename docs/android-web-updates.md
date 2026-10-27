# 安卓签名 Web 资源更新

## 架构与边界

Android 1.1.0（安装版本4）开始具备动态更新能力，旧APK必须先安装一次新版。Gecko内核和固定原生桥接始终随APK分发，不从网络下载内核或原生可执行文件。

Gecko内置扩展只能安装APK内的目录，因此不把下载的ZIP作为新的内置扩展。固定扩展负责桥接；原生私有资源服务只监听127.0.0.1，提供APK基线或验签后的资源目录。固定端口维持Web来源和登录态，随机HttpOnly Cookie、Host/Origin检查和顶层会话检查限制访问。端口被其他进程占用时显示启动错误，不静默换来源。Android仅对127.0.0.1允许明文；静态页CSP只允许本机资源，API由原生服务转发到固定HTTPS后端，不开放任意代理。

首次启用时尝试把原扩展来源下的本应用localStorage迁移到新来源；只在内存交接、不上传、不额外保存令牌副本。无法读取旧来源时仍可重新登录。后续资源更新保持来源不变。卸载或清除应用数据会清理本地包和登录态。

## 构建及签名配置

更新清单地址通过 `VITE_ANDROID_WEB_UPDATE_URL`（Web的 `.env` / `.env.production` 或构建环境）配置；也可用 `PHOTO_NINI_WEB_UPDATE_URL` 覆盖。实际发布位置应是业务Web域名下的 `/android-updates/latest.json`，不经过API鉴权。

APK默认公钥位于 `frontend/apps/android/assets/web-update-public.pem`，它不是秘密；可通过 `PHOTO_NINI_WEB_UPDATE_PUBLIC_KEY` 指定其他公钥文件。私钥只能保存在仓库外或CI密钥存储，通过 `PHOTO_NINI_WEB_UPDATE_PRIVATE_KEY` 指定路径。本机默认读取用户目录下 `.photo-nini/keys/web-update-private.pem`；务必单独安全备份，丢失私钥或更换信任公钥需要重新发行APK。不得把私钥上传到静态站。

在共享Web目录执行 `pnpm build`：先构建页面和 `web-release.json`，再由postbuild自动生成签名包。没有本机私钥且未显式配置私钥时会明确跳过更新包，普通Web构建仍可使用；CI发布必须配置私钥，或再执行 `node ../android/scripts/package-web.mjs` 强制校验发布配置，缺失即失败。

- `PHOTO_NINI_WEB_RELEASE`：可选正安全整数发布号，必须单调递增；默认构建时间毫秒值，发布CI应保证时钟和顺序。
- `PHOTO_NINI_WEB_VERSION`：页面显示版本，默认构建时间；与APK版本、后端版本独立。
- `PHOTO_NINI_WEB_UPDATE_NOTES`：可选更新说明，写入签名版本记录。
- `PHOTO_NINI_PYTHON`：可选Python解释器路径，用标准库生成ZIP，无新增运行时第三方依赖。

签名为RSA-SHA256，私钥至少2048位；当前本机公钥对应3072位RSA。发布脚本必须校验私钥与APK公钥一致。Web发布记录包含构建时API地址及路径前缀，不能用另一套API配置给旧构建重新签名。APK脚本复用同一Web产物，不把更新ZIP再嵌套进APK。

## 产物与部署顺序

Web的 `dist/android-updates/` 包含：

- `web-<release>.zip` 和 `.sha256`：不可变资源包。
- `latest.json`：Base64编码载荷和签名的信封；载荷包含发布号、显示版本、更新说明、最低壳版本、桥接版本、固定API地址、包URL/大小/哈希及逐文件哈希。
- `release-<release>.json`：方便运维查看的版本记录；客户端只信任签名信封。

先上传版本ZIP和版本记录，最后原子替换 `latest.json`。保留之前的不可变版本包；不能先让清单指向尚未上传的文件。普通Web更新也要部署本次Web产物。Nginx应为更新目录单独关闭SPA回退，例如：

```nginx
location ^~ /android-updates/ {
    try_files $uri =404;
    add_header Cache-Control "no-cache" always;
}
```

清单和包使用HTTPS，禁止重定向；包必须与清单同源。只部署普通Web而未部署更新目录不会更新安卓页面。本实现不自动向生产服务器上传。

## 启动与回退

启动显示原生独立更新页，依次展示检查、下载百分比、校验和页面版本。没有更新或检查失败则进入本地可用版本；更新中不会进入编辑器。应用已打开时不自动重载编辑页面，下次启动才检查，避免丢失未保存内容。

下载直接写私有临时文件并流式哈希；压缩包最大128MiB，解压最多256MiB、4096个文件，拒绝越界路径和重复文件。验签、包哈希及所有文件哈希必须通过；不接受更低发布号、不兼容壳/桥接或不同API的包。持久化副本每次启动也重新验签和校验文件。

候选版本在共享应用挂载并建立受来源校验的原生连接后才确认；45秒未就绪、页面失败或启动崩溃时退回稳定包。候选未确认就被关闭或进程退出，下一次启动也会退回。失败版本被记住，修复后必须使用新的发布号。客户端保留当前稳定资源，清理过期目录；APK内基线一直可用。

原生导出、相册、返回键、主题继续由固定桥接处理。更新不修改后端鉴权、组织隔离和数据库结构。真实设备、真实账号、商店分发和生产部署需要各自验收，不能以模拟器测试替代。
