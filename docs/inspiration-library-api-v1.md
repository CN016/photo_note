# 灵感库存图 API 与处理约定（4.2.1）

所有以下接口位于 `/api/v1`，要求现有 Access Token 与可信 tenant.Scope；个人空间 orgid=-1，组织内仍只访问创建者数据。不接受请求指定 user_id/orgid，响应不返回内部自增 ID。现有图库接口保持兼容。

| 接口 | 参数/结果 |
| --- | --- |
| GET /inspirations/daily-goal | goal{enabled,target,reminder_time}、today{day,saved_count,target}、reminder_pending |
| PUT /inspirations/daily-goal | JSON enabled、target（启用时1..100000）、reminder_time（北京时间HH:mm）；只更新当天目标快照 |
| GET /inspirations/activity?year=2026 | 同上并返回年度 days；无历史记录的日期视为0，未启用目标时 target=null |
| POST /inspirations/daily-goal/reminder/ack | JSON day=当前北京时间日期；返回 acknowledged，原子确认一次，关联站内消息标为已读 |
| GET /inspirations/browse-summary | date_from/date_to（YYYY-MM-DD，最多366天）；返回 people/colors/status/days/tags/projects 分组；days 各自最多3张授权缩略图信息 |
| POST /inspirations/{business_id}/processing/retry | 4.2.1起保留兼容路径但返回409，不允许手动触发AI；未识别记录等待每日任务 |

GET /inspirations 在原有参数上增加 `date_from`、`date_to`（包含两端的北京时间入库日）、`project`（项目UUID或unassigned）、`people`（0/1/2/3+）、`empty_shot`（true/false）、`color`（h00..h11、black/white/gray）、`status`（pending_compression/compressing/pending_analysis/analyzing/succeeded/failed/unsupported/canceled）。不同维度按 AND 组合，数据库执行筛选及分页。项目关联同时校验双方空间与创建者。热力图调用独立活动接口，不跟随图库筛选。

灵感图增加 people_count、is_empty_shot、dominant_color、dominant_hue、color_bucket、color_palette、analysis_status、analysis_version、analysis_algorithm、analyzed_at。未知/失败人数与空镜为null；空镜只筛选 succeeded 且人数0。color_palette 包含 version、最多五项 palette[{hex,percentage}]、other_percentage、平均亮度和饱和度；百分数按全图计算，含其他颜色后合计100%。

创建图片、历史日计数加1与持久化任务在同一事务提交；编辑画布/标签不增加计数或重算，替换图片递增代次并清除旧画像，取消旧任务；换图只立即排队压缩和预生成缩略图，AI留待每日未识别扫描。删除不扣减历史计数。迁移仅能回填迁移时仍存在的历史图片，无法恢复此前已删除图片；历史目标不追溯。没有存图或停机的日期在活动查询中沿用最近的每日目标快照；禁用当天的NULL快照会中断延续，未来日期不预填目标。每日提醒事件为 `inspiration.goal.unmet`，事件键包含北京时间日期并在orgid/user_id内唯一，同事务保存站内消息与所有启用渠道outbox。

处理顺序：等待压缩→压缩中→等待识别→识别中→完成。数据库 SKIP LOCKED、随机租约令牌、图片key和代次比较避免重复领取/迟到覆盖；租约到期后恢复。停止或重启不会丢失数据库任务，但未到期租约须等待到期。重试延迟默认60/300/1800秒，达到上限后等待每日未识别扫描。无法授权读取的历史图片不抓取外部URL，不支持项单独展示。

私有系统配置 `inspirations.processing`（orgid=-1）统一控制启用、算法、03:00扫描时刻、压缩2/识别10并发、100ms轮询、压缩300秒/识别30秒超时、600秒租约、重试间隔、Python/脚本/模型/Socket路径、5000万像素上限。仅首次新增允许压缩后异步识别；每日到点扫描只选择analyzed_at IS NULL的当前图片，每批500张按内部ID游标处理。没有启动补扫，不因算法版本过期重新识别成功记录；已许可任务的重试和租约恢复属于原任务续跑。服务若停机错过当天时刻，不在启动或打开页面时补跑。并发、暂停、时间、超时、重试由worker轮询读取；算法版本和进程路径/模型等启动装配项修改后需重启API，改变模型时必须同步算法版本。

部署保持单API容器及现有Compose。先备份数据库，再按顺序执行未应用迁移（000023与本批000024），API启动只检查，不自动迁移。先回载已交付Alpine AI基础镜像，再构建API。Linux amd64已验证；Windows原生Go运行及其他架构需另配Python/Socket路径与运行环境，不能视为已验证。

## 4.2.1 缩略图读取

`GET /api/v1/files/{storekey}/thumbnail` 使用现有鉴权中间件及FileService归属校验；返回image/jpeg。命中磁盘缓存也重新验证空间、创建者与源文件存在性，删除或无权限返回404。原图下载接口不改，缩略图仍使用private,no-store，防止共享HTTP缓存绕过私有文件授权。

私有配置files.thumbnails控制dimension=384、quality=78、max_pixels=40000000、workers=2，最大输入字节复用files.max_upload_bytes。并发在启动装配时读取，修改需要重启；其他缩略参数按请求读取。缩略图作为派生JPEG保存在原文件同一持久目录，名称由源文件大小/修改时间/尺寸/质量指纹确定；不新增公开storekey或认领文件归属。换图原子替换后失效旧缓存，删除源文件同步清理副本。首次历史图片缓存未命中时仅生成显示副本，不创建AI任务；新增/换图的压缩完成阶段预生成副本，生成失败不回滚已保存图片。

前端以会话ID/组织/会话代次隔离48项、16MiB、5分钟内存Blob缓存，同图合并在途请求并限制最多4个下载；退出/切换空间清理，旧响应不能进入新空间。日历预览同步返回file_hash，压缩内容变化会重新读取。JPEG/PNG/WebP/GIF及BMP/TIFF由服务器生成小图，浏览器不重复编码；历史HEIC/AVIF保留原客户端兼容解码，转换也合并缓存。损坏或超像素限制的普通图片返回400，不走兼容回退绕过限制；特殊格式415才允许兼容。原生平台与特殊格式本批没有真机新增验收。
