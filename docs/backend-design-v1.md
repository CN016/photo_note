# 后端 V1 设计

## 1. 目标与边界

首版后端为跨平台图片与文本编辑器提供账户、标签和灵感图数据接口。技术栈采用 Go、Gin、GORM、PostgreSQL 和 Redis。当前交付仅锁定设计，不包含服务代码、数据库迁移执行器、对象存储上传协议或部署配置。

PostgreSQL 保存配置、用户、标签和灵感图；Redis 保存访问令牌、刷新令牌及其会话索引。配置表只保存非秘密业务配置，密钥和部署凭证由服务运行环境提供。

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
| `config_value` | `text` | 非空，保存非秘密配置值 |
| `description` | `text` | 配置说明 |

配置表仅供服务端管理，V1 不提供面向客户端的配置读写接口。

### 3.2 `users` 用户表

| 字段 | 类型 | 规则 / 用途 |
| --- | --- | --- |
| `nickname` | `varchar(64)` | 昵称 |
| `phone` | `varchar(16)` | 非空、唯一，按 E.164 格式规范化后存储 |
| `password_hash` | `text` | 非空，保存 Argon2id 编码哈希，不保存明文 |
| `avatar_key` | `text` | 头像 OSS key 或由服务管理的文件标识 |

手机号是 V1 登录标识。首版使用手机号和密码注册/登录，不包含短信验证码流程。

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
| `data` | `jsonb` | 非空，保存灵感图复杂数据；作为对象使用并包含 `schema_version` |

创建或更新灵感图时，服务端一次性检查所有 `tag_ids` 是否存在且属于当前用户；任一标签不合法则整次请求失败。删除标签时，在同一 PostgreSQL 事务中从该用户的灵感图 `tag_ids` 中移除对应业务 UUID，再删除标签记录。删除灵感图采用物理删除；V1 不引入软删除字段。

### 3.5 索引清单

- 四张表均对 `business_id` 建唯一索引。
- `app_configs.config_key` 和 `users.phone` 建唯一索引。
- `tags.user_id`、`inspirations.user_id` 建 B-tree 索引，满足按用户隔离和查询的要求。
- `inspirations.tag_ids` 建 GIN 索引；V1 不为 `data` 增加 JSONB 索引，后续只在明确有查询条件时通过迁移添加。

PostgreSQL 的 GIN 适用于索引复合值中的成员（包括数组元素），供标签数组检索使用：[PostgreSQL GIN 索引文档](https://www.postgresql.org/docs/current/gin.html)。

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

统一前缀 `/api/v1`。除注册、登录、刷新以外，所有路由需要有效 Access Token。资源归属一律从 token 中的用户业务 UUID 得出，服务端不接受客户端传入的 `user_id` 覆盖归属。

### 5.1 响应与错误

除 `204`（无响应体）外，成功响应使用 `{ "data": ..., "request_id": "..." }`；错误响应使用 `{ "error": { "code": "...", "message": "...", "details": ... }, "request_id": "..." }`。错误信息不得泄露密码哈希、token 或其他用户是否存在的敏感细节。

| HTTP 状态 | 使用场景 |
| --- | --- |
| `200` | 查询、登录、刷新和更新成功 |
| `201` | 注册或创建资源成功 |
| `204` | 退出或删除成功，无响应体 |
| `400` | JSON、UUID 或查询参数格式错误 |
| `401` | 缺少、无效、过期或已使用的 token |
| `404` | 资源不存在或不属于当前用户 |
| `409` | 手机号或配置 key 等唯一值冲突 |
| `422` | 字段校验失败 |
| `503` | Redis 等必需依赖暂不可用 |
| `500` | 未预期的服务端错误 |

列表接口使用 `page`、`page_size`，默认 1 和 20，`page_size` 最大 100；返回 `items`、`page`、`page_size`、`total`。结果按 `created_at DESC, business_id DESC` 稳定排序。

### 5.2 路由与字段

| 方法与路径 | 鉴权 | 请求字段 / 行为 | 成功结果 |
| --- | --- | --- | --- |
| `POST /auth/register` | 否 | `phone`, `password`, `nickname`；服务端规范化手机号并创建用户 | 用户资料与 token pair |
| `POST /auth/login` | 否 | `phone`, `password` | 用户资料与 token pair |
| `POST /auth/refresh` | 否（请求体带 RT） | `refresh_token`；原子轮换 | 新 token pair |
| `POST /auth/logout` | AT | 撤销当前会话 | `204` |
| `GET /users/me` | AT | 无 | 当前用户 `business_id`、昵称、手机号、头像 key 和时间字段 |
| `PATCH /users/me` | AT | 可选 `nickname`, `avatar_key` | 更新后的当前用户资料 |
| `GET /tags` | AT | 分页参数 | 当前用户标签列表 |
| `POST /tags` | AT | `content` | 新建标签及其业务 UUID |
| `PATCH /tags/{business_id}` | AT | `content` | 更新后的标签 |
| `DELETE /tags/{business_id}` | AT | 删除标签并清理灵感图引用 | `204` |
| `GET /inspirations` | AT | 分页参数，可重复传 `tag_ids` 和 `tag_match=any\|all` | 当前用户灵感图列表 |
| `POST /inspirations` | AT | `tag_ids`, `image_key`, `data` | 新建灵感图及其业务 UUID |
| `GET /inspirations/{business_id}` | AT | 路径业务 UUID | 灵感图详情 |
| `PATCH /inspirations/{business_id}` | AT | 可选 `tag_ids`, `image_key`, `data` | 更新后的灵感图 |
| `DELETE /inspirations/{business_id}` | AT | 路径业务 UUID | `204` |

所有请求和响应使用 UTF-8 JSON；日期时间以 RFC 3339 格式返回。创建请求不得传 `id`、`business_id`、`user_id` 或时间字段，这些由服务端生成或从 token 得出。更新请求只修改列出的字段，未提供字段保持原值；`tag_ids: []` 表示清空所有标签。响应对象不得包含自增 `id`、`password` 或 `password_hash`。

注册请求示例：`{ "phone": "+8613800138000", "password": "...", "nickname": "..." }`。手机号必须是规范化 E.164 值；密码长度为 8 至 128 个字符；昵称最多 64 个字符。登录使用相同的 `phone` 和 `password` 字段；手机号不存在和密码错误使用相同的 `401` 错误语义，避免泄露账户是否存在。

注册和登录的 `data` 包含当前用户资料和 `tokens`：

```json
{
  "data": {
    "user": {
      "business_id": "uuid",
      "nickname": "昵称",
      "phone": "+8613800138000",
      "avatar_key": null,
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

灵感图创建请求示例：`{ "tag_ids": ["uuid"], "image_key": "...", "data": { "schema_version": 1 } }`。`data` 必须是 JSON 对象并带正整数 `schema_version`；其他编辑数据字段由编辑器文档版本定义并原样保存。灵感图响应对象包含 `business_id`、`tag_ids`、`image_key`、`data`、`created_at`、`updated_at`。列表使用同一分页结构。

标签检索 `tag_match` 默认 `any`：命中任一指定标签；`all`：必须同时包含所有指定标签；不传 `tag_ids` 时不按标签过滤。`tag_ids` 以重复 query 参数传入，例如 `?tag_ids=uuid1&tag_ids=uuid2&tag_match=all`。用户归属从 token 得出；访问其他用户的资源统一返回 `404`。列表对象和详情对象都只返回业务 UUID，不返回自增 `id`。

## 6. 验收与未覆盖内容

### 文档验收

- 检查所有四张表均包含通用字段，业务 UUID 均唯一，内部 `id` 不出现在 API 契约中。
- 检查标签和灵感图均有 `user_id` 索引；标签数组保存标签业务 UUID 并配置 GIN 索引。
- 检查文档与 `AGENTS.md` 对无外键、GORM 配置和 SQL 脚本归档规则一致。
- 检查所有受保护 API、刷新轮换、会话退出和 Redis 故障语义都要求 Redis 校验。

### 当前不包含

- 已生成初始建表脚本 `backend/migrations/000001_create_v1_schema.sql`，但尚未在 PostgreSQL 执行；未创建 Go 模块、Gin 服务、GORM 模型、迁移执行器或 Redis 实例配置。
- 未定义 OSS 签名/上传 API、短信验证码、管理员配置 API、部署环境和生产容量参数。
- 未执行 PostgreSQL/Redis 集成验证、构建或应用测试；这些需要在后续实施时按 `AGENTS.md` 要求记录。
