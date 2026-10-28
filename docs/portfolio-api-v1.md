# 作品集 V1 接口与分享

## 数据及范围

作品集、照片及策划关联使用迁移 `000025_portfolios.sql`，业务 UUID 作为对外标识。所有管理操作从 Access Token 会话取得空间和用户，标签、策划和灵感图必须属于该空间及创建者。照片上传保存到 `public`，复制灵感图时复制当前工作图并保留原图属性快照，不改动灵感图。可公开完整元数据及原始图片内的 EXIF，包括 GPS 和设备序列号；公开页面不返回关联策划内容或标题。

## 鉴权接口

- `GET /api/v1/portfolios` 返回 `items`，包含标题、简介、标签 ID、分享状态和可选首图 key。
- `POST /api/v1/portfolios` 接受 `title`、`description`、`tag_ids`；新作品集默认不启用分享。
- `GET /api/v1/portfolios/{business_id}` 读取详情；`PATCH` 接受 `title`、`description`、`tag_ids`、`project_ids` 并替换对应内容；`GET /projects` 读取关联策划 ID；`DELETE` 删除作品集、关联和公开照片。
- `GET /api/v1/portfolios/{business_id}/photos` 返回照片；`POST` 使用单个 multipart `file` 上传；`POST /photos/copy` 使用 `inspiration_id` 创建独立公开副本。
- `PATCH /photos/{photo_id}` 接受 `title`、`tag_ids`、`sort_order`；`DELETE` 删除照片及公开文件。上述照片路径均以 `/api/v1/portfolios/{business_id}` 为前缀。
- `POST /api/v1/portfolios/{business_id}/share` 创建或重置随机令牌，明文仅在本次响应的 `data.token` 中返回；`DELETE` 撤销。重置和撤销立即使旧令牌失效。

## 公开读取与部署

`GET /api/v1/public-portfolios/{share_token}` 无需鉴权，返回作品集、照片及作品实际使用的标签；`GET /api/v1/public-portfolio-images/{storekey}` 返回公共位图。关闭分享后已知图片直链仍可访问；删除照片后服务端删除文件，但已下载或缓存的副本无法撤回。文件清理失败保留数据库记录供重试；文件已删但事务失败时重试可继续清理记录，期间可能出现失效图片。

二维码和链接由 Web 页面基于公开 Web 地址生成。可设置 `VITE_PUBLIC_WEB_BASE_URL`，未设置时优先从 `VITE_ANDROID_WEB_UPDATE_URL` 的 `/android-updates/latest.json` 推导站点根目录，普通浏览器可使用当前 HTTPS 站点；Android 本机回环地址不会被用于分享。请确保发布配置指向实际公开业务 Web 站点，并配置站点对 `/p/*` 的 SPA 路由回退。未配置有效公开地址时不生成令牌。

## 验证边界

公开属性来自已有采集结果；不存在的 EXIF 不会凭空补齐。灵感图复制使用当前工作图，不保证保留原始文件中的全部二进制 EXIF；已保存的属性快照会保留。当前上传支持服务端可解码的 JPEG、PNG、GIF、WebP。分享二维码只在生成令牌时展示，重新打开后需重置链接才能获得新二维码。
