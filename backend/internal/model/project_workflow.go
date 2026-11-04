package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ProjectShooting 保存可逐步填写的拍摄信息；空值表示尚未确定，完整后才能同步档期。
type ProjectShooting struct {
	StartAt  *time.Time `json:"start_at" gorm:"column:shoot_start_at;type:timestamptz"`
	EndAt    *time.Time `json:"end_at" gorm:"column:shoot_end_at;type:timestamptz"`
	Location string     `json:"location" gorm:"column:shoot_location;type:varchar(240);not null;default:''"`
	Person   string     `json:"person" gorm:"column:shoot_person;type:varchar(120);not null;default:''"`
	Kind     string     `json:"kind" gorm:"column:shoot_kind;type:varchar(8);not null;default:''"`
}

// Complete 判断能否创建有明确时间、地点和人物的拍摄档期。
func (s ProjectShooting) Complete() bool {
	return s.StartAt != nil && s.EndAt != nil && s.Location != "" && s.Person != "" && s.Kind != ""
}

// Fingerprint 标识上次已处理的拍摄信息，不包含正文，且不作为授权依据。
// UTC 规范化防止同一时刻的不同偏移表示造成重复同步。
func (s ProjectShooting) Fingerprint() string {
	if s.StartAt != nil {
		value := s.StartAt.UTC()
		s.StartAt = &value
	}
	if s.EndAt != nil {
		value := s.EndAt.UTC()
		s.EndAt = &value
	}
	data, _ := json.Marshal(s)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// ProjectReview 是完成总结时生成的独立文档；之后的编辑不回写原策划。
type ProjectReview struct {
	BaseModel
	UserID    uuid.UUID `gorm:"column:user_id;type:uuid;not null"`
	ProjectID uuid.UUID `gorm:"column:project_id;type:uuid;not null"`
	Title     string    `gorm:"column:title;type:varchar(600);not null"`
	Content   string    `gorm:"column:content;type:text;not null"`
	Revision  int64     `gorm:"column:revision;type:bigint;not null;default:1"`
}

// TableName 固定总结文档表名，与版本化迁移及租户回调保持一致。
func (ProjectReview) TableName() string { return "project_reviews" }

// ProjectStatusCounts 用于完整搜索结果的分类计数，不受当前分页和所选状态影响。
type ProjectStatusCounts struct {
	Unfinished int64 `json:"unfinished"`
	Executed   int64 `json:"executed"`
	Completed  int64 `json:"completed"`
}
