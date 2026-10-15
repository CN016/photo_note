# 后端服务

后端服务入口位于 `cmd/api/`。服务使用 `migrations/` 中的版本化 SQL 初始化数据库结构和运行配置；启动时会检查 V1 表是否存在、当前 schema 是否含外键以及配置表中的必需运行项，不会自动执行 GORM AutoMigrate。

从仓库根目录复制 `backend/.env.example` 为 `backend/.env`，替换 PostgreSQL 和 Redis 密码后运行：

```sh
docker compose -f backend/docker-compose.yml up -d --build
```

API 默认通过宿主机 `127.0.0.1:8089` 提供服务，监听地址、请求上限、超时、token 有效期和文件存储配置从 `app_configs` 读取；`000002_seed_runtime_config.sql` 和 `000003_seed_file_storage_config.sql` 为新数据库添加初始值。readiness 地址 `/healthz` 和公开配置地址 `GET /api/v1/configs/public` 均无需鉴权；健康检查成功响应包含 `data.version` 当前后端版本。版本号统一维护在 `internal/version/version.go`，每批后端改动都必须按仓库根目录 `AGENTS.md` 递增。公开配置接口只返回 `is_public=true` 的配置项 key/value；可不传参数读取全部开放项，或重复传 `keys` 查询特定 key，例如 `?keys=frontend.theme&keys=frontend.upload_limit`。现有数据库升级时需备份后单独执行新增迁移 `backend/migrations/000004_add_config_public_flag.sql`；历史迁移保持不变，新增字段默认关闭。Compose 内的服务使用 `POSTGRES_USER` 连接 PostgreSQL；该账号是 PostgreSQL 初始化管理员，适合本地快速部署。生产环境应预先创建权限受限的数据库账号，并设置 `DB_USER`、`DB_PASSWORD` 覆盖默认值。

文件上传使用 `POST /api/v1/files`，请求为单文件 `multipart/form-data`，字段名为 `file`；上传大小由配置表中的 `files.max_upload_bytes` 控制，默认 50 MiB。当前 `files.active_store_type` 默认为 `local`，上传时每次读取该配置；文件 key 的类型前缀用于后续下载分发。下载使用 `GET /api/v1/files/{storekey}`，上传和下载均需 Bearer Access Token。V1 不建立文件元数据、不绑定用户，也不提供文件列表、删除和自动清理接口；拥有有效 storekey 的已鉴权用户都能获取对应文件。`oss` 类型已保留 key 路由分支，但当前尚未接入 OSS 后端，切换到 `oss` 后上传和读取会返回 501。

本地文件默认写入 `/var/lib/photo-nini/uploads/<yyyymmdd>/<uuid前三位>/<uuid>.<ext>`，由 Compose 命名卷 `uploaded_files_data` 持久化。`files.local_root` 修改后需重启 API；单文件大小限制 `files.max_upload_bytes` 也是启动时读取。新上传的存储类型每次上传时从配置表重新读取，变更后立即对下一次上传生效。

灵感图 `POST`/`PATCH` 请求可传 `file_name` 保存原始图片文件名。创建记录或通过 `image_key` 更换有效 storekey 后，接口先保存记录并快速返回，再由固定并发数的后台工作线程读取 JPEG、PNG、GIF 或 WebP 图片并压缩；任务完成前响应中的 `file_hash` 和 `file_size` 为 `null`。超过 1 MB 的静态图片会压缩并原位覆盖同一 storekey，不生成新 key；不透明图片转为 JPEG，有透明通道的图片保留为 PNG。超过 1 MB 的动画 GIF 或 WebP、无效图片、文件缺失或超过服务端处理上限时，异步任务失败，不会作为本次 `POST`/`PATCH` 的 HTTP 错误返回，`file_hash` 和 `file_size` 会保持为空；重新提交图片 key 可触发新任务。处理成功后，后端基于最终存储字节保存 SHA-256（小写 64 位十六进制）到 `file_hash`，并将实际字节数保存到 `file_size`，单位为字节。小于或等于 1 MB 的图片保持原始内容。读取时按文件实际内容返回 MIME 类型和下载扩展名，因此压缩后即使编码格式变化，原 storekey 仍可使用。每条记录的 `async_version` 在带图片的异步更新时递增，后台任务仅在图片 key 和代次仍与任务一致时写回元数据；新增记录的代次为 0。同一记录尚未执行的旧任务会被最新代次替换。该队列保存在 API 进程内，正常收到退出信号后会在关闭超时内等待已接收任务，进程异常终止时仍可能丢失未完成任务。更换为非 storekey 的 `image_key` 时哈希和大小会清空。现有数据库升级需确认并按顺序补跑尚未应用的 `backend/migrations/000005_add_inspiration_file_metadata.sql`、`backend/migrations/000006_add_inspiration_file_size.sql` 和 `backend/migrations/000007_add_inspiration_async_version.sql`；如果 `000005`、`000006` 已应用，则仅需应用 `000007`。历史记录的异步代次默认为 0。

账本使用 `ledger_records` 表逐笔保存已完成收支和可恢复草稿；金额在数据库中保存为整数分，API 使用两位小数的十进制字符串。所有 `/api/v1/ledger/*` 接口均需 Bearer Access Token，并从令牌读取用户业务 UUID 限定查询和写入。`GET /api/v1/ledger/records?from=YYYY-MM-DD&to=YYYY-MM-DD` 返回日期范围内的正式记录及当前用户的未完成草稿；`POST /api/v1/ledger/records` 创建草稿，`PATCH /api/v1/ledger/records/{business_id}/draft` 自动保存快照并校验草稿修订号，`POST /api/v1/ledger/records/{business_id}/complete` 完成草稿或应用编辑，`DELETE /api/v1/ledger/records/{business_id}` 删除记录，`POST /api/v1/ledger/import` 在单个事务中导入完整记录。正式分析不包含草稿；编辑已完成记录期间，统计仍按原正式字段计算。旧营收日历 JSON/CSV 导入时类型默认为收入。已有数据库升级前先备份 PostgreSQL，再确认并按顺序应用尚未执行的版本化迁移；账本表迁移为 `backend/migrations/000009_create_ledger_records.sql`，API 不会自动执行 SQL。

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
