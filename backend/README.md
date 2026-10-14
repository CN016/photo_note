# 后端服务

后端服务入口位于 `cmd/api/`。服务使用 `migrations/` 中的版本化 SQL 初始化数据库结构和运行配置；启动时会检查 V1 表是否存在、当前 schema 是否含外键以及配置表中的必需运行项，不会自动执行 GORM AutoMigrate。

从仓库根目录复制 `backend/.env.example` 为 `backend/.env`，替换 PostgreSQL 和 Redis 密码后运行：

```sh
docker compose -f backend/docker-compose.yml up -d --build
```

API 默认通过宿主机 `127.0.0.1:8089` 提供服务，监听地址、请求上限、超时和 token 有效期从 `app_configs` 读取；`000002_seed_runtime_config.sql` 为新数据库添加初始值。readiness 地址为 `/healthz`，调用时也需要有效的 Bearer Access Token。Compose 内的服务使用 `POSTGRES_USER` 连接 PostgreSQL；该账号是 PostgreSQL 初始化管理员，适合本地快速部署。生产环境应预先创建权限受限的数据库账号，并设置 `DB_USER`、`DB_PASSWORD` 覆盖默认值。

### 构建镜像源

Compose 默认通过 DaoCloud Docker Hub 代理拉取 Go 和 Alpine 基础镜像，并使用 `goproxy.cn` 下载 Go 模块。Dockerfile 保留 `go.mod`、`go.sum` 独立层，依赖清单未变化时，Docker 可复用 Go 模块下载层。Docker 镜像代理和 Go 模块代理是两项独立设置；如果服务器无法访问默认代理，可在启动 Compose 的 shell 中覆盖：

```sh
GO_BASE_IMAGE=golang:1.24-alpine \
RUNTIME_BASE_IMAGE=alpine:3.22 \
GOPROXY=https://proxy.golang.org,direct \
docker compose -f backend/docker-compose.yml up -d --build
```

以上变量是构建参数，不需要写入 `backend/.env`。镜像源应按部署服务器的网络选择；构建速度取决于服务器到镜像仓库和 Go 模块代理的实际连接。

### 本地构建并导入服务器

目标服务器为 Linux `amd64` 时，可在本机预构建并导出镜像：

```sh
docker buildx build --platform linux/amd64 \
  --build-arg GO_BASE_IMAGE=m.daocloud.io/docker.io/library/golang:1.24-alpine \
  --build-arg RUNTIME_BASE_IMAGE=m.daocloud.io/docker.io/library/alpine:3.22 \
  --build-arg GOPROXY=https://goproxy.cn,direct \
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
