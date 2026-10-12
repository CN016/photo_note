BEGIN;

-- 服务监听模式和地址由统一配置服务从配置表读取，环境变量只保留数据库与 Redis 连接设置。
INSERT INTO app_configs (business_id, config_key, config_value, description)
VALUES
    ('a7d58d52-8a1d-4a7b-8da2-b5d3b2110001', 'app.mode', 'release', 'Gin 服务运行模式，可选 release、debug 或 test。'),
    ('a7d58d52-8a1d-4a7b-8da2-b5d3b2110002', 'http.listen_addr', ':8080', 'HTTP 服务监听地址；容器内应监听全部网卡。'),
    ('a7d58d52-8a1d-4a7b-8da2-b5d3b2110003', 'http.max_json_body_bytes', '10485760', 'HTTP JSON 请求体最大字节数，默认 10 MiB。'),
    ('a7d58d52-8a1d-4a7b-8da2-b5d3b2110004', 'http.read_header_timeout', '5s', 'HTTP 请求头读取超时时长，使用 Go duration 格式。'),
    ('a7d58d52-8a1d-4a7b-8da2-b5d3b2110005', 'http.read_timeout', '15s', 'HTTP 请求读取超时时长，使用 Go duration 格式。'),
    ('a7d58d52-8a1d-4a7b-8da2-b5d3b2110006', 'http.write_timeout', '30s', 'HTTP 响应写入超时时长，使用 Go duration 格式。'),
    ('a7d58d52-8a1d-4a7b-8da2-b5d3b2110007', 'http.idle_timeout', '60s', 'HTTP keep-alive 空闲连接超时时长，使用 Go duration 格式。'),
    ('a7d58d52-8a1d-4a7b-8da2-b5d3b2110008', 'auth.access_token_ttl', '2h', 'Access Token 有效期，使用 Go duration 格式。'),
    ('a7d58d52-8a1d-4a7b-8da2-b5d3b2110009', 'auth.refresh_token_ttl', '720h', 'Refresh Token 有效期，使用 Go duration 格式。')
ON CONFLICT (config_key) DO NOTHING;

COMMIT;
