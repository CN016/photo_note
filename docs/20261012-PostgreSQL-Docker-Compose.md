# PostgreSQL 17 Docker Compose

## 改动目的

提供可直接启动的 PostgreSQL 17 容器配置，并为本地部署准备独立的数据库管理员和应用运行账号。

## 主要内容

- 新增 `backend/docker-compose.yml`：启动 PostgreSQL 17，绑定本机 `127.0.0.1:5432`，使用命名数据卷并提供健康检查。
- 容器首次初始化时按文件名执行 `backend/migrations/` 中的 SQL：先创建 V1 表，再创建权限受限的 `photo_nini_app` 运行账号。
- 新增 `backend/.env.example` 作为凭证格式示例；本地真实密码放在 `backend/.env`，并加入 `.gitignore`。
- 更新 `docs/backend-design-v1.md`，记录 Compose、首启初始化及尚未包含的部署能力。
- 在本机 `backend/.env` 中生成独立的随机管理员和应用账号密码；从仓库根目录可用 `docker compose -f backend/docker-compose.yml up -d` 启动。

## 验证

- 执行 `docker compose -f backend/docker-compose.yml config --quiet`，退出码为 0，Compose 配置校验通过。
- 确认 `backend/.env` 被 `.gitignore` 排除；配置校验和改动检查均未输出密码。

## 未验证内容与限制

- 未启动 PostgreSQL 容器，未在数据库中实际执行建表和账号授权 SQL。
- 官方 PostgreSQL 镜像只会在空数据目录首次初始化时执行 `/docker-entrypoint-initdb.d` 脚本；已有数据卷不会自动补跑新增初始化脚本。
- 当前端口只绑定到宿主机回环地址；远程访问应按部署网络和防火墙策略另行配置。
