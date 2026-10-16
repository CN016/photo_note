-- 首次部署时手动替换占位手机号，并确认它对应已经注册的 Photo Nini 账号。
-- 此文件是运维模板，不作为版本化迁移执行；不要提交真实手机号或在生产前预设管理账号。
BEGIN;

UPDATE users
SET is_admin = true, updated_at = CURRENT_TIMESTAMP
WHERE phone = 'REPLACE_WITH_EXISTING_11_DIGIT_PHONE';

-- 执行后必须确认 UPDATE 影响且仅影响一行；否则回滚并修正目标账号。
-- COMMIT;
-- ROLLBACK;
