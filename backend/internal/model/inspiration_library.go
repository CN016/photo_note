package model

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// InspirationAnalysis 保存服务端生成的可查询画像；空值表示尚无有效结果，客户端不能写入这些字段。
type InspirationAnalysis struct {
	PeopleCount       *int       `json:"people_count"`
	IsEmptyShot       *bool      `json:"is_empty_shot"`
	DominantColor     *string    `json:"dominant_color"`
	DominantHue       *float64   `json:"dominant_hue"`
	ColorBucket       *string    `json:"color_bucket"`
	ColorPalette      *JSONB     `gorm:"type:jsonb" json:"color_palette,omitempty"`
	AnalysisStatus    string     `gorm:"not null;default:pending_compression" json:"analysis_status"`
	AnalysisVersion   int64      `gorm:"not null;default:0" json:"analysis_version"`
	AnalysisAlgorithm string     `json:"analysis_algorithm"`
	AnalyzedAt        *time.Time `json:"analyzed_at"`
}

// InspirationGoal 是当前空间当前用户的目标；提醒时间使用北京时间 HH:mm，不接受归属字段。
type InspirationGoal struct {
	BaseModel
	UserID       uuid.UUID `json:"-"`
	Enabled      bool      `json:"enabled"`
	Target       int       `json:"target"`
	ReminderTime string    `json:"reminder_time"`
}

func (InspirationGoal) TableName() string { return "inspiration_goals" }

// InspirationActivity 持久保存每日成功入库次数；删除图片不回减，目标快照仅当天可更新。
type InspirationActivity struct {
	BaseModel
	UserID                 uuid.UUID  `json:"-"`
	Day                    string     `gorm:"type:date" json:"day"`
	SavedCount             int        `json:"saved_count"`
	Target                 *int       `json:"target"`
	ReminderNotificationID *uuid.UUID `gorm:"type:uuid" json:"-"`
	ReminderAcknowledgedAt *time.Time `json:"-"`
}

func (InspirationActivity) TableName() string { return "inspiration_activities" }

// InspirationProcessingJob 用数据库代替内存排队；租约令牌防止超时旧 worker 回写新任务。
type InspirationProcessingJob struct {
	BaseModel
	UserID        uuid.UUID
	InspirationID uuid.UUID
	ImageKey      string
	AsyncVersion  int64
	Algorithm     string
	Stage         string
	AnalysisReady bool `gorm:"not null;default:false"`
	Attempts      int
	NextAttemptAt time.Time
	LeaseUntil    *time.Time
	LeaseToken    *uuid.UUID `gorm:"type:uuid"`
	ErrorCode     string
}

func (InspirationProcessingJob) TableName() string { return "inspiration_processing_jobs" }

// InspirationFilters 是额外检索条件；服务层验证后传入 repository，不能包含客户端租户标识。
type InspirationFilters struct {
	DateFrom  string
	DateTo    string
	Project   string
	People    string
	EmptyShot string
	Color     string
	Status    string
}

// InspirationFacet 是图库分组的安全投影，只返回业务键、展示名称和数量。
type InspirationFacet struct {
	Key      string               `json:"key"`
	Name     string               `json:"name,omitempty"`
	Count    int64                `json:"count"`
	Previews []InspirationPreview `gorm:"-" json:"previews,omitempty"`
}

// InspirationPreview 只提供已鉴权图库缩略图所需的私有定位信息，不提供公开下载地址。
type InspirationPreview struct {
	BusinessID uuid.UUID `json:"business_id"`
	ImageKey   string    `json:"image_key"`
	FileName   string    `json:"file_name"`
	FileHash   *string   `json:"file_hash"`
	Day        string    `json:"-"`
}

type inspirationFilterKey struct{}

// WithInspirationFilters 仅传递服务层已验证的查询条件，不改变可信身份。
func WithInspirationFilters(ctx context.Context, filters InspirationFilters) context.Context {
	return context.WithValue(ctx, inspirationFilterKey{}, filters)
}

// InspirationFiltersFromContext 未提供扩展筛选时保持旧列表行为。
func InspirationFiltersFromContext(ctx context.Context) InspirationFilters {
	value, _ := ctx.Value(inspirationFilterKey{}).(InspirationFilters)
	return value
}
