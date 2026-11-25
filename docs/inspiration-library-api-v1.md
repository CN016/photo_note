# 灵感库存图 API 与处理约定（4.13.0）

所有以下接口位于 `/api/v1`，要求现有 Access Token 与可信 tenant.Scope；个人空间 orgid=-1，组织内仍只访问创建者数据。不接受请求指定 user_id/orgid，响应不返回内部自增 ID。现有图库接口保持兼容。

| 接口 | 参数/结果 |
| --- | --- |
| GET /inspirations/daily-goal | goal{enabled,target,reminder_time}、today{day,saved_count,target}、reminder_pending |
| PUT /inspirations/daily-goal | JSON enabled、target（启用时1..100000）、reminder_time（北京时间HH:mm）；只更新当天目标快照 |
| GET /inspirations/activity?year=2026 | 同上并返回年度 days；无历史记录的日期视为0，未启用目标时 target=null |
| POST /inspirations/daily-goal/reminder/ack | JSON day=当前北京时间日期；返回 acknowledged，原子确认一次，关联站内消息标为已读 |
| GET /inspirations/browse-summary | date_from/date_to（YYYY-MM-DD，最多366天）；返回 people/colors/status/days/tags/projects 分组；days 各自最多3张授权缩略图信息 |
| POST /inspirations/{business_id}/processing/retry | 4.2.1起保留兼容路径但返回409，不允许手动触发AI；未识别记录等待每日任务 |

GET /inspirations 在原有参数上增加 `date_from`、`date_to`（包含两端的北京时间入库日）、`project`（项目UUID或unassigned）、`people`（兼容旧客户端的 0/1/2/3+）、`people_mode`（present/absent/exact/range）、`people_min`、`people_max`、`empty_shot`（true/false）、`color`、`status`、`tag_source`（manual/ai）。`q` 同时检索文件名、`data.image_note` 整图备注和 `data.anchors[].note` 锚点文字，匹配不区分大小写。快捷人数“无人”使用 absent，1 至 6 人使用 exact，“多人”使用 6 至 100000 的 range；高级入口继续保留 present/absent/exact/range。`color` 同时兼容旧颜色桶和 `#rrggbb` 任意色值；传十六进制色并设置 `sort=color_match` 时，只返回已有颜色画像的成功记录，并按每张图片 `color_palette.palette` 前三个代表色的加权 RGB 距离升序排列。`tag_source` 在使用 `tag_ids` 时限定标签分配来源；`manual` 要求标签未出现在当前图片自动标签中，`ai` 要求同名自动标签存在。不同维度按 AND 组合，数据库执行筛选及分页。项目关联同时校验双方空间与创建者。热力图调用独立活动接口，不跟随图库筛选。

标签列表 `GET /tags` 的每个标签新增可选 `source`：`manual` 表示仅由用户分配、`ai` 表示仅由自动标签产生、`mixed` 表示该用户不同图片同时存在人工和 AI 分配、`unused` 表示当前没有图片引用。自动标签继续独立保存在 `auto_tags`，并与 `tag_ids` 合并；同一张图片若人工选择了与 AI 同名的标签，现有结构无法区分这次重复确认，会按 AI 来源处理。

灵感图增加 people_count、is_empty_shot、dominant_color、dominant_hue、color_bucket、color_palette、analysis_status、analysis_version、analysis_algorithm、analyzed_at。未知/失败人数与空镜为null；空镜只筛选 succeeded 且人数0。color_palette 包含 version、最多五项 palette[{hex,percentage}]、other_percentage、平均亮度和饱和度；百分数按全图计算，含其他颜色后合计100%。

4.11.0 增加 `inspirations.auto_tags jsonb`。成功识别时保存模型/规则版本、标签名称、来源和分数；标签同时复用或创建当前用户标签并合并到 `tag_ids`，所以客户端仍通过已有 `tag_ids/tag_names` 读取和筛选。`auto_tags=null` 表示从未完成自动标签，每日任务会补扫；用户后来删除某个标签时不会因为自动标签记录仍在而被后台立即重加。

创建图片、历史日计数加1与持久化任务在同一事务提交；编辑画布/标签不增加计数或重算，替换图片递增代次并清除旧画像，取消旧任务；换图只立即排队压缩和预生成缩略图，AI留待每日未识别扫描。删除不扣减历史计数。迁移仅能回填迁移时仍存在的历史图片，无法恢复此前已删除图片；历史目标不追溯。没有存图或停机的日期在活动查询中沿用最近的每日目标快照；禁用当天的NULL快照会中断延续，未来日期不预填目标。每日提醒事件为 `inspiration.goal.unmet`，事件键包含北京时间日期并在orgid/user_id内唯一，同事务保存站内消息与所有启用渠道outbox。

处理顺序：等待压缩→压缩中→等待识别→识别中→完成。识别阶段同时返回人数、颜色和自动标签；数据库 SKIP LOCKED、随机租约令牌、图片key和代次比较避免重复领取/迟到覆盖；租约到期后恢复。停止或重启不会丢失数据库任务，但未到期租约须等待到期。重试延迟默认60/300/1800秒，达到上限后等待每日自动标签扫描。无法授权读取的历史图片不抓取外部URL，不支持项单独展示。

历史图片无需新增搜索列：`color_palette` 已保存前五代表色，本批只读取前三种；`auto_tags` 与 `tag_ids` 可在查询时推导标签来源。对 `auto_tags IS NULL` 的历史成功图片，现有每日扫描会进入 `pending_analysis` 并重新计算颜色和目标标签。需要在当天扫描前主动排队时，可先备份数据库，再按组织与用户范围把目标记录的 `auto_tags` 置空；不要在未限定 `orgid/user_id` 的生产库上批量更新：

```sql
BEGIN;
UPDATE inspirations
SET auto_tags = NULL, updated_at = CURRENT_TIMESTAMP
WHERE orgid = -1
  AND user_id = '替换为当前用户业务 UUID'::uuid;
COMMIT;
```

下一次每日扫描（默认北京时间 03:00）会分批为该范围重新生成颜色前五代表色、人数和自动标签；服务停机错过扫描时刻时不会因打开页面立即补跑。

私有系统配置 `inspirations.processing`（orgid=-1）统一控制启用、算法、03:00扫描时刻、压缩2/识别10并发、10个自动标签模型推理、100ms轮询、压缩300秒/识别30秒超时、600秒租约、重试间隔、YOLOX/颜色/SigLIP路径、标签向量路径、top-k/最低分数、Socket路径和5000万像素上限。上传成功后立即持久化任务并异步完成压缩、识别和打标；每日到点扫描 `analysis_status<>'succeeded' OR auto_tags IS NULL` 的当前图片，每批500张按内部ID游标处理。没有启动补扫，也不因算法版本变化重复识别已成功的自动标签。已许可任务的重试和租约恢复属于原任务续跑；服务若停机错过当天时刻，不在启动或打开页面时补跑。并发、暂停、时间、超时、重试由worker轮询读取；算法版本和进程路径/模型等启动装配项修改后需重启API，改变模型时必须同步算法版本。

标签模型使用 Apache-2.0 的 SigLIP 2 Base patch16-224 FP16 视觉编码器；文本编码器只在构建前生成127个固定标签向量，运行时不加载。候选包含人员、场景、物体、色彩、光线、构图、风格、情绪，以及大远景、远景、全景、中景、中近景、近景、特写、大特写、半身、全身和胸像等景别。另按人数、空镜、横竖构图、主色、明暗和饱和度补充确定性标签。单图通常返回约24个标签，最多30个。

AI服务在Docker `--memory=2g --cpus=2`下实测最大推理重叠数为10：10张图约14.20秒，Python进程峰值RSS约1.228GB，单图24个标签。请求正文总预算256MB、解码像素预算384MB；模型只加载一份共享Session，并限制ORT内部单线程，避免十任务产生额外线程池。该结果只证明单AI容器内存可用，部署时仍需为Go API、PostgreSQL、Redis和宿主系统预留内存。

部署保持单API容器及现有Compose。先备份数据库，再按顺序执行未应用迁移（至少包含 `000023`、`000024` 与本批 `000030_inspiration_auto_tags.sql`），API启动只检查，不自动迁移。4.11.1起PostgreSQL连接固定使用pgx简单协议，避免应用长期连接在迁移增列后继续复用旧`SELECT *`结果描述并返回SQLSTATE `0A000`。构建前先运行 `backend/ai/prepare_tag_embeddings.py` 生成并校验FP16视觉模型；ONNX模型不提交Git，最终API镜像必须包含视觉模型、标签向量和许可文件。Linux amd64已验证；Windows原生Go运行及其他架构需另配Python/Socket路径与运行环境，不能视为已验证。

## 4.2.1 缩略图读取

`GET /api/v1/files/{storekey}/thumbnail` 使用现有鉴权中间件及FileService归属校验；返回image/jpeg。命中磁盘缓存也重新验证空间、创建者与源文件存在性，删除或无权限返回404。原图下载接口不改，缩略图仍使用private,no-store，防止共享HTTP缓存绕过私有文件授权。

私有配置files.thumbnails控制dimension=384、quality=78、max_pixels=40000000、workers=2，最大输入字节复用files.max_upload_bytes。并发在启动装配时读取，修改需要重启；其他缩略参数按请求读取。缩略图作为派生JPEG保存在原文件同一持久目录，名称由源文件大小/修改时间/尺寸/质量指纹确定；不新增公开storekey或认领文件归属。换图原子替换后失效旧缓存，删除源文件同步清理副本。首次历史图片缓存未命中时仅生成显示副本，不创建AI任务；新增/换图的压缩完成阶段预生成副本，生成失败不回滚已保存图片。

前端以会话ID/组织/会话代次隔离48项、16MiB、5分钟内存Blob缓存，同图合并在途请求并限制最多4个下载；退出/切换空间清理，旧响应不能进入新空间。日历预览同步返回file_hash，压缩内容变化会重新读取。JPEG/PNG/WebP/GIF及BMP/TIFF由服务器生成小图，浏览器不重复编码；历史HEIC/AVIF保留原客户端兼容解码，转换也合并缓存。损坏或超像素限制的普通图片返回400，不走兼容回退绕过限制；特殊格式415才允许兼容。原生平台与特殊格式本批没有真机新增验收。
