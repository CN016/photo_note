# 多租户与组织管理接口（4.0.0）

## 身份和数据范围

账号、手机号和头像为全局身份；注册、手机号密码登录默认进入个人空间 `orgid=-1`。一个账号可加入多个组织，每个会话只绑定一个当前空间。组织编号是独立正整数业务编号，不使用内部自增主键，最大为 JavaScript 安全整数；组织和成员仍有全局唯一业务 UUID。

全部 23 张业务表含 `orgid`。全局账号、系统配置、反馈和反馈任务固定为 `-1`。其余私有业务按“当前空间＋创建者”过滤，个人版也必须同时检查 `user_id`。加入组织不迁移已有个人数据；移除成员保留其原组织数据，重新加入可恢复访问。组织只提供停用和启用，不提供硬删除。

Bearer token 与现有成功/错误封套保持不变。注册、登录、刷新响应的 `tokens` 新增 `orgid`、`session_id`、`session_version`；`GET /api/v1/users/me` 新增 `orgid` 和 `organization_role`（`personal/admin/member`）。`is_admin` 仍仅代表平台角色。旧版无空间信息的 token 拒绝使用，升级后需要重新登录。

## 空间切换

- `GET /api/v1/environments`：`data.items` 返回当前账号的空间，字段为 `orgid/business_id/name/role/status`，个人空间的组织 UUID 为空。
- `POST /api/v1/auth/switch-environment`：请求为 `refresh_token`、目标 `orgid`、`expected_session_version`、随机 UUID `switch_id`，响应为 `data.tokens`。
- 刷新保留空间；切换验证目标成员关系和组织状态，Redis 原子增加空间版本并撤销旧 token。相同切换标识提供有限期恢复能力，Redis 不保存明文 token。来源组织停用或成员失效后仍可用当前刷新凭证切回个人空间。
- 普通业务每次重新校验组织与成员状态，客户端不能用请求中的 `orgid` 覆盖会话范围；失效不会自动将原写入重发到个人空间。

Web 所有标签页同步切换：先冻结交互、执行离开守卫、完成旧空间保存及上传，再通过 Web Locks 屏障和凭证锁提交。未完成的手动表单或上传会阻止切换，用户处理后重试。准备阶段最多等待 30 秒；结果未知时要求重新登录，不猜测旧令牌仍有效。没有 Web Locks/BroadcastChannel 的浏览器不能执行同步切换。迟到响应、旧代次广播和旧请求重试不能污染新空间。

## 组织和成员管理

平台管理前缀为 `/api/v1/atelier-7f3c9a2e`：

| 方法与路径 | 内容 |
| --- | --- |
| `GET /organizations` | `page/page_size/q` 分页检索 |
| `POST /organizations` | `{name, initial_admin_user_id}` 创建组织及首位管理员 |
| `PATCH /organizations/{orgid}` | 修改 `name` 或 `status`（`active/disabled`） |
| `GET /organizations/{orgid}/members` | 有效成员分页 |
| `POST /organizations/{orgid}/members` | `{user_id, role}` 添加或恢复已有账号 |
| `PATCH /organizations/{orgid}/members/{user_id}` | `{role}` 设置 `admin/member` |
| `DELETE /organizations/{orgid}/members/{user_id}` | 移除成员 |

组织管理员使用 `/api/v1/organization` 的 `GET/PATCH` 读取或修改当前组织名称；`/members` 和 `/members/{user_id}` 提供上述成员操作。成员使用用户业务 UUID，不通过昵称猜测账号。事务串行保护最后一位有效管理员；组织管理员不能停启组织、授权平台管理员或访问全局配置、用户管理及反馈。

## 图库管理与文件授权

普通用户只能访问本人资源。组织管理员可通过 `/api/v1/organization/inspirations` 查看、编辑、删除本组织成员灵感图，项目、计划、账本、档期、消息和 webhook 保持创建者权限。

平台图库为 `/api/v1/atelier-7f3c9a2e/inspirations`，必须显式且只提供一个 `orgid` 查询参数；`-1` 为受控的个人版图库管理范围。不提供省略组织后全站混查。平台选择的管理范围不改变普通业务会话。

两个图库前缀均支持列表 `GET`、`/{business_id}` 的 `GET/PATCH/DELETE`、`/{business_id}/tags` 的 `GET`、`/{business_id}/files` 的 `POST`、`/{business_id}/file?storekey=...` 与 `/{business_id}/files/{storekey}` 的 `GET`。编辑复用原有字段，保留创建者；管理文件读取只允许该图实际引用的文件。被项目引用时禁止删除，错误不泄露私有项目内容。

私有文件在 `file_ownerships` 登记 `storekey/orgid/user_id`。上传登记失败回收新文件；私有读取、引用、替换和删除均校验归属，不允许客户端认领未知 key。`GET/POST /api/v1/users/me/avatar` 专供本人头像，固定个人范围，上传字段为 `file`，验证实际位图内容；普通文件下载没有跨空间回退。

历史文件由头像、灵感图、计划封面、项目封面和附件引用回填，保留原 key 与物理位置。多账号引用同一 key 时登记未知归属并拒绝访问，不自动任选所有者；无引用孤立文件不自动授予权限。

## 系统功能、异步任务与升级

反馈由平台统一处理，组织管理员无反馈权限。反馈、反馈任务固定系统域 `-1`，不受当前来源组织失效影响。反馈回复在提交者个人消息中心查看，通知外发只使用接收人的个人 webhook，不使用管理员当前组织或接收人的其他组织渠道。系统配置共用 `app_configs.orgid=-1`，不提供组织覆盖。

公开档期、游客预约和反馈图片保留原公开访问方式，不增加登录或到期签名限制；预约从分享记录推导组织与创建者。私有异步任务携带原组织、所有者和业务代次，关联及回写使用同一范围；通知发送前复查组织、成员和渠道，撤权后的待发送任务取消。已发出的外部消息不能撤回。

`000021_multitenant_organizations.sql` 给原 20 表增加组织字段、约束和索引，并新建组织、成员、私有文件授权表；所有字段附中文注释，不建外键。租户业务唯一约束及 upsert 冲突目标加入 `orgid`，保留全局主键、UUID、手机号和公开令牌唯一性；GIN 保留并配合组织 BTree 过滤，全局 worker 保留调度扫描索引。启动检查全部表和组织字段，缺少迁移时拒绝启动。

升级须先备份数据库与文件，停止旧 API/worker，执行尚未应用的迁移并核对历史文件冲突，再同步发布 4.0.0 前后端、启动并重新登录。含组织数据后不能直接退回不识别组织的旧版本。
