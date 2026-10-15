BEGIN;

-- 历史记录使用 0 作为初始代次；仅新的异步图片更新任务会递增该字段。
ALTER TABLE inspirations
    ADD COLUMN IF NOT EXISTS async_version bigint NOT NULL DEFAULT 0;

COMMENT ON COLUMN inspirations.async_version IS '灵感图异步图片处理代次；新增记录从 0 开始，仅提交新的异步图片更新任务时递增，后台结果仅在代次和图片 key 均未变化时写回。';

COMMIT;
