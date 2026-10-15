BEGIN;

CREATE TABLE schedule_items (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    business_id uuid NOT NULL,
    user_id uuid NOT NULL,
    item_kind varchar(16) NOT NULL,
    title varchar(120) NOT NULL,
    content text NOT NULL DEFAULT '',
    start_at timestamptz NOT NULL,
    end_at timestamptz NOT NULL,
    recurrence varchar(16) NOT NULL DEFAULT 'none',
    recurrence_weekdays integer[] NOT NULL DEFAULT ARRAY[]::integer[],
    recurrence_end date,
    capacity integer NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ck_schedule_items_kind CHECK (item_kind IN ('work', 'availability')),
    CONSTRAINT ck_schedule_items_interval CHECK (start_at < end_at),
    CONSTRAINT ck_schedule_items_recurrence CHECK (recurrence IN ('none', 'daily', 'weekly', 'monthly')),
    CONSTRAINT ck_schedule_items_capacity CHECK (capacity >= 1),
    CONSTRAINT ck_schedule_items_weekdays CHECK (item_kind <> 'availability' OR recurrence <> 'weekly' OR cardinality(recurrence_weekdays) > 0)
);
CREATE UNIQUE INDEX ux_schedule_items_business_id ON schedule_items (business_id);
CREATE INDEX ix_schedule_items_user_start ON schedule_items (user_id, start_at, end_at);
CREATE INDEX ix_schedule_items_user_kind ON schedule_items (user_id, item_kind);

CREATE TABLE schedule_exceptions (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    business_id uuid NOT NULL,
    user_id uuid NOT NULL,
    schedule_business_id uuid NOT NULL,
    occurrence_date date NOT NULL,
    exception_action varchar(16) NOT NULL,
    title varchar(120),
    content text,
    start_at timestamptz,
    end_at timestamptz,
    capacity integer,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ck_schedule_exceptions_action CHECK (exception_action IN ('cancelled', 'override')),
    CONSTRAINT ck_schedule_exceptions_interval CHECK (start_at IS NULL OR end_at IS NULL OR start_at < end_at),
    CONSTRAINT ck_schedule_exceptions_capacity CHECK (capacity IS NULL OR capacity >= 1),
    CONSTRAINT uq_schedule_exceptions_occurrence UNIQUE (schedule_business_id, occurrence_date)
);
CREATE UNIQUE INDEX ux_schedule_exceptions_business_id ON schedule_exceptions (business_id);
CREATE INDEX ix_schedule_exceptions_user_schedule ON schedule_exceptions (user_id, schedule_business_id);

CREATE TABLE schedule_bookings (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    business_id uuid NOT NULL,
    user_id uuid NOT NULL,
    schedule_business_id uuid NOT NULL,
    occurrence_start timestamptz NOT NULL,
    occurrence_end timestamptz NOT NULL,
    guest_name varchar(80) NOT NULL,
    guest_phone varchar(32) NOT NULL,
    note varchar(1000) NOT NULL DEFAULT '',
    booking_status varchar(16) NOT NULL DEFAULT 'pending',
    request_code varchar(12) NOT NULL,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ck_schedule_bookings_interval CHECK (occurrence_start < occurrence_end),
    CONSTRAINT ck_schedule_bookings_status CHECK (booking_status IN ('pending', 'confirmed', 'rejected', 'expired', 'cancelled'))
);
CREATE UNIQUE INDEX ux_schedule_bookings_business_id ON schedule_bookings (business_id);
CREATE UNIQUE INDEX ux_schedule_bookings_request_code ON schedule_bookings (request_code);
CREATE INDEX ix_schedule_bookings_user_status ON schedule_bookings (user_id, booking_status, created_at DESC);
CREATE INDEX ix_schedule_bookings_slot ON schedule_bookings (schedule_business_id, occurrence_start, booking_status, expires_at);

CREATE TABLE schedule_shares (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    business_id uuid NOT NULL,
    user_id uuid NOT NULL,
    token_hash char(64) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX ux_schedule_shares_business_id ON schedule_shares (business_id);
CREATE UNIQUE INDEX ux_schedule_shares_user_id ON schedule_shares (user_id);
CREATE UNIQUE INDEX ux_schedule_shares_token_hash ON schedule_shares (token_hash);

INSERT INTO app_configs (business_id, config_key, config_value, description, is_public)
VALUES (gen_random_uuid(), 'schedule.booking_rate_limit_per_ip_hour', '10', '每个可信客户端 IP 每小时允许提交的公开预约申请数。', false)
ON CONFLICT (config_key) DO NOTHING;

COMMENT ON TABLE schedule_items IS '用户档期主表，保存工作事项、开放预约时段及每日/每周/每月重复规则。';
COMMENT ON COLUMN schedule_items.id IS '数据库内部自增主键，仅供数据库内部使用，不返回客户端，也不用于表间关联。';
COMMENT ON COLUMN schedule_items.business_id IS '档期对外业务 UUID，每条档期唯一，由服务端生成。';
COMMENT ON COLUMN schedule_items.user_id IS '档期所属用户的 users.business_id，由访问令牌确定，不设置数据库外键。';
COMMENT ON COLUMN schedule_items.item_kind IS '档期类型，只允许 work 工作事项或 availability 可预约开放时段。';
COMMENT ON COLUMN schedule_items.title IS '档期标题，最多 120 个 Unicode 字符，公开查询只向访客展示开放时段标题。';
COMMENT ON COLUMN schedule_items.content IS '档期补充内容；工作事项只对拥有者可见，开放时段内容由拥有者主动提供给访客。';
COMMENT ON COLUMN schedule_items.start_at IS '首次发生开始时刻，使用带时区时间；重复规则按 Asia/Shanghai 本地墙上时间展开。';
COMMENT ON COLUMN schedule_items.end_at IS '首次发生结束时刻，使用带时区时间，必须晚于开始时刻。';
COMMENT ON COLUMN schedule_items.recurrence IS '重复规则，只允许 none 单次、daily 每日、weekly 指定星期或 monthly 同一日。';
COMMENT ON COLUMN schedule_items.recurrence_weekdays IS '每周重复时使用的星期编号数组，1 至 7 代表周一至周日；其他重复规则为空数组。';
COMMENT ON COLUMN schedule_items.recurrence_end IS '重复规则最后一个有效本地日历日期，可为空表示未指定结束日期；每月缺少对应日期时跳过。';
COMMENT ON COLUMN schedule_items.capacity IS '开放时段可接受的预约人数上限，工作事项固定使用默认值 1。';
COMMENT ON COLUMN schedule_items.created_at IS '档期创建时间，使用带时区时间并默认为数据库当前时间。';
COMMENT ON COLUMN schedule_items.updated_at IS '档期最近修改时间，使用带时区时间并由服务端维护。';

COMMENT ON TABLE schedule_exceptions IS '重复档期的单次例外表，按发生日期保存取消或覆盖，不改变重复系列其他日期。';
COMMENT ON COLUMN schedule_exceptions.id IS '数据库内部自增主键，仅供数据库内部使用。';
COMMENT ON COLUMN schedule_exceptions.business_id IS '单次例外对外业务 UUID，每条记录唯一。';
COMMENT ON COLUMN schedule_exceptions.user_id IS '例外所属用户的 users.business_id，由服务校验归属，不设置数据库外键。';
COMMENT ON COLUMN schedule_exceptions.schedule_business_id IS '被覆盖档期的 schedule_items.business_id，不设置数据库外键，由服务校验归属。';
COMMENT ON COLUMN schedule_exceptions.occurrence_date IS '重复系列原始发生项的中国标准时间日期，用于定位单次例外。';
COMMENT ON COLUMN schedule_exceptions.exception_action IS '例外行为，只允许 cancelled 取消该次，或 override 使用下列非空覆盖字段。';
COMMENT ON COLUMN schedule_exceptions.title IS '覆盖后的单次标题；override 时由服务完整保存，cancelled 时为空。';
COMMENT ON COLUMN schedule_exceptions.content IS '覆盖后的单次内容；工作事项内容仍仅向拥有者返回。';
COMMENT ON COLUMN schedule_exceptions.start_at IS '覆盖后的单次开始时刻，使用带时区时间；cancelled 时为空。';
COMMENT ON COLUMN schedule_exceptions.end_at IS '覆盖后的单次结束时刻，必须晚于覆盖开始时刻。';
COMMENT ON COLUMN schedule_exceptions.capacity IS '覆盖后的单次开放时段容量；工作事项或取消例外为空。';
COMMENT ON COLUMN schedule_exceptions.created_at IS '例外创建时间，使用带时区时间并默认为数据库当前时间。';
COMMENT ON COLUMN schedule_exceptions.updated_at IS '例外最近修改时间，使用带时区时间并由服务端维护。';

COMMENT ON TABLE schedule_bookings IS '客户预约申请表，保存访客对开放档期具体发生项的申请及拥有者处理状态。';
COMMENT ON COLUMN schedule_bookings.id IS '数据库内部自增主键，仅供数据库内部使用。';
COMMENT ON COLUMN schedule_bookings.business_id IS '预约申请对外业务 UUID，每条申请唯一。';
COMMENT ON COLUMN schedule_bookings.user_id IS '档期拥有者的 users.business_id，由分享令牌解析，不接受访客提供。';
COMMENT ON COLUMN schedule_bookings.schedule_business_id IS '申请对应的 schedule_items.business_id，不设置数据库外键。';
COMMENT ON COLUMN schedule_bookings.occurrence_start IS '访客申请的实际发生项开始时刻，使用带时区时间并与开放档期一致。';
COMMENT ON COLUMN schedule_bookings.occurrence_end IS '访客申请的实际发生项结束时刻，使用带时区时间。';
COMMENT ON COLUMN schedule_bookings.guest_name IS '访客必填姓名，仅向档期拥有者展示，用于线下联系。';
COMMENT ON COLUMN schedule_bookings.guest_phone IS '访客必填联系电话，仅向档期拥有者展示，用于线下联系。';
COMMENT ON COLUMN schedule_bookings.note IS '访客可选备注，最多 1000 个字符，不写入服务日志。';
COMMENT ON COLUMN schedule_bookings.booking_status IS '申请状态；待确认超时、拒绝后释放名额，确认后继续占用名额。';
COMMENT ON COLUMN schedule_bookings.request_code IS '随机生成的访客申请编号，唯一且不包含用户或数据库内部标识。';
COMMENT ON COLUMN schedule_bookings.expires_at IS '待确认申请占位到期时刻，创建后 24 小时；其他状态为空。';
COMMENT ON COLUMN schedule_bookings.created_at IS '申请创建时间，使用带时区时间并默认为数据库当前时间。';
COMMENT ON COLUMN schedule_bookings.updated_at IS '申请状态或信息最近修改时间，使用带时区时间并由服务端维护。';

COMMENT ON TABLE schedule_shares IS '用户预约分享配置表，保存可撤销随机令牌的摘要，不保存原始公开令牌。';
COMMENT ON COLUMN schedule_shares.id IS '数据库内部自增主键，仅供数据库内部使用。';
COMMENT ON COLUMN schedule_shares.business_id IS '预约分享配置对外业务 UUID，每份配置唯一。';
COMMENT ON COLUMN schedule_shares.user_id IS '分享链接拥有者的 users.business_id，由服务端认证后确定。';
COMMENT ON COLUMN schedule_shares.token_hash IS '随机分享令牌的小写 SHA-256 十六进制摘要，用于公开令牌匹配。';
COMMENT ON COLUMN schedule_shares.enabled IS '分享链接是否有效；关闭后对应预约地址立即不可访问。';
COMMENT ON COLUMN schedule_shares.created_at IS '分享配置创建时间，使用带时区时间并默认为数据库当前时间。';
COMMENT ON COLUMN schedule_shares.updated_at IS '分享配置最近修改时间，使用带时区时间并由服务端维护。';

COMMIT;
