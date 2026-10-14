BEGIN;

-- 历史灵感图没有文件名来源，迁移后以空字符串兼容；文件哈希留空直到图片通过新接口重新提交。
ALTER TABLE inspirations
    ADD COLUMN IF NOT EXISTS file_name text NOT NULL DEFAULT '';

ALTER TABLE inspirations
    ADD COLUMN IF NOT EXISTS file_hash varchar(64);

-- 仅索引已计算的哈希，支持按文件内容去重或查找且避免索引无哈希的历史记录。
CREATE INDEX IF NOT EXISTS ix_inspirations_file_hash
    ON inspirations (file_hash)
    WHERE file_hash IS NOT NULL;

COMMENT ON COLUMN inspirations.file_name IS '原始图片文件名，仅用于展示，不得用作本地存储路径；最多保存 255 个字符。';
COMMENT ON COLUMN inspirations.file_hash IS '图片内容的 SHA-256 小写十六进制摘要，固定 64 个字符；storekey 指向的文件由服务端读取后计算，非 storekey 或历史记录可为空。';

COMMIT;
