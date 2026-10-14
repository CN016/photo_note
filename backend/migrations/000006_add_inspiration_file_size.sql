BEGIN;

-- 历史图片的实际字节大小无法安全推断，新增字段允许为空并由后续创建或更新请求补齐。
ALTER TABLE inspirations
    ADD COLUMN IF NOT EXISTS file_size bigint;

COMMENT ON COLUMN inspirations.file_size IS '图片文件最终存储大小，单位为字节；storekey 图片由服务端压缩或确认后记录，非 storekey 图片及历史记录可为空。';

COMMIT;
