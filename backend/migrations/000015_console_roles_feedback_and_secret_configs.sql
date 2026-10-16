BEGIN;

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS is_admin boolean NOT NULL DEFAULT false;
COMMENT ON COLUMN users.is_admin IS '是否允许进入管理工作台；默认 false，仅由数据库运维或受信管理接口修改，用户注册和个人资料接口不得授予。';

ALTER TABLE app_configs
    ADD COLUMN IF NOT EXISTS is_secret boolean NOT NULL DEFAULT false;
COMMENT ON COLUMN app_configs.is_secret IS '配置值是否为秘密；秘密值不得通过公开接口返回，管理接口只能显示是否已配置。';
UPDATE app_configs
SET is_secret = true, is_public = false, updated_at = CURRENT_TIMESTAMP
WHERE config_key IN ('feedback.feishu_app_secret', 'feedback.admin_phone', 'error_logs.admin_phone');

ALTER TABLE feedbacks
    ADD COLUMN IF NOT EXISTS feedback_status varchar(16) NOT NULL DEFAULT 'new';
ALTER TABLE feedbacks
    DROP CONSTRAINT IF EXISTS ck_feedbacks_feedback_status;
ALTER TABLE feedbacks
    ADD CONSTRAINT ck_feedbacks_feedback_status CHECK (feedback_status IN ('new', 'processed'));
CREATE INDEX IF NOT EXISTS ix_feedbacks_status_created
    ON feedbacks (feedback_status, created_at DESC);
COMMENT ON COLUMN feedbacks.feedback_status IS '反馈处理状态，仅允许 new（待处理）或 processed（已处理），新反馈默认为 new。';

COMMIT;
