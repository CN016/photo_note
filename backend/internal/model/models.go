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
}

// TableName 固定配置模型使用与版本化建表 SQL 相同的表名。
func (AppConfig) TableName() string { return "app_configs" }

// User 映射用户表；密码哈希仅供认证流程使用，不通过 JSON 序列化。
type User struct {
	BaseModel
	Nickname     string  `gorm:"column:nickname;type:varchar(64);not null"`
	Phone        string  `gorm:"column:phone;type:varchar(16);not null;uniqueIndex"`
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
	Data     JSONB          `gorm:"column:data;type:jsonb;not null"`
}

// TableName 固定灵感图模型使用与版本化建表 SQL 相同的表名。
func (Inspiration) TableName() string { return "inspirations" }
