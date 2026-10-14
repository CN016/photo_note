BEGIN;

-- 已有配置保持默认私有；只有显式设置 is_public=true 的配置值可由公开接口读取。
ALTER TABLE app_configs
    ADD COLUMN IF NOT EXISTS is_public boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN app_configs.is_public IS '是否允许未登录客户端通过公开配置接口读取配置值；默认不开放，只有明确标记为 true 时才返回配置值。';

COMMIT;
