BEGIN;

-- 旧数据全部留在个人域，不根据手机号、名称或未来成员关系自动搬迁。
DO $migration$
DECLARE table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'app_configs','users','tags','inspirations','feedbacks','ledger_records',
        'schedule_items','schedule_exceptions','schedule_bookings','schedule_shares',
        'notifications','notice_config','notice_deliveries','error_logs','feedback_notification_jobs',
        'plans','plan_reminders','projects','project_inspiration_usages','project_attachments'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ADD COLUMN orgid bigint NOT NULL DEFAULT -1', table_name);
        EXECUTE format('COMMENT ON COLUMN %I.orgid IS %L', table_name,
            '独立租户业务编号；-1 表示个人数据或本表明确的系统数据域，正数关联 organizations.orgid，不使用内部自增主键且不创建外键。');
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (orgid = -1 OR orgid BETWEEN 1 AND 9007199254740991)', table_name, 'ck_' || table_name || '_orgid');
        EXECUTE format('CREATE INDEX %I ON %I (orgid, business_id)', 'ix_' || table_name || '_org_business', table_name);
    END LOOP;
    FOREACH table_name IN ARRAY ARRAY['app_configs','users','feedbacks','feedback_notification_jobs'] LOOP
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (orgid = -1)', table_name, 'ck_' || table_name || '_system_org');
    END LOOP;
END
$migration$;

-- 租户业务编号使用独立序列；上限保证各前端平台按 JSON number 往返不会丢失整数精度。
CREATE SEQUENCE organization_orgid_seq AS bigint MINVALUE 1 MAXVALUE 9007199254740991 START WITH 1 NO CYCLE;
COMMENT ON SEQUENCE organization_orgid_seq IS '组织独立业务编号序列，与 organizations.id 内部主键分开，范围限制为正的 JavaScript 安全整数。';
CREATE TABLE organizations (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    business_id uuid NOT NULL,
    orgid bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    name varchar(120) NOT NULL,
    organization_status varchar(16) NOT NULL DEFAULT 'active',
    CONSTRAINT ck_organizations_orgid CHECK (orgid BETWEEN 1 AND 9007199254740991),
    CONSTRAINT ck_organizations_name CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    CONSTRAINT ck_organizations_status CHECK (organization_status IN ('active','disabled'))
);
CREATE UNIQUE INDEX ux_organizations_business_id ON organizations (business_id);
CREATE UNIQUE INDEX ux_organizations_orgid ON organizations (orgid);
CREATE INDEX ix_organizations_org_status_created ON organizations (orgid, organization_status, created_at DESC);

CREATE TABLE organization_members (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    business_id uuid NOT NULL,
    orgid bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_id uuid NOT NULL,
    member_role varchar(16) NOT NULL DEFAULT 'member',
    member_status varchar(16) NOT NULL DEFAULT 'active',
    removed_at timestamptz,
    CONSTRAINT ck_organization_members_orgid CHECK (orgid BETWEEN 1 AND 9007199254740991),
    CONSTRAINT ck_organization_members_role CHECK (member_role IN ('admin','member')),
    CONSTRAINT ck_organization_members_status CHECK (member_status IN ('active','removed'))
);
CREATE UNIQUE INDEX ux_organization_members_business_id ON organization_members (business_id);
CREATE UNIQUE INDEX ux_organization_members_org_user ON organization_members (orgid, user_id);
CREATE INDEX ix_organization_members_user_status_org ON organization_members (user_id, member_status, orgid);
CREATE INDEX ix_organization_members_org_status_role ON organization_members (orgid, member_status, member_role);

CREATE TABLE file_ownerships (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    business_id uuid NOT NULL,
    orgid bigint NOT NULL DEFAULT -1,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    storekey text NOT NULL,
    user_id uuid,
    CONSTRAINT ck_file_ownerships_orgid CHECK (orgid = -1 OR orgid BETWEEN 1 AND 9007199254740991),
    CONSTRAINT ck_file_ownerships_private CHECK (storekey ~ '^(local|oss)_[0-9]{8}_[0-9a-f-]{36}\.[a-zA-Z0-9]+$')
);
CREATE UNIQUE INDEX ux_file_ownerships_business_id ON file_ownerships (business_id);
CREATE UNIQUE INDEX ux_file_ownerships_storekey ON file_ownerships (storekey);
CREATE INDEX ix_file_ownerships_org_user_key ON file_ownerships (orgid, user_id, storekey);

COMMENT ON TABLE organizations IS '组织租户资料表，租户业务编号由独立序列分配，不与数据库内部主键混用。';
COMMENT ON COLUMN organizations.id IS '数据库内部整数自增主键，不返回客户端且不用于组织关系。';
COMMENT ON COLUMN organizations.business_id IS '组织对外业务 UUID，服务端生成且全局唯一。';
COMMENT ON COLUMN organizations.orgid IS '组织独立正整数业务编号，由 organization_orgid_seq 分配；最大为 JavaScript 安全整数，不是内部 id。';
COMMENT ON COLUMN organizations.created_at IS '组织创建时间，带时区并统一按 UTC 保存。';
COMMENT ON COLUMN organizations.updated_at IS '组织资料最近修改时间，带时区。';
COMMENT ON COLUMN organizations.name IS '组织显示名称，去除首尾空白后为 1 至 120 个字符。';
COMMENT ON COLUMN organizations.organization_status IS '组织状态：active 为可用，disabled 为停用；停用不删除资源。';

COMMENT ON TABLE organization_members IS '全局账号与组织的成员关系表；允许加入多个组织，成员移除后资源仍留在原组织。';
COMMENT ON COLUMN organization_members.id IS '数据库内部整数自增主键，不返回客户端且不用于关联。';
COMMENT ON COLUMN organization_members.business_id IS '成员关系业务 UUID，恢复已移除成员时沿用原记录。';
COMMENT ON COLUMN organization_members.orgid IS '所属 organizations.orgid 独立业务编号，必须为正数，由服务层验证存在性，不创建外键。';
COMMENT ON COLUMN organization_members.created_at IS '首次建立成员关系的时间，带时区。';
COMMENT ON COLUMN organization_members.updated_at IS '成员角色或状态最近修改时间，带时区。';
COMMENT ON COLUMN organization_members.user_id IS '全局 users.business_id；账号保持个人版，成员资格不改写账号归属。';
COMMENT ON COLUMN organization_members.member_role IS '组织角色：admin 为组织管理员，member 为普通成员；不授予平台管理员权限。';
COMMENT ON COLUMN organization_members.member_status IS '成员状态：active 为有效，removed 为已移除；每次请求重新校验。';
COMMENT ON COLUMN organization_members.removed_at IS '最近移除成员的时间，带时区；有效成员为空。';

COMMENT ON TABLE file_ownerships IS '私有文件最小授权登记，只维护不可变的组织和创建者，不提供文件元数据列表或删除管理。';
COMMENT ON COLUMN file_ownerships.id IS '数据库内部整数自增主键，不返回客户端且不用于关联。';
COMMENT ON COLUMN file_ownerships.business_id IS '文件授权记录业务 UUID，服务端生成且全局唯一。';
COMMENT ON COLUMN file_ownerships.orgid IS '文件创建时的租户业务编号；-1 为个人版，不能被后续请求重新认领或迁移。';
COMMENT ON COLUMN file_ownerships.created_at IS '授权登记创建时间，带时区；历史记录以迁移执行时间记入。';
COMMENT ON COLUMN file_ownerships.updated_at IS '授权登记最近修改时间，带时区；普通请求不更新所有者。';
COMMENT ON COLUMN file_ownerships.storekey IS '全局唯一的私有 local 或 oss 文件定位键，不包含公共反馈文件键。';
COMMENT ON COLUMN file_ownerships.user_id IS '文件创建者的 users.business_id；历史多用户冲突时为空并默认拒绝任何普通用户访问。';

-- 保留内部主键、全局业务 UUID、手机号、公开令牌和预约查询码唯一性；业务索引包含组织维度。
DROP INDEX ix_tags_user_id;
CREATE INDEX ix_tags_user_id ON tags (orgid, user_id);
DROP INDEX ux_tags_user_content;
CREATE UNIQUE INDEX ux_tags_user_content ON tags (orgid, user_id, content);
DROP INDEX ix_inspirations_user_id;
CREATE INDEX ix_inspirations_user_id ON inspirations (orgid, user_id, created_at DESC);
DROP INDEX ix_inspirations_file_hash;
CREATE INDEX ix_inspirations_file_hash ON inspirations (orgid, user_id, file_hash) WHERE file_hash IS NOT NULL;
-- tag_ids 与 search_grams 的 GIN 保留，由组织 BTree 组合过滤，不引入 btree_gin 扩展。
DROP INDEX ix_feedbacks_user_id;
CREATE INDEX ix_feedbacks_user_id ON feedbacks (orgid, user_id);
DROP INDEX ix_feedbacks_status_created;
CREATE INDEX ix_feedbacks_status_created ON feedbacks (orgid, feedback_status, created_at DESC);
DROP INDEX ix_ledger_records_user_date;
CREATE INDEX ix_ledger_records_user_date ON ledger_records (orgid, user_id, record_date DESC);
DROP INDEX ix_schedule_items_user_start;
CREATE INDEX ix_schedule_items_user_start ON schedule_items (orgid, user_id, start_at, end_at);
DROP INDEX ix_schedule_items_user_kind;
CREATE INDEX ix_schedule_items_user_kind ON schedule_items (orgid, user_id, item_kind);
DROP INDEX ix_schedule_exceptions_user_schedule;
CREATE INDEX ix_schedule_exceptions_user_schedule ON schedule_exceptions (orgid, user_id, schedule_business_id);
ALTER TABLE schedule_exceptions DROP CONSTRAINT uq_schedule_exceptions_occurrence;
ALTER TABLE schedule_exceptions ADD CONSTRAINT uq_schedule_exceptions_occurrence UNIQUE (orgid, schedule_business_id, occurrence_date);
DROP INDEX ix_schedule_bookings_user_status;
CREATE INDEX ix_schedule_bookings_user_status ON schedule_bookings (orgid, user_id, booking_status, created_at DESC);
DROP INDEX ix_schedule_bookings_slot;
CREATE INDEX ix_schedule_bookings_slot ON schedule_bookings (orgid, schedule_business_id, occurrence_start, booking_status, expires_at);
DROP INDEX ux_schedule_shares_user_id;
CREATE UNIQUE INDEX ux_schedule_shares_user_id ON schedule_shares (orgid, user_id);
DROP INDEX ux_notifications_user_event_key;
CREATE UNIQUE INDEX ux_notifications_user_event_key ON notifications (orgid, user_id, event_key);
DROP INDEX ix_notifications_user_created;
CREATE INDEX ix_notifications_user_created ON notifications (orgid, user_id, created_at DESC);
DROP INDEX ix_notifications_user_unread;
CREATE INDEX ix_notifications_user_unread ON notifications (orgid, user_id, read_at, created_at DESC);
DROP INDEX ix_notice_config_user;
CREATE INDEX ix_notice_config_user ON notice_config (orgid, user_id, created_at DESC) WHERE deleted_at IS NULL;
DROP INDEX ix_notice_config_user_enabled;
CREATE INDEX ix_notice_config_user_enabled ON notice_config (orgid, user_id, enabled) WHERE deleted_at IS NULL;
DROP INDEX ix_notice_deliveries_ready;
CREATE INDEX ix_notice_deliveries_ready ON notice_deliveries (next_attempt_at, created_at, orgid) WHERE delivery_status IN ('pending','retry');
DROP INDEX ix_notice_deliveries_user_created;
CREATE INDEX ix_notice_deliveries_user_created ON notice_deliveries (orgid, user_id, created_at DESC);
DROP INDEX ix_notice_deliveries_notification;
CREATE INDEX ix_notice_deliveries_notification ON notice_deliveries (orgid, user_id, notification_business_id);
DROP INDEX ix_notice_deliveries_config;
CREATE INDEX ix_notice_deliveries_config ON notice_deliveries (orgid, user_id, notice_config_business_id);
DROP INDEX ix_feedback_notification_jobs_ready;
CREATE INDEX ix_feedback_notification_jobs_ready ON feedback_notification_jobs (job_status, next_attempt_at, created_at, orgid);
DROP INDEX ux_feedback_notification_jobs_feedback_business_id;
CREATE UNIQUE INDEX ux_feedback_notification_jobs_feedback_business_id ON feedback_notification_jobs (orgid, feedback_business_id);
DROP INDEX ix_error_logs_dedup_last_seen;
CREATE INDEX ix_error_logs_dedup_last_seen ON error_logs (orgid, dedup_key, last_seen_at DESC);
DROP INDEX ix_error_logs_created_at;
CREATE INDEX ix_error_logs_created_at ON error_logs (orgid, created_at DESC);
DROP INDEX ix_error_logs_code_created;
CREATE INDEX ix_error_logs_code_created ON error_logs (orgid, error_code, created_at DESC);
DROP INDEX ix_plans_user_status_start;
CREATE INDEX ix_plans_user_status_start ON plans (orgid, user_id, plan_status, starts_at) WHERE deleted_at IS NULL;
DROP INDEX ix_plan_reminders_user_plan;
CREATE INDEX ix_plan_reminders_user_plan ON plan_reminders (orgid, user_id, plan_business_id);
DROP INDEX ix_plan_reminders_ready;
CREATE INDEX ix_plan_reminders_ready ON plan_reminders (remind_at, created_at, orgid) WHERE reminder_status = 'pending';
DROP INDEX ix_plan_reminders_configured;
CREATE INDEX ix_plan_reminders_configured ON plan_reminders (orgid, plan_business_id, lead_minutes) WHERE is_configured = true;
DROP INDEX ix_projects_user_updated;
CREATE INDEX ix_projects_user_updated ON projects (orgid, user_id, updated_at DESC) WHERE deleted_at IS NULL;
DROP INDEX ux_project_inspiration_usages_pair;
CREATE UNIQUE INDEX ux_project_inspiration_usages_pair ON project_inspiration_usages (orgid, user_id, project_business_id, inspiration_business_id);
DROP INDEX ix_project_inspiration_usages_inspiration;
CREATE INDEX ix_project_inspiration_usages_inspiration ON project_inspiration_usages (orgid, user_id, inspiration_business_id);
DROP INDEX ux_project_attachments_project_storekey;
CREATE UNIQUE INDEX ux_project_attachments_project_storekey ON project_attachments (orgid, user_id, project_business_id, storekey);
DROP INDEX ix_project_attachments_user_project;
CREATE INDEX ix_project_attachments_user_project ON project_attachments (orgid, user_id, project_business_id);

-- 仅从持久化引用推断旧文件创建者；同 key 多账号引用时隔离，不按当前请求账号认领。
WITH referenced_files AS (
    SELECT avatar_key AS storekey, business_id AS user_id FROM users WHERE avatar_key IS NOT NULL
    UNION ALL SELECT image_key, user_id FROM inspirations
    UNION ALL SELECT data ->> 'source_image_key', user_id FROM inspirations
    UNION ALL SELECT cover_image_key, user_id FROM plans
    UNION ALL SELECT cover_image_key, user_id FROM projects
    UNION ALL SELECT storekey, user_id FROM project_attachments
), grouped AS (
    SELECT storekey, CASE WHEN count(DISTINCT user_id) = 1 THEN min(user_id::text)::uuid ELSE NULL END AS user_id
    FROM referenced_files
    WHERE storekey ~ '^(local|oss)_[0-9]{8}_[0-9a-f-]{36}\.[a-zA-Z0-9]+$'
    GROUP BY storekey
)
INSERT INTO file_ownerships (business_id, orgid, storekey, user_id)
SELECT gen_random_uuid(), -1, storekey, user_id FROM grouped;

COMMIT;
