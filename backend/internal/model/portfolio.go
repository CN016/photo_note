package model

import (
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Portfolio 是独立作品集；归属由服务端会话确定，标签保存业务 UUID。
type Portfolio struct {
	BaseModel
	UserID         uuid.UUID      `gorm:"column:user_id;type:uuid;not null"`
	Title          string         `gorm:"column:title;type:varchar(120);not null"`
	Description    string         `gorm:"column:description;type:text;not null"`
	TagIDs         pq.StringArray `gorm:"column:tag_ids;type:uuid[];not null"`
	ShareTokenHash *string        `gorm:"column:share_token_hash;type:varchar(64)"`
}

// TableName 固定作品集表名称。
func (Portfolio) TableName() string { return "portfolios" }

// PortfolioPhoto 保存作品集的公开副本；元数据独立保存，不依赖原灵感图存续。
type PortfolioPhoto struct {
	BaseModel
	UserID        uuid.UUID      `gorm:"column:user_id;type:uuid;not null"`
	PortfolioID   uuid.UUID      `gorm:"column:portfolio_id;type:uuid;not null"`
	ImageKey      string         `gorm:"column:image_key;type:text;not null"`
	Title         string         `gorm:"column:title;type:varchar(255);not null"`
	TagIDs        pq.StringArray `gorm:"column:tag_ids;type:uuid[];not null"`
	SortOrder     int64          `gorm:"column:sort_order;type:bigint;not null"`
	ImageMetadata JSONB          `gorm:"column:image_metadata;type:jsonb;not null"`
}

// TableName 固定作品照片表名称。
func (PortfolioPhoto) TableName() string { return "portfolio_photos" }

// PortfolioProject 保存作品集与策划的业务 UUID 关联；不使用数据库外键。
type PortfolioProject struct {
	BaseModel
	UserID      uuid.UUID `gorm:"column:user_id;type:uuid;not null"`
	PortfolioID uuid.UUID `gorm:"column:portfolio_id;type:uuid;not null"`
	ProjectID   uuid.UUID `gorm:"column:project_id;type:uuid;not null"`
}

// TableName 固定作品集策划关联表名称。
func (PortfolioProject) TableName() string { return "portfolio_projects" }
