# 灵感库存图 API 与处理约定（4.2.0）

所有以下接口位于 `/api/v1`，要求现有 Access Token 与可信 tenant.Scope；个人空间 orgid=-1，组织内仍只访问创建者数据。不接受请求指定 user_id/orgid，响应不返回内部自增 ID。现有图库接口保持兼容。

| 接口 | 参数/结果 |
| --- | --- |
| GET /inspirations/daily-goal | goal{enabled,target,reminder_time}、today{day,saved_count,target}、reminder_pending |
| PUT /inspirations/daily-goal | JSON enabled、target（启用时1..100000）、reminder_time（北京时间HH:mm）；只更新当天目标快照 |
| GET /inspirations/activity?year=2026 | 同上并返回年度 days；无历史记录的日期视为0，未启用目标时 target=null |
| POST /inspirations/daily-goal/reminder/ack | JSON day=当前北京时间日期；返回 acknowledged，原子确认一次，关联站内消息标为已读 |
| GET /inspirations/browse-summary | date_from/date_to（YYYY-MM-DD，最多366天）；返回 people/colors/status/days/tags/projects 分组；days 各自最多3张授权缩略图信息 |
| POST /inspirations/{business_id}/processing/retry | 当前代次失败或不支持任务重试，202；成功任务不能重复重试，不增加每日次数 |

GET /inspirations 在原有参数上增加 `date_from`、`date_to`（包含两端的北京时间入库日）、`project`（项目UUID或unassigned）、`people`（0/1/2/3+）、`empty_shot`（true/false）、`color`（h00..h11、black/white/gray）、`status`（pending_compression/compressing/pending_analysis/analyzing/succeeded/failed/unsupported/canceled）。不同维度按 AND 组合，数据库执行筛选及分页。项目关联同时校验双方空间与创建者。热力图调用独立活动接口，不跟随图库筛选。

灵感图增加 people_count、is_empty_shot、dominant_color、dominant_hue、color_bucket、color_palette、analysis_status、analysis_version、analysis_algorithm、analyzed_at。未知/失败人数与空镜为null；空镜只筛选 succeeded 且人数0。color_palette 包含 version、最多五项 palette[{hex,percentage}]、other_percentage、平均亮度和饱和度；百分数按全图计算，含其他颜色后合计100%。

创建图片、历史日计数加1与持久化任务在同一事务提交；编辑画布/标签不增加计数或重算，替换图片递增代次并清除旧画像，取消旧任务。删除不扣减历史计数。迁移仅能回填迁移时仍存在的历史图片，无法恢复此前已删除图片；历史目标不追溯。没有存图或停机的日期在活动查询中沿用最近的每日目标快照；禁用当天的NULL快照会中断延续，未来日期不预填目标。每日提醒事件为 `inspiration.goal.unmet`，事件键包含北京时间日期并在orgid/user_id内唯一，同事务保存站内消息与所有启用渠道outbox。

处理顺序：等待压缩→压缩中→等待识别→识别中→完成。数据库 SKIP LOCKED、随机租约令牌、图片key和代次比较避免重复领取/迟到覆盖；租约到期后恢复。停止或重启不会丢失数据库任务，但未到期租约须等待到期。重试延迟默认60/300/1800秒，达到上限后允许手动重试。无法授权读取的历史图片不抓取外部URL，不支持项单独展示。

私有系统配置 `inspirations.processing`（orgid=-1）统一控制启用、算法、03:00扫描时刻、压缩2/识别10并发、100ms轮询、压缩300秒/识别30秒超时、600秒租约、重试间隔、Python/脚本/模型/Socket路径、5000万像素上限。启动补扫与每日到点扫描各自执行，按内部ID每批500张游标处理。并发、暂停、时间、超时、重试由worker轮询读取；算法版本和进程路径/模型等启动装配项修改后需重启API，改变模型时必须同步算法版本。

部署保持单API容器及现有Compose。先备份数据库，再按顺序执行未应用迁移（本批000023），API启动只检查，不自动迁移。先回载已交付Alpine AI基础镜像，再构建API。Linux amd64已验证；Windows原生Go运行及其他架构需另配Python/Socket路径与运行环境，不能视为已验证。
