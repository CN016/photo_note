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
