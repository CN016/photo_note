-- 仅用于本次多租户浏览器验收的合成测试库，不包含凭据，不得用于项目或生产数据库。
BEGIN;
DO $$
BEGIN
    IF current_database() <> 'tenant_test' THEN
        RAISE EXCEPTION '此浏览器测试夹具仅允许在tenant_test临时数据库执行';
    END IF;
END;
$$;

-- 测试服务与现有8089服务隔离；所有业务运行配置仍经app_configs读取。
UPDATE app_configs SET config_value = CASE config_key
    WHEN 'app.mode' THEN 'test'
    WHEN 'http.listen_addr' THEN '127.0.0.1:18090'
    WHEN 'files.local_root' THEN './dist/tenant-e2e-uploads'
    WHEN 'files.active_store_type' THEN 'local'
    WHEN 'http.public_base_url' THEN 'https://tenant-test.invalid'
    WHEN 'feedback.notify_admin' THEN 'false'
    WHEN 'feedback.feishu_enabled' THEN 'false'
    WHEN 'error_logs.notify_admin' THEN 'false'
    ELSE config_value END,
    updated_at = CURRENT_TIMESTAMP
WHERE orgid = -1 AND config_key IN (
    'app.mode', 'http.listen_addr', 'files.local_root', 'files.active_store_type',
    'http.public_base_url', 'feedback.notify_admin', 'feedback.feishu_enabled', 'error_logs.notify_admin'
);

-- 合成账号通过正常注册流程建立Argon2id哈希，脚本只授予该测试账号平台管理员角色。
-- 在注册前执行此脚本也安全；注册成功后再执行一次即可完成角色初始化。
UPDATE users SET is_admin = true, updated_at = CURRENT_TIMESTAMP
WHERE orgid = -1 AND phone = '19900000022' AND nickname = '租户浏览器验收';
COMMIT;
