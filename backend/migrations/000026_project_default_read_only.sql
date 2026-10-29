-- 旧策划默认继续以编辑模式打开；该字段不是授权控制。
ALTER TABLE projects ADD COLUMN default_read_only boolean NOT NULL DEFAULT false;
COMMENT ON COLUMN projects.default_read_only IS '默认以阅读模式打开；用户可切换编辑，不作为权限控制';
