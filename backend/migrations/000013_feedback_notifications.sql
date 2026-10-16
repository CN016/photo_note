BEGIN;

ALTER TABLE notice_deliveries
    ADD COLUMN part_index smallint NOT NULL DEFAULT 1,
    ADD COLUMN part_count smallint NOT NULL DEFAULT 1,
    ADD CONSTRAINT ck_notice_deliveries_part_range CHECK (part_index >= 1 AND part_count >= part_index);

COMMENT ON COLUMN notice_deliveries.part_index IS '当前 webhook 消息分段序号，从 1 开始，用于分别展示和重试每段投递。';
COMMENT ON COLUMN notice_deliveries.part_count IS '同一通知在该渠道拆分后的总段数，至少为当前分段序号。';

CREATE TABLE feedback_notification_jobs (
    id                  SERIAL PRIMARY KEY,
    business_id         UUID NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    feedback_business_id UUID NOT NULL,
    job_status          VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempt_count       SMALLINT NOT NULL DEFAULT 0,
    next_attempt_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_attempt_at     TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    last_error_code     VARCHAR(64) NOT NULL DEFAULT '',
    CONSTRAINT ck_feedback_notification_jobs_attempts CHECK (attempt_count >= 0 AND attempt_count <= 5),
    CONSTRAINT ck_feedback_notification_jobs_status CHECK (job_status IN ('pending', 'processing', 'retry', 'succeeded', 'skipped', 'failed'))
);

CREATE UNIQUE INDEX ux_feedback_notification_jobs_business_id
    ON feedback_notification_jobs (business_id);
CREATE UNIQUE INDEX ux_feedback_notification_jobs_feedback_business_id
    ON feedback_notification_jobs (feedback_business_id);
CREATE INDEX ix_feedback_notification_jobs_ready
    ON feedback_notification_jobs (job_status, next_attempt_at, created_at);

COMMENT ON TABLE feedback_notification_jobs IS '反馈提交后的持久化通知任务，通过后台 worker 查找接收用户并创建站内通知。';
COMMENT ON COLUMN feedback_notification_jobs.id IS '数据库内部自增主键，仅用于数据库内部操作，不返回 API，也不用于表间关联。';
COMMENT ON COLUMN feedback_notification_jobs.business_id IS '服务端生成的任务业务 UUID，每条反馈通知任务唯一。';
COMMENT ON COLUMN feedback_notification_jobs.created_at IS '任务创建时间，带时区，由数据库默认写入。';
COMMENT ON COLUMN feedback_notification_jobs.updated_at IS '任务状态最近更新时间，带时区，由任务领取和完成操作维护。';
COMMENT ON COLUMN feedback_notification_jobs.feedback_business_id IS '对应 feedbacks.business_id 的反馈业务 UUID，建立唯一索引但不创建数据库外键。';
COMMENT ON COLUMN feedback_notification_jobs.job_status IS '任务状态：pending、processing、retry、succeeded、skipped 或 failed。';
COMMENT ON COLUMN feedback_notification_jobs.attempt_count IS '后台实际领取处理次数，最多五次，进程崩溃后由过期处理中任务恢复。';
COMMENT ON COLUMN feedback_notification_jobs.next_attempt_at IS '任务下次可领取时间，带时区，用于暂时性错误的退避重试。';
COMMENT ON COLUMN feedback_notification_jobs.last_attempt_at IS '最近一次领取任务的时间，带时区；尚未领取时为空。';
COMMENT ON COLUMN feedback_notification_jobs.completed_at IS '任务进入成功、跳过或最终失败状态的时间，带时区；等待重试时为空。';
COMMENT ON COLUMN feedback_notification_jobs.last_error_code IS '最近一次安全错误分类，不保存手机号、反馈正文、图片 key 或底层错误原文。';

INSERT INTO app_configs (business_id, config_key, config_value, description, is_public)
VALUES
    ('d7b2f77a-ff2b-4d44-9db5-8be001013001', 'feedback.notify_admin', 'false', '是否在后台异步通知指定的已有管理员账户接收新反馈。', false),
    ('d7b2f77a-ff2b-4d44-9db5-8be001013002', 'feedback.admin_phone', '', '新反馈站内通知的专用接收用户手机号，必须对应已注册账户。', false),
    ('d7b2f77a-ff2b-4d44-9db5-8be001013003', 'http.public_base_url', '', '用于生成反馈公共图片绝对链接的 HTTPS 基础地址，可包含路径前缀，不包含查询参数、片段或凭据。', false)
ON CONFLICT (config_key) DO NOTHING;

COMMIT;
