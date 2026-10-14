BEGIN;

-- 文件存储路径与单文件上限由统一配置服务加载，不创建文件元数据表。
INSERT INTO app_configs (business_id, config_key, config_value, description)
VALUES
    ('a7d58d52-8a1d-4a7b-8da2-b5d3b2110010', 'files.local_root', '/var/lib/photo-nini/uploads', '本地文件存储根目录；容器部署时应挂载持久化卷。'),
    ('a7d58d52-8a1d-4a7b-8da2-b5d3b2110011', 'files.max_upload_bytes', '52428800', '单个上传文件允许的最大字节数，默认 50 MiB。'),
    ('a7d58d52-8a1d-4a7b-8da2-b5d3b2110012', 'files.active_store_type', 'local', '当前新上传文件使用的存储类型，支持 local 或 oss；类型变更后新写入立即按新值路由。')
ON CONFLICT (config_key) DO NOTHING;

-- 较大的本地文件需要更长读写时间；仅将未自定义的原始默认值延长到五分钟。
UPDATE app_configs
SET config_value = '5m',
    description = 'HTTP 请求读取超时时长，使用 Go duration 格式；应覆盖最大文件上传时间。',
    updated_at = CURRENT_TIMESTAMP
WHERE config_key = 'http.read_timeout'
  AND config_value = '15s';

UPDATE app_configs
SET config_value = '5m',
    description = 'HTTP 响应写入超时时长，使用 Go duration 格式；应覆盖大文件下载时间。',
    updated_at = CURRENT_TIMESTAMP
WHERE config_key = 'http.write_timeout'
  AND config_value = '30s';

COMMIT;
