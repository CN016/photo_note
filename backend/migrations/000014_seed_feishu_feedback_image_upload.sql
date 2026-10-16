BEGIN;

INSERT INTO app_configs (business_id, config_key, config_value, description, is_public)
VALUES
    (gen_random_uuid(), 'feedback.feishu_app_id', '', '用于把反馈图片上传到飞书并生成原生图片节点的飞书自建应用 App ID。', false),
    (gen_random_uuid(), 'feedback.feishu_app_secret', '', '用于把反馈图片上传到飞书的自建应用密钥，只供后台任务读取，不得开放给客户端。', false)
ON CONFLICT (config_key) DO NOTHING;

COMMIT;
