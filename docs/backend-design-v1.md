# 后端 V1 设计

## 1. 目标与边界

首版后端为跨平台图片与文本编辑器提供账户、标签、灵感图和文件读写接口。技术栈采用 Go、Gin、GORM、PostgreSQL 和 Redis。`backend/` 已包含服务代码、版本化 SQL、PostgreSQL 17、Redis 和 API 的 Docker Compose 配置；数据库迁移由 PostgreSQL 空数据卷首次启动时执行初始化 SQL，服务本身不自动运行 GORM AutoMigrate。

PostgreSQL 保存配置、用户、标签和灵感图；Redis 保存访问令牌、刷新令牌及其会话索引。`backend/docker-compose.yml` 同时启动 PostgreSQL 17、Redis 7 和 Gin API；数据库初始化 SQL 在空数据卷首次启动时创建 schema 并写入服务默认配置。`app_configs` 统一保存服务配置，包括仅供后端读取的私密配置；秘密值必须保持 `is_public=false`，HTTP 与认证运行参数由统一配置服务从表中读取。PostgreSQL/Redis 连接参数和连接凭证仍由服务运行环境提供。

从仓库根目录复制 `backend/.env.example` 为 `backend/.env` 并替换两组随机密码，然后运行 `docker compose -f backend/docker-compose.yml up -d --build`。Compose 和 API 默认使用 `POSTGRES_USER` 作为数据库账号；这是 PostgreSQL 初始化管理员账号，便于本地快速部署。生产环境可预先创建权限受限账号，并通过 `DB_USER`、`DB_PASSWORD` 覆盖。宿主机 PostgreSQL 和 API 分别绑定到 `127.0.0.1:5432`、`127.0.0.1:8089`；Redis 仅在 Compose 内部网络开放。PostgreSQL 数据保存在 `backend/postgres17_data/`，Redis 数据保存在 Compose 命名卷中；普通 `docker compose down` 不会删除数据。初始化 SQL 仅在空数据目录首次启动时执行；后续结构变更需按版本手动应用迁移文件。

Compose 默认使用 DaoCloud Docker Hub 代理拉取 Go 与 Alpine 基础镜像，并使用 `goproxy.cn` 下载 Go 模块。可通过 `GO_BASE_IMAGE`、`RUNTIME_BASE_IMAGE`、`GOPROXY` 三个 shell 环境变量覆盖构建源；这些值作为 Docker build args 传入，不是服务运行配置，也不得在其中放置秘密。Dockerfile 先复制 `go.mod` 和 `go.sum` 并下载依赖，使依赖清单不变时可以复用该构建层。镜像代理与模块代理分别影响容器镜像拉取和 Go 依赖下载，实际收益取决于部署网络。

建议代码职责按 HTTP handler、service、repository 分层：Gin handler 处理协议与参数，service 承担鉴权、用户归属和业务校验，GORM repository 访问 PostgreSQL，Redis repository 管理 token 状态。平台壳只消费版本化 HTTP API，不直接访问数据库或 Redis。

## 2. 通用数据库约定

所有项目自建业务表都包含以下字段：

| 字段 | PostgreSQL 类型 | 规则 |
| --- | --- | --- |
| `id` | `integer` | 自增主键，仅数据库内部使用 |
| `business_id` | `uuid` | 非空；每张表单独建立唯一索引；由服务端生成 UUID v4 |
| `created_at` | `timestamptz` | 非空，创建时间 |
| `updated_at` | `timestamptz` | 非空，修改时间 |

API 只暴露业务 UUID，不返回自增 `id`。表间关系字段保存目标记录的 `business_id`，不建立任何外键约束。服务层负责校验关联记录存在、属于当前用户，并在删除或更新时同步清理关系数据。

所有手写 DDL、索引、版本迁移、初始化和数据修复 SQL 文件放在 `backend/migrations/`，按版本管理并随代码提交；数据库结构的变更必须有对应脚本。运行时查询使用 GORM repository 中的 Go 代码，不要求把每条查询复制成独立 `.sql` 文件。

GORM 迁移设置 `DisableForeignKeyConstraintWhenMigrating: true`，模型避免声明会生成外键的关联约束。迁移脚本不得写外键声明；部署或验收时检查 PostgreSQL 项目 schema，确认不存在外键。数据库可继续使用主键、唯一、非空和必要的检查约束，这些都不属于外键。

## 3. V1 数据表

以下业务字段均与通用字段同时存在。字段命名使用 snake_case；Go 模型字段遵循 Go 命名规范。

### 3.1 `app_configs` 配置表

| 字段 | 类型 | 规则 / 用途 |
| --- | --- | --- |
| `config_key` | `text` | 非空、唯一 |
| `config_value` | `text` | 非空，保存配置值；秘密配置只允许作为私有项保存 |
| `description` | `text` | 配置说明 |
| `is_public` | `boolean` | 非空，默认 `false`；仅为 `true` 时公开配置值 |

只有 `is_public=true` 的配置值可以通过无需鉴权的 `GET /api/v1/configs/public` 读取；响应仅包含 key 和 value，私有项及不存在项均不会返回。可不传参数读取全部开放项，也可重复传入 `keys` 参数筛选，例如 `?keys=frontend.theme&keys=frontend.upload_limit`。公开配置不得包含密码、密钥、令牌等秘密；密码、应用密钥和签名材料等只能作为 `is_public=false` 的私有配置由后端读取。服务通过统一配置服务按 key 加载 `app.mode`、`http.listen_addr`、HTTP 请求体上限和超时、Access/Refresh Token 时长、文件存储根目录和单文件大小上限；`files.active_store_type` 在每次上传时重新读取，使存储类型切换对下一次写入生效。连接 PostgreSQL 和 Redis 所需的地址、账号及密码保留在运行环境中。`000002_seed_runtime_config.sql` 和 `000003_seed_file_storage_config.sql` 为新环境提供非秘密初始值；缺少必需配置项时服务拒绝启动。

### 3.2 `users` 用户表

| 字段 | 类型 | 规则 / 用途 |
| --- | --- | --- |
| `nickname` | `varchar(64)` | 昵称 |
| `phone` | `varchar(11)` | 非空、唯一，只保存 11 位数字 |
| `password_hash` | `text` | 非空，保存 Argon2id 编码哈希，不保存明文 |
| `avatar_key` | `text` | 头像 OSS key 或由服务管理的文件标识 |

手机号是登录标识。当前使用手机号和密码注册/登录，不包含短信验证码流程；旧客户端可在登录时传入 `+86` 加 11 位号码。

### 3.3 `tags` 标签表

| 字段 | 类型 | 规则 / 用途 |
| --- | --- | --- |
| `user_id` | `uuid` | 保存所属用户的 `business_id`，非空，建立 B-tree 索引 |
| `content` | `varchar(64)` | 标签内容，非空 |

### 3.4 `inspirations` 灵感图表

| 字段 | 类型 | 规则 / 用途 |
| --- | --- | --- |
| `user_id` | `uuid` | 保存所属用户的 `business_id`，非空，建立 B-tree 索引 |
| `tag_ids` | `uuid[]` | 保存标签 `business_id`，非空，默认空数组；建立 GIN 索引 |
| `image_key` | `text` | 非空，保存 OSS key 或由服务管理的文件标识 |
| `file_name` | `text` | 非空，保存原始文件名；历史记录默认空字符串，最多 255 个字符 |
| `file_hash` | `varchar(64)` | 可空，保存图片内容 SHA-256 小写十六进制摘要；建立部分 B-tree 索引 |
| `file_size` | `bigint` | 可空，保存服务端确认或压缩后最终文件大小，单位字节；历史记录和非 storekey 图片为空 |
| `data` | `jsonb` | 非空，保存灵感图复杂数据；作为对象使用并包含 `schema_version` |

创建或更新灵感图时，服务端一次性检查所有 `tag_ids` 是否存在且属于当前用户；任一标签不合法则整次请求失败。`image_key` 传入本服务格式的 `storekey` 时，服务端读取其内容；超过 1 MB 的静态图片会压缩后原位覆盖，不生成新 key，再根据最终存储字节计算 SHA-256 和文件大小。`file_size` 以字节为单位，表示压缩覆盖完成后的实际大小；小于或等于压缩目标的文件记录其原始字节数。更新 `image_key` 时重新处理并覆盖旧摘要和大小；非 storekey 的历史 OSS key 等标识不计算哈希或大小，两列均为空。`file_name` 由客户端传入，服务端只保存文件名部分，不保存客户端本机目录。删除标签时，在同一 PostgreSQL 事务中从该用户的灵感图 `tag_ids` 中移除对应业务 UUID，再删除标签记录。删除灵感图采用物理删除；V1 不引入软删除字段。

### 3.5 `feedbacks` 反馈表

| 字段 | 类型 | 规则 / 用途 |
| --- | --- | --- |
| `user_id` | `uuid` | 可空；登录用户保存 `users.business_id`，游客反馈为空；建立 B-tree 索引 |
| `content` | `text` | 非空，默认空字符串；最多 2000 个 Unicode 字符，至少与一张图片提供其一 |
| `image_keys` | `text[]` | 非空，默认空数组；保存服务端验证的图片 storekey，最多 5 个 |

反馈接口允许游客提交；登录请求的用户归属始终从有效 token 得出。正文与图片至少提供一项，图片受文件单体积配置约束。提交事务同时写入 `feedbacks` 与 `feedback_notification_jobs`，由持久化 worker 按专用配置查找已有接收用户并发布 `feedback.submitted` 通知；通知关闭或接收用户缺失时反馈仍成功保留。不提供反馈后台、回复或用户历史页。

反馈图片使用独立 `public_` key 和物理存储目录。无需鉴权的图片读取接口只接受该标记且实际类型为 JPEG、PNG、GIF 或 WebP 的文件；普通文件 key 不可通过该接口读取。通知的 Markdown 和纯文本由后台从完整反馈正文生成，图片绝对链接使用私有 `http.public_base_url` 配置。该 HTTPS 地址可包含反向代理路径前缀，不使用请求 Host。

### 3.6 索引清单

- 所有业务表均对 `business_id` 建唯一索引；000008 迁移后包含用户、标签、灵感图、反馈和配置表。
- `app_configs.config_key` 和 `users.phone` 建唯一索引。
- `tags.user_id`、`inspirations.user_id` 建 B-tree 索引，满足按用户隔离和查询的要求。
- `inspirations.tag_ids` 建 GIN 索引；V1 不为 `data` 增加 JSONB 索引，后续只在明确有查询条件时通过迁移添加。
- `inspirations.file_hash` 建部分 B-tree 索引，只索引非空 SHA-256 摘要。

PostgreSQL 的 GIN 适用于索引复合值中的成员（包括数组元素），供标签数组检索使用：[PostgreSQL GIN 索引文档](https://www.postgresql.org/docs/current/gin.html)。

### 3.7 文件存储

- V1 文件不写入 PostgreSQL，不建立文件元数据表，不关联 `user_id`，也不提供列表、删除或自动清理能力。上传和下载路由仍要求有效 Access Token；已鉴权用户持有有效 `storekey` 即可读取其定位的文件。
- `files.max_upload_bytes` 配置单文件最大字节数，默认 `52428800`（50 MiB）；`files.local_root` 配置本地存储根目录，默认 `/var/lib/photo-nini/uploads`。Compose 使用 `uploaded_files_data` 命名卷持久化该目录。
- `files.active_store_type` 规定新上传的存储类型，每次上传时从配置表重新读取。当前接受 `local` 和 `oss` 类型；V1 仅实现本地后端，配置为 `oss` 时上传返回 501，避免静默写到本地。以后接入 OSS 后，上传按当前配置选择后端并写入对应类型前缀。
- `storekey` 格式为 `<type>_yyyymmdd_uuid.ext`，例如 `local_20261014_550e8400-e29b-41d4-a716-446655440000.png`。下载根据 key 的 type 分派；非法 key 不会被拼接为路径，OSS key 在后端尚未接入时返回 501。
- 本地目录为 `<root>/yyyymmdd/<uuid前三位>/<uuid>.<ext>`；日期使用 UTC，UUID 和扩展名由服务端校验或生成。上传端只接受一个 `file` multipart 字段，并按配置限制实际读取字节数。灵感图保存时，超过 1 MB 的不透明 JPEG、PNG、GIF 或 WebP 会转为 JPEG；含透明像素的图片缩小后保存为 PNG。小于或等于 1 MB 的图片保留原字节。动画 GIF 或 WebP 超过 1 MB 时拒绝保存，解码压缩的图片不得超过 4000 万像素。存储文件仍使用原 storekey，下载根据内容识别真实 MIME 类型和扩展名，并以附件和 `no-store` 方式返回。

## 4. Token 与 Redis

### 4.1 签发和存储

- 每次登录/注册分别用密码学安全随机源生成 32 字节 Access Token 和 32 字节 Refresh Token，使用无填充 Base64URL 编码返回。Token 为不透明随机值，不把用户信息、时间戳或过期时间直接拼入 token。
- Redis 以 token 的 SHA-256 十六进制摘要作为 key 的 token 部分，不存储原始 token。Access 和 refresh 使用不同命名空间：`auth:access:<sha256>`、`auth:refresh:<sha256>`。
- Token Redis value 保存 `user_business_id` 和 `session_id`；`auth:session:<session_id>` 保存该会话的 token key 集合，以便退出时撤销该会话全部 token。Redis 中的 token key 必须设置 TTL。
- Access Token TTL 为 2 小时，Refresh Token TTL 为 30 天。响应返回 `expires_in` 和 `refresh_expires_in` 秒数；有效性及过期时间以 Redis key 是否存在为准。

### 4.2 校验、刷新和退出

- 所有受保护 API 都从 `Authorization: Bearer <access_token>` 读取 token，并通过 Redis 校验对应摘要 key 存在。刷新接口同样通过 Redis 校验 Refresh Token；不得只校验 token 格式或仅凭客户端声明放行。
- 刷新时服务端预生成新的一对 token，通过 Redis Lua 脚本或等价原子操作校验并删除旧 Refresh Token、写入新 AT/RT 和更新会话索引。旧 RT 重放或并发刷新失败的一方返回未认证；之前的 AT 保持有效至其自身过期或会话退出。
- 退出接口使用当前 Access Token 解析会话索引并删除该会话登记的 token key 和会话 key。重新登录创建新会话，不影响其他设备会话。
- Token 缺失、无效、过期或重放返回 `401`。Redis 不可用时 fail closed 并返回 `503`，不得绕过 Redis 校验。
- 日志、错误信息和分析事件不得记录原始 token 或密码。

GORM 关联迁移默认可能创建外键；配置 `DisableForeignKeyConstraintWhenMigrating: true` 可关闭该行为。V1 仍需同时避免关联约束标签，并通过 schema 检查确认结果：[GORM 约束文档](https://gorm.io/docs/constraints.html)。

## 5. V1 HTTP API

统一前缀 `/api/v1`。注册、登录和刷新是无需 Access Token 的认证入口；退出登录、用户资料、标签、灵感图和文件接口都需要有效 Access Token。每次受保护请求都通过 Redis 校验 token。`/healthz` readiness 接口和 `GET /api/v1/configs/public` 公开访问；公开配置接口只返回 `is_public=true` 的 key/value。业务数据归属从 token 中的用户业务 UUID 得出；文件 V1 不记录用户归属，任何已通过鉴权且持有有效 `storekey` 的用户都可读取对应文件。

### 5.1 响应与错误

除 `204`（无响应体）外，成功响应使用 `{ "data": ..., "request_id": "..." }`；错误响应使用 `{ "error": { "code": "...", "message": "...", "details": ... }, "request_id": "..." }`。错误信息不得泄露密码哈希、token 或其他用户是否存在的敏感细节。

| HTTP 状态 | 使用场景 |
| --- | --- |
| `200` | 查询、登录、刷新和更新成功 |
| `201` | 注册或创建资源成功 |
| `204` | 退出或删除成功，无响应体 |
| `400` | JSON、UUID 或查询参数格式错误 |
| `405` | 请求路径存在，但 HTTP 方法不受支持 |
| `401` | 缺少、无效、过期或已使用的 token |
| `404` | 资源不存在或不属于当前用户 |
| `409` | 手机号或配置 key 等唯一值冲突 |
| `422` | 字段校验失败 |
| `503` | Redis 等必需依赖暂不可用 |
| `413` | 请求体超过服务配置的大小限制 |
| `500` | 未预期的服务端错误 |

列表接口使用 `page`、`page_size`，默认 1 和 20，`page_size` 最大 100；返回 `items`、`page`、`page_size`、`total`。结果按 `created_at DESC, business_id DESC` 稳定排序。

### 5.2 路由与字段

| 方法与路径 | 鉴权 | 请求字段 / 行为 | 成功结果 |
| --- | --- | --- | --- |
| `POST /auth/register` | 否 | 旧 JSON 注册协议不再创建账户 | `426 upgrade_required` |
| `POST /auth/login` | 否 | `phone`, `password` | 用户资料与 token pair |
| `POST /api/v2/auth/register` | 否 | multipart：`phone`, `password`, `nickname`, 必传 `avatar` 图片 | 用户资料与 token pair |
| `POST /auth/refresh` | 否（请求体带 RT） | `refresh_token`；原子轮换 | 新 token pair |
| `POST /auth/logout` | AT | 撤销当前会话 | `204` |
| `GET /healthz` | 否 | 检查 PostgreSQL 和 Redis 连通性 | `data.status` 与当前服务 `data.version` |
| `GET /configs/public` | 否 | 可重复传 `keys` 筛选；只查询 `is_public=true` | `items` 中仅包含 `key`、`value` |
| `POST /feedback` | 可选 AT | multipart：`content`，重复的 `images` 文件字段；允许游客 | `received: true` |
| `GET /public-feedback-images/{storekey}` | 否 | 仅接受带反馈公共标记的图片 key | 实际图片二进制流 |
| `GET /users/me` | AT | 无 | 当前用户 `business_id`、昵称、手机号、头像 key 和时间字段 |
| `PATCH /users/me` | AT | 可选 `nickname`, `avatar_key` | 更新后的当前用户资料 |
| `POST /users/me/password` | AT | `current_password`, `new_password` | `204`；保留当前设备并撤销其他设备会话 |
| `GET /tags` | AT | 分页参数 | 当前用户标签列表 |
| `POST /tags` | AT | `content` | 新建标签及其业务 UUID |
| `PATCH /tags/{business_id}` | AT | `content` | 更新后的标签 |
| `DELETE /tags/{business_id}` | AT | 删除标签并清理灵感图引用 | `204` |
| `GET /inspirations` | AT | 分页参数，可重复传 `tag_ids` 和 `tag_match=any\|all` | 当前用户灵感图列表 |
| `POST /inspirations` | AT | `tag_ids`, `image_key`, 可选 `file_name`, `data` | 新建灵感图及其业务 UUID、文件元数据 |
| `GET /inspirations/{business_id}` | AT | 路径业务 UUID | 灵感图详情 |
| `PATCH /inspirations/{business_id}` | AT | 可选 `tag_ids`, `image_key`, `file_name`, `data` | 更新后的灵感图及文件元数据 |
| `DELETE /inspirations/{business_id}` | AT | 路径业务 UUID | `204` |
| `POST /files` | AT | multipart/form-data，单个文件字段 `file` | `storekey` |
| `GET /files/{storekey}` | AT | 根据 key 中的存储类型读取文件 | 二进制文件流 |

上传成功返回 `{ "data": { "storekey": "local_yyyymmdd_uuid.ext" }, "request_id": "..." }`。修改 `files.active_store_type` 后，后续上传无需重启即可采用新值；`files.max_upload_bytes` 和 `files.local_root` 在启动时加载，修改后需重启服务生效。

JSON 请求和响应使用 UTF-8；日期时间以 RFC 3339 格式返回。创建请求不得传 `id`、`business_id`、`user_id` 或时间字段，这些由服务端生成或从 token 得出。更新请求只修改列出的字段，未提供字段保持原值；`tag_ids: []` 表示清空所有标签。响应对象不得包含自增 `id`、`password` 或 `password_hash`。文件下载返回原始二进制流，不使用 JSON 封套。

健康检查成功时返回 `{"data":{"status":"ok","version":"2.0.0"},"request_id":"..."}`。版本号来自 `backend/internal/version/version.go`，后端代码、接口、配置、数据库迁移或构建配置的每个改动批次都需按 `AGENTS.md` 递增。

当前已部署契约为后端 2.0.0；此处较早的 V1 设计描述已由后续规则替代。注册仅使用 `POST /api/v2/auth/register` multipart，手机号为 11 位数字、密码为 8 至 128 个字符、昵称最多 64 个字符且头像必传；旧 `POST /api/v1/auth/register` 返回 `426 upgrade_required`。登录只接受 11 位数字，兼容旧客户端输入的 `+86` 加 11 位号码并映射到数据库中的 11 位值；手机号不存在和密码错误继续使用相同的 `401` 语义。

反馈正文最多 2000 个 Unicode 字符，最多 5 张图片，至少提交正文或一张图片；单图受 `files.max_upload_bytes` 限制。每个客户端 IP 每小时最多提交 5 次。IP 仅从配置的可信代理链解析，Redis 键保存 IP 摘要。反馈图片 storekey 与正文写入 `feedbacks`，反馈内容不写入日志。新迁移 `000013_feedback_notifications.sql` 建立持久化通知任务并为 webhook 投递增加分段序号；长正文按安全字节上限拆分，UTF-8 字符和图片引用保持完整，每段独立显示投递状态并可单独重试。通知开关默认关闭，不接入短信；启用时使用独立 `feedback.admin_phone` 查找现有账户。飞书的 Markdown 文本组件不把标准 `![...](URL)` 语法作为原生图片；当私有配置 `feedback.feishu_app_id` 和 `feedback.feishu_app_secret` 均已设置时，后台用启用机器人能力且具有图片资源上传权限的自建应用上传反馈图片，再以自定义机器人 `post` 富文本的 `img` 节点投递。没有凭证时发送可点击公共图片链接。凭证由 `000014_seed_feishu_feedback_image_upload.sql` 增加，缺失不影响反馈保存和原有链接通知。

注册和登录的 `data` 包含当前用户资料和 `tokens`：

```json
{
  "data": {
    "user": {
      "business_id": "uuid",
      "nickname": "昵称",
      "phone": "13800138000",
      "avatar_key": "local_yyyymmdd_uuid.jpg",
      "created_at": "2026-10-12T00:00:00Z",
      "updated_at": "2026-10-12T00:00:00Z"
    },
    "tokens": {
      "access_token": "opaque-token",
      "refresh_token": "opaque-token",
      "token_type": "Bearer",
      "expires_in": 7200,
      "refresh_expires_in": 2592000
    }
  },
  "request_id": "request-id"
}
```

刷新请求为 `{ "refresh_token": "..." }`，响应 `data.tokens`，不要求有效 Access Token。`GET /users/me` 和 `PATCH /users/me` 的资料对象使用 `business_id`、`nickname`、`phone`、`avatar_key`、`created_at`、`updated_at` 字段；PATCH 仅允许更新昵称和头像 key。

标签创建和更新请求为 `{ "content": "标签内容" }`，内容最多 64 个字符。标签响应为 `{ "business_id": "uuid", "content": "...", "created_at": "...", "updated_at": "..." }`。标签列表的 `data` 为 `{ "items": [...], "page": 1, "page_size": 20, "total": 0 }`。

灵感图创建请求示例：`{ "tag_ids": ["uuid"], "image_key": "local_yyyymmdd_uuid.ext", "file_name": "原始文件名.png", "data": { "schema_version": 1 } }`。`image_key` 为上传接口返回的 storekey 时，后端读取文件；大于 1 MB 的静态图片会原位覆盖压缩后的内容，不改变 storekey，并根据最终字节计算 `file_hash` 和 `file_size`。`file_size` 是压缩后实际存储的字节数，不接受客户端设置；创建和显式更新 `image_key` 都会重新计算。压缩不透明图片会输出 JPEG，带透明像素的图片会输出 PNG；动画 GIF/WebP 大于目标大小时拒绝保存；下载以实际内容而不是 storekey 扩展名设置 MIME 类型。`file_name` 可选，缺省为空字符串，最多 255 个字符。`data` 必须是 JSON 对象并带正整数 `schema_version`；其他编辑数据字段由编辑器文档版本定义并原样保存。灵感图响应对象包含 `business_id`、`tag_ids`、`image_key`、`file_name`、`file_hash`、`file_size`、`data`、`created_at`、`updated_at`。列表使用同一分页结构。

标签检索 `tag_match` 默认 `any`：命中任一指定标签；`all`：必须同时包含所有指定标签；不传 `tag_ids` 时不按标签过滤。`tag_ids` 以重复 query 参数传入，例如 `?tag_ids=uuid1&tag_ids=uuid2&tag_match=all`。用户归属从 token 得出；访问其他用户的资源统一返回 `404`。列表对象和详情对象都只返回业务 UUID，不返回自增 `id`。

## 6. 验收与未覆盖内容

### 档期管理与客户预约

后端 `2.2.0` 增加独立的档期模块，与收支账本表及接口相互隔离。`schedule_items` 保存工作事项和开放预约时段；`schedule_exceptions` 保存重复系列中单个发生日期的取消或覆盖；`schedule_bookings` 保存访客申请和拥有者处理状态；`schedule_shares` 保存分享令牌摘要。四张业务表均具有标准业务字段、唯一 `business_id` 和中文表/字段注释，不创建外键。`000010_schedule_management.sql` 还新增私有配置 `schedule.booking_rate_limit_per_ip_hour`，初始值为每 IP 每小时 10 次。

所有日期参数和重复规则都按 `Asia/Shanghai` 解释。`none`、`daily`、`weekly` 和 `monthly` 分别表示单次、每日、每周指定星期和每月相同日期；月末不存在目标日期时跳过该次。重复开放时段必须设置结束日期，以便系统在整个有效周期内检查其与工作事项及其他开放时段的冲突；重复工作事项可不设置结束日期。工作事项彼此允许重叠。

拥有者接口位于受保护的 `/api/v1/schedule/`：

- `GET /items?from=YYYY-MM-DD&to=YYYY-MM-DD`：读取最多 90 天的展开后日历项。
- `POST /items`、`PATCH /items/:business_id`、`DELETE /items/:business_id`：创建、替换整个系列、删除整个系列。
- `PATCH /items/:business_id/occurrences/:date`、`DELETE /items/:business_id/occurrences/:date`：覆盖或取消重复系列的单次发生项。
- `GET /bookings?status=pending|confirmed|rejected|expired|all`：列出拥有者申请；读取时同步释放超过 24 小时的待确认名额。
- `POST /bookings/:business_id/confirm` 和 `/reject`：处理有效待确认申请。
- `GET /share`、`POST /share`、`DELETE /share`：查询、生成/轮换和撤销分享链接。服务只保存随机令牌 SHA-256 摘要；明文链接只在创建响应返回。

公开接口 `GET /api/v1/book/:share_token/slots?from=...&to=...` 只返回开放时段和剩余容量；不返回用户 UUID、工作事项、客户资料或数据库内部主键。`POST /api/v1/book/:share_token/requests` 接收 `schedule_id`、`start_at`、`name`、`phone` 和可选 `note`，成功响应返回申请编号。姓名和手机号最多 80/32 个字符，备注最多 1000 个字符。可信代理地址由 Gin 代理配置确定，Redis 以独立键空间按私有配置限流。

预约写入事务会锁定开放时段主记录、核验仍然有效的分享令牌并匹配具体发生项，然后将过期待确认申请改为 `expired`、统计仍占位的 `pending` 和已 `confirmed` 申请，只有容量未满时才插入新申请。待确认申请占位 24 小时；确认后继续占位，拒绝、过期或取消释放名额。没有短信或邮件通知，拥有者在档期管理页确认/拒绝后线下联系客户。

当前前端在桌面和移动 Web 共用 `/schedule` 管理页：月历显示标题/时间摘要并支持选择日期查看完整档期，7/14/自定义 1–90 天视图显示完整卡片； `/book/:shareToken` 为无账号访客页。当前环境通过 Go 测试、独立 PostgreSQL 迁移与并发容量集成用例、前端单元测试及生产构建；未覆盖微信小程序/原生壳真机体验、生产代理链和生产规模并发压力。重复开放时段结束日期是冲突校验要求，因此 UI 会要求设置结束日期。

### 实施验收

- 检查所有五张业务表均包含通用字段，业务 UUID 均唯一，内部 `id` 不出现在 API 契约中。
- 检查标签和灵感图均有 `user_id` 索引；标签数组保存标签业务 UUID 并配置 GIN 索引。
- 检查文档与 `AGENTS.md` 对无外键、GORM 配置和 SQL 脚本归档规则一致。
- 检查所有受保护 API、刷新轮换、会话退出和 Redis 故障语义都要求 Redis 校验。
- Go 服务通过 `go build ./...`；Compose 文件通过 `docker compose config --quiet`。
- 后端 V1 初始实现曾在 PostgreSQL 16 和 Redis 7.4 的隔离容器上完成 70 项 smoke checks，并验证同一个 Refresh Token 并发使用时恰好一项请求成功。2026-10-14 已在 Compose 配置的 PostgreSQL 17.11 容器上应用新增公开配置迁移，验证公开接口和健康检查，并成功构建 Compose API 镜像；完整 70 项用例未在 PostgreSQL 17.11 重跑，生产网络部署仍未验证。

### 当前不包含

- 当前包含 `backend/cmd/api/` 服务入口、Gin HTTP 路由、service、GORM repository、PostgreSQL schema 检查和 Redis token 存储；初始建表脚本为 `backend/migrations/000001_create_v1_schema.sql`。服务启动时拒绝缺表或存在外键的 schema，不会自动建表。
- HTTP 监听参数、请求体上限、超时和 token 生命周期从配置表统一加载；PostgreSQL/Redis 连接参数由环境变量提供。
- 未定义 OSS 签名/上传 API、短信验证码、管理员配置管理 API、完整应用部署编排、备份/高可用方案和生产容量参数。
- Compose PostgreSQL 17.11、Redis 7.4 和 API 镜像已在本机启动并用于公开配置接口联调；生产网络部署仍未验证。本机共享 Go 模块缓存存在校验和不匹配，本次通过隔离缓存完成 Go 构建；模块 checksum 数据库校验未覆盖新依赖。
