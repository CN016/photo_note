BEGIN;

CREATE TABLE ledger_records (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    business_id uuid NOT NULL,
    user_id uuid NOT NULL,
    record_date date NOT NULL,
    record_time time,
    record_type varchar(8) NOT NULL,
    amount_fen bigint NOT NULL DEFAULT 0,
    category text NOT NULL DEFAULT '',
    payment_method text NOT NULL DEFAULT '',
    flag varchar(24) NOT NULL DEFAULT '',
    note text NOT NULL DEFAULT '',
    record_status varchar(16) NOT NULL DEFAULT 'draft',
    draft_data jsonb,
    draft_version bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ck_ledger_records_type CHECK (record_type IN ('income', 'expense')),
    CONSTRAINT ck_ledger_records_amount CHECK (amount_fen >= 0),
    CONSTRAINT ck_ledger_records_flag CHECK (flag IN ('', 'important', 'anomaly', 'pending_check')),
    CONSTRAINT ck_ledger_records_status CHECK (record_status IN ('draft', 'complete')),
    CONSTRAINT ck_ledger_records_draft CHECK (record_status = 'complete' OR draft_data IS NOT NULL),
    CONSTRAINT ck_ledger_records_draft_version CHECK (draft_version >= 0)
);

CREATE UNIQUE INDEX ux_ledger_records_business_id ON ledger_records (business_id);
CREATE INDEX ix_ledger_records_user_date ON ledger_records (user_id, record_date DESC);

COMMENT ON TABLE ledger_records IS '个人收支账本记录表，每行保存一笔用户收入或支出及其草稿状态。';
COMMENT ON COLUMN ledger_records.id IS '数据库内部自增主键，仅供数据库内部使用，不返回客户端，也不用于表间关联。';
COMMENT ON COLUMN ledger_records.business_id IS '服务端生成并对外使用的记录业务 UUID，每条账本记录唯一。';
COMMENT ON COLUMN ledger_records.user_id IS '记录所属用户的 users.business_id，由已验证的访问令牌确定，不设置数据库外键。';
COMMENT ON COLUMN ledger_records.record_date IS '收支发生的本地日历日期，格式为 YYYY-MM-DD，不包含时区转换。';
COMMENT ON COLUMN ledger_records.record_time IS '收支发生的本地时分，可为空；仅保存时间部分，不包含日期和时区。';
COMMENT ON COLUMN ledger_records.record_type IS '记录类型，只允许 income 收入或 expense 支出，金额本身始终为非负整数分。';
COMMENT ON COLUMN ledger_records.amount_fen IS '收支金额的人民币分值，使用非负 bigint 避免小数浮点精度误差。';
COMMENT ON COLUMN ledger_records.category IS '用户填写的收支分类文本，最多 64 个 Unicode 字符，允许自定义分类。';
COMMENT ON COLUMN ledger_records.payment_method IS '支付或收款方式文本，最多 64 个 Unicode 字符，可保存预设值或导入值。';
COMMENT ON COLUMN ledger_records.flag IS '记录标记，只允许空值、important 重点、anomaly 异常或 pending_check 待核对。';
COMMENT ON COLUMN ledger_records.note IS '记录备注文本，最多 2000 个 Unicode 字符，不记录在服务端日志中。';
COMMENT ON COLUMN ledger_records.record_status IS '记录状态，只允许 draft 草稿或 complete 已完成；统计仅使用已完成记录。';
COMMENT ON COLUMN ledger_records.draft_data IS '最近一次自动保存的完整表单快照；新建草稿从此读取，编辑正式记录时暂存待应用字段。';
COMMENT ON COLUMN ledger_records.draft_version IS '草稿快照修订号，用于比较并交换并发写入，防止迟到的旧请求覆盖新内容。';
COMMENT ON COLUMN ledger_records.created_at IS '记录创建时间，使用带时区时间类型并默认为数据库当前时间。';
COMMENT ON COLUMN ledger_records.updated_at IS '记录最近修改时间，使用带时区时间类型并由服务端写入。';

COMMIT;
