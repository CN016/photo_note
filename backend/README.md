# 后端服务

后端服务入口位于 `cmd/api/`。服务使用 `migrations/` 中的版本化 SQL 初始化数据库结构和运行配置；启动时会检查 V1 表是否存在、当前 schema 是否含外键以及配置表中的必需运行项，不会自动执行 GORM AutoMigrate。

从仓库根目录复制 `backend/.env.example` 为 `backend/.env`，替换 PostgreSQL 和 Redis 密码后运行：

```sh
docker compose -f backend/docker-compose.yml up -d --build
```

API 默认通过宿主机 `127.0.0.1:8080` 提供服务，监听地址、请求上限、超时和 token 有效期从 `app_configs` 读取；`000002_seed_runtime_config.sql` 为新数据库添加初始值。readiness 地址为 `/healthz`，调用时也需要有效的 Bearer Access Token。Compose 内的服务使用 `POSTGRES_USER` 连接 PostgreSQL；该账号是 PostgreSQL 初始化管理员，适合本地快速部署。生产环境应预先创建权限受限的数据库账号，并设置 `DB_USER`、`DB_PASSWORD` 覆盖默认值。

PostgreSQL 初始化脚本只会在空数据卷首次启动时执行。已有数据卷升级时，应先备份数据库，再通过 PostgreSQL 客户端按顺序执行尚未应用的版本化迁移文件。
