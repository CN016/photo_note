BEGIN;

-- 迁移前先验证映射无歧义；异常数据会中止整个事务，不修改任何用户手机号。
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM users WHERE phone !~ '^\+86[0-9]{11}$') THEN
        RAISE EXCEPTION '000008 迁移已取消：users.phone 存在非 +86 加 11 位号码，请人工映射后重试';
    END IF;
    IF EXISTS (
        SELECT substring(phone FROM 4)
        FROM users
        GROUP BY substring(phone FROM 4)
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION '000008 迁移已取消：手机号转为 11 位后存在重复，请人工映射后重试';
    END IF;
END $$;

ALTER TABLE users DROP CONSTRAINT ck_users_phone_e164;
UPDATE users SET phone = substring(phone FROM 4), updated_at = CURRENT_TIMESTAMP;
ALTER TABLE users ALTER COLUMN phone TYPE varchar(11);
ALTER TABLE users ADD CONSTRAINT ck_users_phone_11_digits CHECK (phone ~ '^[0-9]{11}$');

COMMENT ON TABLE users IS '用户账户表，手机号保存为 11 位数字并保持唯一。';
COMMENT ON COLUMN users.phone IS '用户登录手机号，仅保存 11 位数字，数据库唯一索引保证账户唯一。';

CREATE TABLE feedbacks (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    business_id uuid NOT NULL,
    user_id uuid,
    content text NOT NULL DEFAULT '',
    image_keys text[] NOT NULL DEFAULT ARRAY[]::text[],
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX ux_feedbacks_business_id ON feedbacks (business_id);
CREATE INDEX ix_feedbacks_user_id ON feedbacks (user_id);

COMMENT ON TABLE feedbacks IS '用户反馈表，保存可选登录用户、反馈文字和图片 storekey。';
COMMENT ON COLUMN feedbacks.id IS '数据库内部自增主键，仅供数据库内部使用，不返回客户端，也不用于表间关联。';
COMMENT ON COLUMN feedbacks.business_id IS '服务端生成并对外使用的反馈业务 UUID，每条反馈记录唯一。';
COMMENT ON COLUMN feedbacks.user_id IS '提交时令牌对应的 users.business_id；游客反馈为空，不设置外键。';
COMMENT ON COLUMN feedbacks.content IS '反馈文字正文，最多 2000 个 Unicode 字符；允许为空但至少需有正文或图片。';
COMMENT ON COLUMN feedbacks.image_keys IS '反馈图片的文件 storekey 数组，最多保存 5 个服务端验证后的图片键。';
COMMENT ON COLUMN feedbacks.created_at IS '记录创建时间，带时区，默认使用数据库当前时间。';
COMMENT ON COLUMN feedbacks.updated_at IS '记录最近修改时间，带时区，默认使用数据库当前时间。';

INSERT INTO app_configs (business_id, config_key, config_value, description, is_public)
VALUES
    ('d7b2f77a-ff2b-4d44-9db5-8be001070001', 'http.trusted_proxy_cidrs', '127.0.0.1/32,::1/128', '允许服务端信任转发客户端 IP 请求头的代理 CIDR 列表；部署拓扑变化时按实际代理地址调整。', false),
    ('d7b2f77a-ff2b-4d44-9db5-8be001070002', 'feedback.max_text_chars', '2000', '单条反馈正文最多允许的 Unicode 字符数量。', false),
    ('d7b2f77a-ff2b-4d44-9db5-8be001070003', 'feedback.max_images', '5', '单条反馈最多允许上传的图片数量。', false),
    ('d7b2f77a-ff2b-4d44-9db5-8be001070004', 'feedback.rate_limit_per_ip_hour', '5', '每个客户端 IP 在滚动一小时内允许提交的反馈次数。', false)
ON CONFLICT (config_key) DO NOTHING;

COMMIT;
