# 后端服务

后端服务入口位于 `cmd/api/`。服务使用 `migrations/` 中的版本化 SQL 初始化数据库结构和运行配置；启动时会检查 V1 表是否存在、当前 schema 是否含外键以及配置表中的必需运行项，不会自动执行 GORM AutoMigrate。

从仓库根目录复制 `backend/.env.example` 为 `backend/.env`，替换 PostgreSQL 和 Redis 密码后运行：

```sh
docker compose -f backend/docker-compose.yml up -d --build
```

API 默认通过宿主机 `127.0.0.1:8089` 提供服务，监听地址、请求上限、超时、token 有效期和文件存储配置从 `app_configs` 读取；`000002_seed_runtime_config.sql` 和 `000003_seed_file_storage_config.sql` 为新数据库添加初始值。readiness 地址 `/healthz` 和公开配置地址 `GET /api/v1/configs/public` 均无需鉴权；健康检查成功响应包含 `data.version` 当前后端版本。版本号统一维护在 `internal/version/version.go`，每批后端改动都必须按仓库根目录 `AGENTS.md` 递增。公开配置接口只返回 `is_public=true` 的配置项 key/value；可不传参数读取全部开放项，或重复传 `keys` 查询特定 key，例如 `?keys=frontend.theme&keys=frontend.upload_limit`。现有数据库升级时需备份后单独执行新增迁移 `backend/migrations/000004_add_config_public_flag.sql`；历史迁移保持不变，新增字段默认关闭。Compose 内的服务使用 `POSTGRES_USER` 连接 PostgreSQL；该账号是 PostgreSQL 初始化管理员，适合本地快速部署。生产环境应预先创建权限受限的数据库账号，并设置 `DB_USER`、`DB_PASSWORD` 覆盖默认值。

文件上传使用 `POST /api/v1/files`，请求为单文件 `multipart/form-data`，字段名为 `file`；上传大小由配置表中的 `files.max_upload_bytes` 控制，默认 50 MiB。当前 `files.active_store_type` 默认为 `local`，上传时每次读取该配置；文件 key 的类型前缀用于后续下载分发。下载使用 `GET /api/v1/files/{storekey}`，上传和下载均需 Bearer Access Token。V1 不建立文件元数据、不绑定用户，也不提供文件列表、删除和自动清理接口；拥有有效 storekey 的已鉴权用户都能获取对应文件。`oss` 类型已保留 key 路由分支，但当前尚未接入 OSS 后端，切换到 `oss` 后上传和读取会返回 501。

反馈上传的图片使用 `public_` 标记和单独的本地物理目录；只有带标记且实际 MIME 为 JPEG、PNG、GIF 或 WebP 的 key 能通过无需 token 的 `GET /api/v1/public-feedback-images/{storekey}` 读取。普通文件仍只由原有 Bearer 鉴权下载接口读取；给普通 key 添加 `public_` 前缀不会指向同一文件。

本地文件默认写入 `/var/lib/photo-nini/uploads/<yyyymmdd>/<uuid前三位>/<uuid>.<ext>`，由 Compose 命名卷 `uploaded_files_data` 持久化。`files.local_root` 修改后需重启 API；单文件大小限制 `files.max_upload_bytes` 也是启动时读取。新上传的存储类型每次上传时从配置表重新读取，变更后立即对下一次上传生效。

反馈公共图片单独写入 `/var/lib/photo-nini/uploads/public/<yyyymmdd>/<uuid前三位>/<uuid>.<ext>`；普通 storekey 仍映射到原有目录。

灵感图 `POST`/`PATCH` 请求可传 `file_name` 保存原始图片文件名。创建记录或通过 `image_key` 更换有效 storekey 后，接口先保存记录并快速返回，再由固定并发数的后台工作线程读取 JPEG、PNG、GIF 或 WebP 图片并压缩；任务完成前响应中的 `file_hash` 和 `file_size` 为 `null`。超过 1 MB 的静态图片会压缩并原位覆盖同一 storekey，不生成新 key；不透明图片转为 JPEG，有透明通道的图片保留为 PNG。超过 1 MB 的动画 GIF 或 WebP、无效图片、文件缺失或超过服务端处理上限时，异步任务失败，不会作为本次 `POST`/`PATCH` 的 HTTP 错误返回，`file_hash` 和 `file_size` 会保持为空；重新提交图片 key 可触发新任务。处理成功后，后端基于最终存储字节保存 SHA-256（小写 64 位十六进制）到 `file_hash`，并将实际字节数保存到 `file_size`，单位为字节。小于或等于 1 MB 的图片保持原始内容。读取时按文件实际内容返回 MIME 类型和下载扩展名，因此压缩后即使编码格式变化，原 storekey 仍可使用。每条记录的 `async_version` 在带图片的异步更新时递增，后台任务仅在图片 key 和代次仍与任务一致时写回元数据；新增记录的代次为 0。同一记录尚未执行的旧任务会被最新代次替换。该队列保存在 API 进程内，正常收到退出信号后会在关闭超时内等待已接收任务，进程异常终止时仍可能丢失未完成任务。更换为非 storekey 的 `image_key` 时哈希和大小会清空。现有数据库升级需确认并按顺序补跑尚未应用的 `backend/migrations/000005_add_inspiration_file_metadata.sql`、`backend/migrations/000006_add_inspiration_file_size.sql` 和 `backend/migrations/000007_add_inspiration_async_version.sql`；如果 `000005`、`000006` 已应用，则仅需应用 `000007`。历史记录的异步代次默认为 0。

创建与换图 `PATCH /api/v1/inspirations` 请求可携带顶层 `image_metadata` 对象。Web 在转换工作画布前用 `exifr` 尝试读取原文件属性，同时保存原图大小和浏览器提供的修改时间；服务端 Docker 运行镜像安装 ExifTool，在压缩前从编辑文档 `data.source_image_key` 读取原图并补充 EXIF/IPTC/XMP 属性，源 key 无法读取时回退到本次工作图。已知属性写入独立可空列，原图大小和修改时间与压缩工作副本的 `file_size` 分开保存；其他命名空间标签写入 `image_metadata_extra`，大小最多 1 MiB。服务端识别值覆盖客户端冲突值，服务端无法读取的值保留客户端结果。解析失败或 ExifTool 不可用不会中断图片压缩，图片 key 与 `async_version` 保护后台写回；属性只在创建、详情和更新响应中返回，列表省略该对象。GPS、序列号等属性随灵感图私有记录保存，接口仍要求 Access Token；不收集本机路径、文件系统创建时间或访问时间。迁移为 `backend/migrations/000016_add_inspiration_image_metadata.sql`，不回填历史记录。Web 端 `exifr` 7.1.3 使用 MIT 许可；服务端 Alpine `exiftool` 包使用 Artistic-1.0-Perl 或 GPL-1.0-or-later 许可，版本随 Alpine 3.22 仓库更新。

账本使用 `ledger_records` 表逐笔保存已完成收支和可恢复草稿；金额在数据库中保存为整数分，API 使用两位小数的十进制字符串。所有 `/api/v1/ledger/*` 接口均需 Bearer Access Token，并从令牌读取用户业务 UUID 限定查询和写入。`GET /api/v1/ledger/records?from=YYYY-MM-DD&to=YYYY-MM-DD` 返回日期范围内的正式记录及当前用户的未完成草稿；`POST /api/v1/ledger/records` 创建草稿，`PATCH /api/v1/ledger/records/{business_id}/draft` 自动保存快照并校验草稿修订号，`POST /api/v1/ledger/records/{business_id}/complete` 完成草稿或应用编辑，`DELETE /api/v1/ledger/records/{business_id}` 删除记录，`POST /api/v1/ledger/import` 在单个事务中导入完整记录。正式分析不包含草稿；编辑已完成记录期间，统计仍按原正式字段计算。旧营收日历 JSON/CSV 导入时类型默认为收入。已有数据库升级前先备份 PostgreSQL，再确认并按顺序应用尚未执行的版本化迁移；账本表迁移为 `backend/migrations/000009_create_ledger_records.sql`，API 不会自动执行 SQL。

### 消息中心与 webhook 通知

登录用户可在共享 Web 应用 `/messages` 页查看灵感图处理、服务端错误和用户反馈通知，筛选未读消息、标记已读并打开关联灵感图。反馈通知显示反馈正文、反馈人昵称和手机号；游客反馈会标记为未登录。站内消息中的反馈图片以内嵌图片显示。提交反馈成功后，表单文字和图片预览会清空，并展示感谢与等待提示。应用打开期间导航角标约每 30 秒刷新一次，并在应用内提示新消息；不提供浏览器关闭后的系统推送。

用户可在“Webhook 渠道”中创建多个企业微信、钉钉或飞书自定义机器人配置，停用的配置仍计入总数上限。配置数量上限来自私有 `app_configs` 项 `notice.max_webhooks_per_user`，迁移初始值为 20，运行时读取，配置更新后无需重启。支持启停、修改、软删除和异步测试；完整 webhook 地址和签名密钥只在创建/修改时提交，读取只返回平台主机遮罩和是否配置签名。编辑时空白 webhook 地址或签名密钥表示保留原值；清除签名需显式设置 `clear_signing_secret=true`。

通知 API 均需 Bearer Access Token，并从 token 取得用户业务 UUID：`GET /api/v1/notifications?page=1&page_size=20&unread_only=false` 分页查询消息及各渠道投递状态，`GET /api/v1/notifications/unread-count` 读取角标数量，`PATCH /api/v1/notifications/{business_id}/read` 标记单条已读，`POST /api/v1/notifications/read-all` 标记全部已读。配置 API 为 `GET/POST /api/v1/notice-configs`、`PATCH/DELETE /api/v1/notice-configs/{business_id}` 和 `POST /api/v1/notice-configs/{business_id}/test`；测试接口只创建 outbox 并以 202 返回。用 `GET /api/v1/notice-deliveries/{business_id}` 查询投递状态，`POST /api/v1/notice-deliveries/{business_id}/retry` 重新排队最终失败的任务。

后台事件在同一数据库事务中写入站内通知和各启用配置的 webhook outbox，独立 worker 负责外部 HTTP 请求，普通消息使用 12 秒超时；飞书反馈原生图片会为令牌获取、最多五张图片上传和 webhook 请求合计使用最多 90 秒的任务上下文，每个 HTTP 请求仍限制为 12 秒且拒绝重定向。最多 5 次发送尝试。超长反馈 Markdown 会按保守的 3800 字节上限拆分，每个渠道的每段都有独立投递状态和重试入口。消息中心分别展示发送状态、安全错误分类和尝试次数；`delivered` 表示 webhook 请求获得成功 HTTP 响应且未返回已识别的平台错误码，不代表群成员已阅读。网络错误、HTTP 状态或平台业务码只转换成安全错误分类，不记录 webhook 地址、签名密钥或远端响应正文。投递失败仍可从站内消息查看并单渠道重试；灵感图事件要求当前 `image_key` 和 `async_version` 同时匹配，不为已删除或过期版本生成通知。新数据库应按序执行 `backend/migrations/000001` 至当前最新迁移；已有数据库升级前先备份并按顺序手动执行所有尚未应用的文件。`000011_create_notification_center.sql` 创建消息中心表，`000013_feedback_notifications.sql` 增加反馈任务和分段投递字段，`000014_seed_feishu_feedback_image_upload.sql` 增加可选飞书图片上传凭证；迁移均不建立外键。

### 服务端错误日志与管理员通知

统一 HTTP 5xx、灵感图图片处理后台失败、通知 worker 数据库操作失败会进入 `error_logs` 表。请求线程只生成安全摘要并非阻塞地投递到容量为 512 的内存队列；标准输出、数据库写入、一分钟去重和管理员站内通知由后台 worker 处理。重复键优先按源码文件和行号计算，一分钟内同一代码行的重复只增加 `occurrence_count` 并更新时间；缺少源码位置时才按错误码和原因类型区分。间隔超过一分钟后再次发生会新增一行。PostgreSQL 原因保存 SQLSTATE 等结构化信息，其他原因会遮蔽常见凭据格式，不保存请求正文、上传内容或访问令牌。

迁移 `backend/migrations/000012_create_error_logs.sql` 创建错误日志表，并增加私有配置 `error_logs.notify_admin`（初始为 `true`）和 `error_logs.admin_phone`（初始为空）。需要通知管理员时，将 `error_logs.admin_phone` 更新为已注册用户的 11 位手机号；worker 每条新日志读取配置并按手机号查找用户，找到后只创建该用户的站内通知，不将错误内容发送到 webhook。将 `error_logs.notify_admin` 设为 `false` 可暂停通知。已有数据库应先备份，再按顺序手动执行所有尚未应用的迁移直至 `000012`；API 启动会检查 `error_logs` 表是否存在。

```sql
UPDATE app_configs
SET config_value = '<已注册管理员手机号>', updated_at = now()
WHERE config_key = 'error_logs.admin_phone';
```

### 用户反馈通知与公共图片

新反馈与 `feedback_notification_jobs` 任务在同一事务保存。后台 worker 按 `feedback.admin_phone` 查找已注册用户，并以稳定 `feedback.submitted:<feedback business_id>` 事件键创建站内通知和该用户启用渠道的 webhook outbox；通知包含完整反馈文字、反馈人昵称与手机号、图片 URL，不匹配接收用户时仅将任务标为安全错误状态，反馈仍保留。默认 `feedback.notify_admin=false`，无短信功能。启用前应设置独立手机号和 HTTPS 公共基础地址；地址可包含反向代理路径前缀，例如 `https://photo.016imcn.love/back`。手机号、开关与地址均为私有配置，业务代码只经统一配置服务读取。

`http.public_base_url` 必须是 HTTPS 地址且包含主机名；允许配置反向代理路径前缀，但不允许查询参数、片段或用户凭据。worker 会在该地址后拼接反馈图片 API 路径生成绝对 URL，不采信请求 `Host`。反馈图片链接指向上方的无需鉴权读取接口；其他文件 key 不具有公共读取权限。迁移 `backend/migrations/000013_feedback_notifications.sql` 会添加私有反馈配置、任务表与 webhook 分段列；`backend/migrations/000014_seed_feishu_feedback_image_upload.sql` 添加飞书 App ID 和 App Secret 私有配置。运维应先依次应用迁移，再填入实际接收配置，最后启用开关：

企业微信和钉钉通知同时发送图片 Markdown 与可点击图片链接。飞书标准 Markdown 图片语法不会在当前卡片 Markdown 文本里变成原生图片。飞书自定义机器人富文本支持 `img` 节点，但必须先通过飞书图片上传接口取得 `image_key`。配置飞书自建应用凭证后，worker 从公共反馈图片存储读取文件并上传，再以 `post` 富文本中的 `img` 节点投递；上传的 key 和应用令牌不进入站内通知载荷、日志或 API 响应。没有配置凭证时继续发送可点击公共图片链接。应用需启用机器人能力并授予获取与上传图片资源权限；单张图片不得超过飞书 10 MiB 限制，且仅接受飞书支持的图片格式。

```sql
UPDATE app_configs SET config_value = '<已注册用户手机号>', updated_at = now() WHERE config_key = 'feedback.admin_phone';
UPDATE app_configs SET config_value = 'https://<公开服务域名>/<可选路径前缀>', updated_at = now() WHERE config_key = 'http.public_base_url';
UPDATE app_configs SET config_value = '<飞书自建应用 App ID>', updated_at = now() WHERE config_key = 'feedback.feishu_app_id';
UPDATE app_configs SET config_value = '<飞书自建应用 App Secret>', updated_at = now() WHERE config_key = 'feedback.feishu_app_secret';
UPDATE app_configs SET config_value = 'true', updated_at = now() WHERE config_key = 'feedback.notify_admin';
```

`feedback.feishu_app_id` 和 `feedback.feishu_app_secret` 默认留空且均为私有配置；两项都配置后才启用原生图片上传。应用密钥不要写入仓库、镜像或运维日志。配置只用于后台反馈图片通知，飞书 API 权限或上传失败会记录可重试的安全投递状态。

### 构建镜像源

Compose 默认通过 DaoCloud Docker Hub 代理拉取 Go 和 Alpine 基础镜像，并先用 `goproxy.cn` 下载 Go 模块；该代理失败时继续尝试 Go 官方模块代理和 direct。Dockerfile 保留 `go.mod`、`go.sum` 独立层，依赖清单未变化时，Docker 可复用 Go 模块下载层。Docker 镜像代理和 Go 模块代理是两项独立设置；如果服务器无法访问默认代理，可在启动 Compose 的 shell 中覆盖：

```sh
GO_BASE_IMAGE=golang:1.24-alpine \
RUNTIME_BASE_IMAGE=alpine:3.22 \
GOPROXY='https://proxy.golang.org,direct' \
docker compose -f backend/docker-compose.yml up -d --build
```

以上变量是构建参数，不需要写入 `backend/.env`。镜像源应按部署服务器的网络选择；构建速度取决于服务器到镜像仓库和 Go 模块代理的实际连接。

### 本地构建并导入服务器

目标服务器为 Linux `amd64` 时，可在本机预构建并导出镜像：

```sh
docker buildx build --platform linux/amd64 \
  --build-arg GO_BASE_IMAGE=m.daocloud.io/docker.io/library/golang:1.24-alpine \
  --build-arg RUNTIME_BASE_IMAGE=m.daocloud.io/docker.io/library/alpine:3.22 \
  --build-arg 'GOPROXY=https://goproxy.cn|https://proxy.golang.org|direct' \
  --tag photo-nini-api:local --load \
  --file backend/Dockerfile backend
mkdir -p backend/dist
docker save --output backend/dist/photo-nini-api-linux-amd64.tar photo-nini-api:local
```

将 tar 文件复制到服务器后，先创建并配置 `backend/.env`，再导入镜像并启动服务：

```sh
docker load --input photo-nini-api-linux-amd64.tar
docker compose -f backend/docker-compose.yml up -d --no-build
```

导出包仅适用于 `linux/amd64`；若服务器是 ARM64，需要将构建平台改为 `linux/arm64` 并单独导出对应镜像。

PostgreSQL 初始化 SQL 只会在空数据目录首次启动时执行。PostgreSQL 数据保存在 `backend/postgres17_data/`，Redis 数据保存在 Compose 命名卷中；普通 `docker compose down` 不删除这两处数据。已有数据升级时，应先备份数据库，再通过 PostgreSQL 客户端按顺序执行尚未应用的版本化迁移文件。
