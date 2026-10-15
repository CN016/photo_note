package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// BaseModel 声明所有项目业务表共用字段；ID 只用于数据库内部，自增键绝不进入 API DTO。
type BaseModel struct {
	ID         int       `gorm:"column:id;primaryKey;autoIncrement" json:"-"`
	BusinessID uuid.UUID `gorm:"column:business_id;type:uuid;not null;uniqueIndex" json:"-"`
	CreatedAt  time.Time `gorm:"column:created_at;type:timestamptz;not null" json:"-"`
	UpdatedAt  time.Time `gorm:"column:updated_at;type:timestamptz;not null" json:"-"`
}

// AppConfig 映射仅供服务端管理的非秘密业务配置表。
type AppConfig struct {
	BaseModel
	ConfigKey   string `gorm:"column:config_key;type:text;not null;uniqueIndex"`
	ConfigValue string `gorm:"column:config_value;type:text;not null"`
	Description string `gorm:"column:description;type:text;not null"`
	// IsPublic 默认关闭；只有显式开放的配置值才能通过无需鉴权的客户端接口读取。
	IsPublic bool `gorm:"column:is_public;type:boolean;not null;default:false" json:"-"`
}

// TableName 固定配置模型使用与版本化建表 SQL 相同的表名。
func (AppConfig) TableName() string { return "app_configs" }

// User 映射用户表；密码哈希仅供认证流程使用，不通过 JSON 序列化。
type User struct {
	BaseModel
	Nickname     string  `gorm:"column:nickname;type:varchar(64);not null"`
	Phone        string  `gorm:"column:phone;type:varchar(11);not null;uniqueIndex"`
	PasswordHash string  `gorm:"column:password_hash;type:text;not null" json:"-"`
	AvatarKey    *string `gorm:"column:avatar_key;type:text"`
}

// TableName 固定用户模型使用与版本化建表 SQL 相同的表名。
func (User) TableName() string { return "users" }

// Tag 映射标签表；UserID 保存 users.business_id，不声明 GORM 关系以避免外键生成。
type Tag struct {
	BaseModel
	UserID  uuid.UUID `gorm:"column:user_id;type:uuid;not null;index"`
	Content string    `gorm:"column:content;type:varchar(64);not null"`
}

// TableName 固定标签模型使用与版本化建表 SQL 相同的表名。
func (Tag) TableName() string { return "tags" }

// Inspiration 映射灵感图表；标签数组保存标签业务 UUID，归属和存在性由 service 校验。
type Inspiration struct {
	BaseModel
	UserID   uuid.UUID      `gorm:"column:user_id;type:uuid;not null;index"`
	TagIDs   pq.StringArray `gorm:"column:tag_ids;type:uuid[];not null;default:ARRAY[]::uuid[]"`
	ImageKey string         `gorm:"column:image_key;type:text;not null"`
	// FileName 保存客户端提供的原始文件名，只作为展示信息，不用于拼接磁盘路径。
	FileName string `gorm:"column:file_name;type:text;not null;default:''"`
	// FileHash 保存有效 storekey 指向文件的 SHA-256 小写十六进制值；非 storekey 图片为空。
	FileHash *string `gorm:"column:file_hash;type:varchar(64);index:ix_inspirations_file_hash" json:"-"`
	// FileSize 保存文件经过压缩并写回存储后的字节数；非 storekey 图片和历史记录可为空。
	FileSize *int64 `gorm:"column:file_size;type:bigint" json:"-"`
	// AsyncVersion 是异步图片处理代次；新增记录为 0，提交新的图片处理任务时递增。
	AsyncVersion int64 `gorm:"column:async_version;type:bigint;not null;default:0" json:"-"`
	Data         JSONB `gorm:"column:data;type:jsonb;not null"`
}

// TableName 固定灵感图模型使用与版本化建表 SQL 相同的表名。
func (Inspiration) TableName() string { return "inspirations" }

// Feedback 映射用户反馈记录；图片只保存 storekey，文件内容仍由独立文件存储管理。
type Feedback struct {
	BaseModel
	UserID    *uuid.UUID     `gorm:"column:user_id;type:uuid;index"`
	Content   string         `gorm:"column:content;type:text;not null;default:''"`
	ImageKeys pq.StringArray `gorm:"column:image_keys;type:text[];not null;default:ARRAY[]::text[]"`
}

// TableName 固定反馈模型使用与版本化建表 SQL 相同的表名。
func (Feedback) TableName() string { return "feedbacks" }

// LedgerRecord 保存当前用户的一笔收入或支出；草稿 JSON 与已完成金额分开，编辑时不影响正式统计。
type LedgerRecord struct {
	BaseModel
	UserID        uuid.UUID  `gorm:"column:user_id;type:uuid;not null;index:ix_ledger_records_user_date,priority:1"`
	RecordDate    time.Time  `gorm:"column:record_date;type:date;not null;index:ix_ledger_records_user_date,priority:2"`
	RecordTime    *time.Time `gorm:"column:record_time;type:time"`
	RecordType    string     `gorm:"column:record_type;type:varchar(8);not null"`
	AmountFen     int64      `gorm:"column:amount_fen;type:bigint;not null;default:0"`
	Category      string     `gorm:"column:category;type:text;not null;default:''"`
	PaymentMethod string     `gorm:"column:payment_method;type:text;not null;default:''"`
	Flag          string     `gorm:"column:flag;type:varchar(24);not null;default:''"`
	Note          string     `gorm:"column:note;type:text;not null;default:''"`
	Status        string     `gorm:"column:record_status;type:varchar(16);not null;default:'draft'"`
	DraftData     *JSONB     `gorm:"column:draft_data;type:jsonb"`
	DraftVersion  int64      `gorm:"column:draft_version;type:bigint;not null;default:0"`
}

// TableName 固定账本记录模型使用与版本化迁移相同的表名。
func (LedgerRecord) TableName() string { return "ledger_records" }

// ScheduleItem 保存用户的工作事项或开放预约时段；重复规则按中国标准时间展开。
type ScheduleItem struct {
	BaseModel
	UserID             uuid.UUID     `gorm:"column:user_id;type:uuid;not null;index"`
	Kind               string        `gorm:"column:item_kind;type:varchar(16);not null"`
	Title              string        `gorm:"column:title;type:varchar(120);not null"`
	Content            string        `gorm:"column:content;type:text;not null;default:''"`
	StartAt            time.Time     `gorm:"column:start_at;type:timestamptz;not null"`
	EndAt              time.Time     `gorm:"column:end_at;type:timestamptz;not null"`
	Recurrence         string        `gorm:"column:recurrence;type:varchar(16);not null;default:'none'"`
	RecurrenceWeekdays pq.Int64Array `gorm:"column:recurrence_weekdays;type:integer[];not null;default:ARRAY[]::integer[]"`
	RecurrenceEnd      *time.Time    `gorm:"column:recurrence_end;type:date"`
	Capacity           int           `gorm:"column:capacity;type:integer;not null;default:1"`
}

// TableName 固定档期模型使用与版本化 SQL 迁移一致的表名。
func (ScheduleItem) TableName() string { return "schedule_items" }

// ScheduleException 保存某个重复档期发生项的取消或覆盖内容，不修改整个重复系列。
type ScheduleException struct {
	BaseModel
	UserID             uuid.UUID  `gorm:"column:user_id;type:uuid;not null;index"`
	ScheduleBusinessID uuid.UUID  `gorm:"column:schedule_business_id;type:uuid;not null;index"`
	OccurrenceDate     time.Time  `gorm:"column:occurrence_date;type:date;not null"`
	Action             string     `gorm:"column:exception_action;type:varchar(16);not null"`
	Title              *string    `gorm:"column:title;type:varchar(120)"`
	Content            *string    `gorm:"column:content;type:text"`
	StartAt            *time.Time `gorm:"column:start_at;type:timestamptz"`
	EndAt              *time.Time `gorm:"column:end_at;type:timestamptz"`
	Capacity           *int       `gorm:"column:capacity;type:integer"`
}

// TableName 固定单次例外模型使用与版本化 SQL 迁移一致的表名。
func (ScheduleException) TableName() string { return "schedule_exceptions" }

// ScheduleBooking 保存访客对一个具体发生项的申请；游客联系方式只用于拥有者线下联系。
type ScheduleBooking struct {
	BaseModel
	UserID             uuid.UUID  `gorm:"column:user_id;type:uuid;not null;index"`
	ScheduleBusinessID uuid.UUID  `gorm:"column:schedule_business_id;type:uuid;not null;index"`
	OccurrenceStart    time.Time  `gorm:"column:occurrence_start;type:timestamptz;not null"`
	OccurrenceEnd      time.Time  `gorm:"column:occurrence_end;type:timestamptz;not null"`
	GuestName          string     `gorm:"column:guest_name;type:varchar(80);not null"`
	GuestPhone         string     `gorm:"column:guest_phone;type:varchar(32);not null"`
	Note               string     `gorm:"column:note;type:varchar(1000);not null;default:''"`
	Status             string     `gorm:"column:booking_status;type:varchar(16);not null;default:'pending'"`
	RequestCode        string     `gorm:"column:request_code;type:varchar(12);not null;uniqueIndex"`
	ExpiresAt          *time.Time `gorm:"column:expires_at;type:timestamptz"`
}

// TableName 固定预约申请模型使用与版本化 SQL 迁移一致的表名。
func (ScheduleBooking) TableName() string { return "schedule_bookings" }

// ScheduleShare 只保存公开随机令牌的 SHA-256 摘要；撤销后原链接立即失效。
type ScheduleShare struct {
	BaseModel
	UserID    uuid.UUID `gorm:"column:user_id;type:uuid;not null;uniqueIndex"`
	TokenHash string    `gorm:"column:token_hash;type:char(64);not null;uniqueIndex"`
	Enabled   bool      `gorm:"column:enabled;type:boolean;not null;default:true"`
}

// TableName 固定分享配置模型使用与版本化 SQL 迁移一致的表名。
func (ScheduleShare) TableName() string { return "schedule_shares" }
