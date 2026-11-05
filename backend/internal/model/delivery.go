package model

import (
	"github.com/google/uuid"
	"github.com/lib/pq"
	"time"
)

// DeliveryTask 是创建者私有的交付任务；分享摘要、内部备注和归属不直接序列化。
type DeliveryTask struct {
	BaseModel
	UserID         uuid.UUID  `json:"-"`
	Title          string     `json:"title"`
	Customer       string     `json:"customer"`
	Description    string     `json:"description"`
	InternalNote   string     `json:"-"`
	Stage          string     `json:"stage"`
	Status         string     `json:"status"`
	Version        int        `json:"version"`
	LimitMode      string     `json:"limit_mode"`
	SelectionLimit int        `json:"selection_limit"`
	Deadline       *time.Time `json:"deadline"`
	ExpiresAt      *time.Time `json:"expires_at"`
	AllowOriginal  bool       `json:"allow_original"`
	AllowDownload  bool       `json:"allow_download"`
	ShareHash      *string    `json:"-"`
	AccessHash     string     `json:"-"`
	CurrentRound   int        `json:"current_round"`
	ReceivedAt     *time.Time `json:"received_at"`
}

// TableName 对应交付任务版本化迁移。
func (DeliveryTask) TableName() string { return "delivery_tasks" }

// DeliveryPhoto 将原文件与压缩预览分离，任务关联只使用业务 UUID；发布批次为零代表草稿。
type DeliveryPhoto struct {
	BaseModel
	UserID         uuid.UUID  `json:"-"`
	TaskID         uuid.UUID  `json:"task_id"`
	Kind           string     `json:"kind"`
	Filename       string     `json:"filename"`
	GroupName      string     `json:"group_name"`
	OriginalKey    string     `json:"-"`
	PreviewKey     string     `json:"-"`
	Digest         string     `json:"-"`
	Bytes          int64      `json:"bytes"`
	PreviewBytes   int        `json:"preview_bytes"`
	Mark           string     `json:"mark"`
	SourceID       *uuid.UUID `json:"source_id"`
	PublishedBatch int        `json:"published_batch"`
}

// TableName 对应交付资产表。
func (DeliveryPhoto) TableName() string { return "delivery_photos" }

// DeliveryRound 持有当轮发布清单和规则快照，正式提交后不能直接覆盖选择；退回创建新轮次。
type DeliveryRound struct {
	BaseModel
	UserID         uuid.UUID      `json:"-"`
	TaskID         uuid.UUID      `json:"-"`
	Number         int            `json:"number"`
	Version        int            `json:"version"`
	Status         string         `json:"status"`
	PhotoIDs       pq.StringArray `gorm:"type:uuid[]" json:"photo_ids"`
	SelectedIDs    pq.StringArray `gorm:"type:uuid[]" json:"selected_ids"`
	Notes          JSONB          `gorm:"type:jsonb" json:"notes"`
	LimitMode      string         `json:"limit_mode"`
	SelectionLimit int            `json:"selection_limit"`
	Deadline       *time.Time     `json:"deadline"`
	SubmittedAt    *time.Time     `json:"submitted_at"`
}

// TableName 对应选片轮次表。
func (DeliveryRound) TableName() string { return "delivery_rounds" }

// DeliveryEvent 记录安全的操作分类及原阶段，不记录令牌、访问码和客户内容。
type DeliveryEvent struct {
	BaseModel
	UserID      uuid.UUID `json:"-"`
	TaskID      uuid.UUID `json:"-"`
	Action      string    `json:"action"`
	Stage       string    `json:"stage"`
	TaskVersion int       `json:"-"`
	Notified    bool      `json:"-"`
}

// TableName 对应流转记录表。
func (DeliveryEvent) TableName() string { return "delivery_events" }

// DeliveryExport 是持久化打包任务；照片清单和业务版本冻结，旧版本作业不能发布到新任务状态。
type DeliveryExport struct {
	BaseModel
	UserID      uuid.UUID      `json:"-"`
	TaskID      uuid.UUID      `json:"-"`
	TaskVersion int            `json:"-"`
	Kind        string         `json:"kind"`
	PhotoIDs    pq.StringArray `gorm:"type:uuid[]" json:"-"`
	Status      string         `json:"status"`
	Progress    int            `json:"progress"`
	Total       int            `json:"total"`
	FileKey     string         `json:"-"`
	Bytes       int64          `json:"bytes"`
	ErrorCode   string         `json:"error_code"`
	Attempts    int            `json:"-"`
	Public      bool           `json:"-"`
}

// TableName 固定持久化交付打包作业表。
func (DeliveryExport) TableName() string { return "delivery_exports" }
