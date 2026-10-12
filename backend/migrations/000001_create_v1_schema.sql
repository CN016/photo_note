BEGIN;

CREATE TABLE app_configs (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    business_id uuid NOT NULL,
    config_key text NOT NULL,
    config_value text NOT NULL,
    description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX ux_app_configs_business_id
    ON app_configs (business_id);
CREATE UNIQUE INDEX ux_app_configs_config_key
    ON app_configs (config_key);

CREATE TABLE users (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    business_id uuid NOT NULL,
    nickname varchar(64) NOT NULL,
    phone varchar(16) NOT NULL,
    password_hash text NOT NULL,
    avatar_key text,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ck_users_phone_e164
        CHECK (phone ~ '^\+[1-9][0-9]{1,14}$')
);

CREATE UNIQUE INDEX ux_users_business_id
    ON users (business_id);
CREATE UNIQUE INDEX ux_users_phone
    ON users (phone);

CREATE TABLE tags (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    business_id uuid NOT NULL,
    user_id uuid NOT NULL,
    content varchar(64) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX ux_tags_business_id
    ON tags (business_id);
CREATE INDEX ix_tags_user_id
    ON tags (user_id);

CREATE TABLE inspirations (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    business_id uuid NOT NULL,
    user_id uuid NOT NULL,
    tag_ids uuid[] NOT NULL DEFAULT ARRAY[]::uuid[],
    image_key text NOT NULL,
    data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX ux_inspirations_business_id
    ON inspirations (business_id);
CREATE INDEX ix_inspirations_user_id
    ON inspirations (user_id);
CREATE INDEX ix_inspirations_tag_ids_gin
    ON inspirations USING GIN (tag_ids);

COMMENT ON TABLE app_configs IS '应用业务配置表，仅保存非秘密配置项。';
COMMENT ON COLUMN app_configs.id IS '数据库内部自增主键，仅供数据库内部使用，不返回客户端，也不用于表间关联。';
COMMENT ON COLUMN app_configs.business_id IS '服务端生成并对外使用的业务 UUID，每条配置记录唯一。';
COMMENT ON COLUMN app_configs.config_key IS '配置项唯一键，用于服务端查找配置。';
COMMENT ON COLUMN app_configs.config_value IS '配置项文本值，不得存放密钥、密码等秘密信息。';
COMMENT ON COLUMN app_configs.description IS '配置项用途说明。';
COMMENT ON COLUMN app_configs.created_at IS '记录创建时间，带时区，默认使用数据库当前时间。';
COMMENT ON COLUMN app_configs.updated_at IS '记录最近修改时间，带时区，默认使用数据库当前时间。';

COMMENT ON TABLE users IS '用户账户表，手机号为规范化后的 E.164 格式。';
COMMENT ON COLUMN users.id IS '数据库内部自增主键，仅供数据库内部使用，不返回客户端，也不用于表间关联。';
COMMENT ON COLUMN users.business_id IS '服务端生成并对外使用的用户业务 UUID，记录唯一。';
COMMENT ON COLUMN users.nickname IS '用户昵称，最多 64 个字符。';
COMMENT ON COLUMN users.phone IS '用户手机号，按 E.164 格式规范化并唯一保存。';
COMMENT ON COLUMN users.password_hash IS '用户密码的 Argon2id 编码哈希，不得保存明文密码。';
COMMENT ON COLUMN users.avatar_key IS '头像的 OSS 对象键或服务管理的文件标识，可为空。';
COMMENT ON COLUMN users.created_at IS '记录创建时间，带时区，默认使用数据库当前时间。';
COMMENT ON COLUMN users.updated_at IS '记录最近修改时间，带时区，默认使用数据库当前时间。';

COMMENT ON TABLE tags IS '用户标签表，标签归属通过用户业务 UUID 表示并由服务层校验。';
COMMENT ON COLUMN tags.id IS '数据库内部自增主键，仅供数据库内部使用，不返回客户端，也不用于表间关联。';
COMMENT ON COLUMN tags.business_id IS '服务端生成并对外使用的标签业务 UUID，记录唯一。';
COMMENT ON COLUMN tags.user_id IS '所属用户的 users.business_id；不设置外键，存在性和归属由服务层校验。';
COMMENT ON COLUMN tags.content IS '标签文本内容，最多 64 个字符。';
COMMENT ON COLUMN tags.created_at IS '记录创建时间，带时区，默认使用数据库当前时间。';
COMMENT ON COLUMN tags.updated_at IS '记录最近修改时间，带时区，默认使用数据库当前时间。';

COMMENT ON TABLE inspirations IS '灵感图表，保存图片引用、编辑数据及关联标签业务 UUID。';
COMMENT ON COLUMN inspirations.id IS '数据库内部自增主键，仅供数据库内部使用，不返回客户端，也不用于表间关联。';
COMMENT ON COLUMN inspirations.business_id IS '服务端生成并对外使用的灵感图业务 UUID，记录唯一。';
COMMENT ON COLUMN inspirations.user_id IS '所属用户的 users.business_id；不设置外键，存在性和归属由服务层校验。';
COMMENT ON COLUMN inspirations.tag_ids IS '标签业务 UUID 数组，元素对应 tags.business_id；标签存在性和用户归属由服务层校验。';
COMMENT ON COLUMN inspirations.image_key IS '图片的 OSS 对象键或服务管理的文件路径标识。';
COMMENT ON COLUMN inspirations.data IS '版本化的编辑器 JSONB 对象，必须包含正整数 schema_version。';
COMMENT ON COLUMN inspirations.created_at IS '记录创建时间，带时区，默认使用数据库当前时间。';
COMMENT ON COLUMN inspirations.updated_at IS '记录最近修改时间，带时区，默认使用数据库当前时间。';

COMMIT;
