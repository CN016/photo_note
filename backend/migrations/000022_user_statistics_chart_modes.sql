BEGIN;

ALTER TABLE users ADD COLUMN IF NOT EXISTS extra jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN users.extra IS '当前用户的私有扩展展示偏好 JSON 对象；statistics_chart_modes 保存拍摄统计各卡片的图表模式，不包含权限或秘密。';

COMMIT;
