BEGIN;

ALTER TABLE inspirations ADD COLUMN IF NOT EXISTS auto_tags jsonb;

COMMENT ON COLUMN inspirations.auto_tags IS '自动图片标签结果 JSON，包含模型版本、标签名称/分数/来源及完成时间；NULL 表示尚未完成自动标签，用户手动标签仍保存在 tag_ids。';

CREATE INDEX IF NOT EXISTS ix_inspirations_auto_tags_pending
    ON inspirations(id)
    WHERE auto_tags IS NULL;

COMMENT ON INDEX ix_inspirations_auto_tags_pending IS '每日系统 worker 扫描尚未完成自动标签图片的部分索引；仅系统扫描使用。';

CREATE INDEX IF NOT EXISTS ix_inspirations_auto_tags_gin
    ON inspirations USING GIN (auto_tags);

UPDATE app_configs
SET config_value = (
        config_value::jsonb || jsonb_build_object(
            'algorithm', 'yolox-nano-v1/color-v1/siglip2-base-fp16-tags-v1',
            'tag_model_path', '/opt/photo-nini-ai/models/siglip2_vision_fp16.onnx',
            'tag_embeddings_path', '/opt/photo-nini-ai/models/siglip2_tag_embeddings.npz',
            'tag_workers', 10,
            'tag_top_k', 18,
            'tag_min_score', 0.045
        )
    )::text,
    updated_at = CURRENT_TIMESTAMP
WHERE orgid = -1
  AND config_key = 'inspirations.processing';

COMMIT;
