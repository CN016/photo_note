package model

import (
	"encoding/json"
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
	// IsSecret 标识配置值是否属于秘密；这类值仅在服务端读取，管理接口也只回报是否已配置。
	IsSecret bool `gorm:"column:is_secret;type:boolean;not null;default:false" json:"-"`
}

// TableName 固定配置模型使用与版本化建表 SQL 相同的表名。
func (AppConfig) TableName() string { return "app_configs" }

// ErrorLog 保存服务端错误的安全诊断信息；不保存请求体、访问令牌或数据库自增关联。
type ErrorLog struct {
	BaseModel
	DedupKey       string     `gorm:"column:dedup_key;type:char(64);not null;index:ix_error_logs_dedup_last_seen,priority:1"`
	HTTPStatus     int        `gorm:"column:http_status;type:integer;not null"`
	ErrorCode      string     `gorm:"column:error_code;type:varchar(64);not null"`
	ErrorMessage   string     `gorm:"column:error_message;type:text;not null"`
	CauseType      string     `gorm:"column:cause_type;type:varchar(128);not null;default:''"`
	CauseMessage   string     `gorm:"column:cause_message;type:text;not null;default:''"`
	SourceFile     string     `gorm:"column:source_file;type:text;not null;default:''"`
	SourceLine     int        `gorm:"column:source_line;type:integer;not null;default:0"`
	SourceFunction string     `gorm:"column:source_function;type:text;not null;default:''"`
	StackTrace     string     `gorm:"column:stack_trace;type:text;not null;default:''"`
	RequestID      *uuid.UUID `gorm:"column:request_id;type:uuid"`
	HTTPMethod     string     `gorm:"column:http_method;type:varchar(16);not null;default:''"`
	HTTPRoute      string     `gorm:"column:http_route;type:text;not null;default:''"`
	Occurrences    int64      `gorm:"column:occurrence_count;type:bigint;not null;default:1"`
	LastSeenAt     time.Time  `gorm:"column:last_seen_at;type:timestamptz;not null;index:ix_error_logs_dedup_last_seen,priority:2"`
}

// TableName 固定错误日志模型使用与版本化迁移 SQL 相同的表名。
func (ErrorLog) TableName() string { return "error_logs" }

// User 映射用户表；密码哈希仅供认证流程使用，不通过 JSON 序列化。
type User struct {
	BaseModel
	Nickname     string  `gorm:"column:nickname;type:varchar(64);not null"`
	Phone        string  `gorm:"column:phone;type:varchar(11);not null;uniqueIndex"`
	PasswordHash string  `gorm:"column:password_hash;type:text;not null" json:"-"`
	AvatarKey    *string `gorm:"column:avatar_key;type:text"`
	// IsAdmin 由数据库运维指定，任何注册或个人资料请求都不能修改此权限。
	IsAdmin bool `gorm:"column:is_admin;type:boolean;not null;default:false"`
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
	UserID uuid.UUID      `gorm:"column:user_id;type:uuid;not null;index"`
	TagIDs pq.StringArray `gorm:"column:tag_ids;type:uuid[];not null;default:ARRAY[]::uuid[]"`
	// TagNames 是查询时按当前用户的标签记录填充的展示字段，不持久化到 inspirations 表。
	TagNames []string `gorm:"-" json:"-"`
	ImageKey string   `gorm:"column:image_key;type:text;not null"`
	// FileName 保存客户端提供的原始文件名，只作为展示信息，不用于拼接磁盘路径。
	FileName string `gorm:"column:file_name;type:text;not null;default:''"`
	// FileHash 保存有效 storekey 指向文件的 SHA-256 小写十六进制值；非 storekey 图片为空。
	FileHash *string `gorm:"column:file_hash;type:varchar(64);index:ix_inspirations_file_hash" json:"-"`
	// FileSize 保存文件经过压缩并写回存储后的字节数；非 storekey 图片和历史记录可为空。
	FileSize *int64 `gorm:"column:file_size;type:bigint" json:"-"`
	// AsyncVersion 是异步图片处理代次；新增记录为 0，提交新的图片处理任务时递增。
	AsyncVersion int64 `gorm:"column:async_version;type:bigint;not null;default:0" json:"-"`
	// ImageMetadata 将可检索的常见图像属性映射到独立可空列；额外标签使用 JSONB 扩展字段。
	ImageMetadata InspirationImageMetadata `gorm:"embedded" json:"-"`
	Data          JSONB                    `gorm:"column:data;type:jsonb;not null"`
}

// TableName 固定灵感图模型使用与版本化建表 SQL 相同的表名。
func (Inspiration) TableName() string { return "inspirations" }

// InspirationImageMetadata 保存浏览器和服务端从原始图片读取的 EXIF、IPTC、XMP 等属性。
// 所有数值字段采用固定单位；原始日期字符串不擅自附加时区。
type InspirationImageMetadata struct {
	ImageID                 *string        `gorm:"column:image_id" json:"image_id,omitempty"`
	SourceFileSize          *int64         `gorm:"column:source_file_size" json:"source_file_size,omitempty"`
	SourceLastModifiedAt    *time.Time     `gorm:"column:source_last_modified_at" json:"source_last_modified_at,omitempty"`
	ImageWidth              *int           `gorm:"column:image_width" json:"image_width,omitempty"`
	ImageHeight             *int           `gorm:"column:image_height" json:"image_height,omitempty"`
	HorizontalResolutionDPI *float64       `gorm:"column:horizontal_resolution_dpi" json:"horizontal_resolution_dpi,omitempty"`
	VerticalResolutionDPI   *float64       `gorm:"column:vertical_resolution_dpi" json:"vertical_resolution_dpi,omitempty"`
	BitDepth                *int           `gorm:"column:bit_depth" json:"bit_depth,omitempty"`
	ColorSpace              *string        `gorm:"column:color_space" json:"color_space,omitempty"`
	Compression             *string        `gorm:"column:compression" json:"compression,omitempty"`
	Title                   *string        `gorm:"column:title" json:"title,omitempty"`
	Subject                 *string        `gorm:"column:subject" json:"subject,omitempty"`
	Rating                  *int           `gorm:"column:rating" json:"rating,omitempty"`
	Tags                    pq.StringArray `gorm:"column:metadata_tags;type:text[]" json:"tags,omitempty"`
	Author                  *string        `gorm:"column:author" json:"author,omitempty"`
	Comments                *string        `gorm:"column:comments" json:"comments,omitempty"`
	Copyright               *string        `gorm:"column:copyright" json:"copyright,omitempty"`
	Software                *string        `gorm:"column:software" json:"software,omitempty"`
	DateAcquired            *string        `gorm:"column:date_acquired" json:"date_acquired,omitempty"`
	DateTimeOriginal        *string        `gorm:"column:date_time_original" json:"date_time_original,omitempty"`
	DateTimeDigitized       *string        `gorm:"column:date_time_digitized" json:"date_time_digitized,omitempty"`
	CameraMake              *string        `gorm:"column:camera_make" json:"camera_make,omitempty"`
	CameraModel             *string        `gorm:"column:camera_model" json:"camera_model,omitempty"`
	CameraSerialNumber      *string        `gorm:"column:camera_serial_number" json:"camera_serial_number,omitempty"`
	LensMake                *string        `gorm:"column:lens_make" json:"lens_make,omitempty"`
	LensModel               *string        `gorm:"column:lens_model" json:"lens_model,omitempty"`
	LensSerialNumber        *string        `gorm:"column:lens_serial_number" json:"lens_serial_number,omitempty"`
	FlashMake               *string        `gorm:"column:flash_make" json:"flash_make,omitempty"`
	FlashModel              *string        `gorm:"column:flash_model" json:"flash_model,omitempty"`
	FlashSerialNumber       *string        `gorm:"column:flash_serial_number" json:"flash_serial_number,omitempty"`
	ApertureFNumber         *float64       `gorm:"column:aperture_f_number" json:"aperture_f_number,omitempty"`
	MaximumApertureFNumber  *float64       `gorm:"column:maximum_aperture_f_number" json:"maximum_aperture_f_number,omitempty"`
	ExposureTimeSeconds     *float64       `gorm:"column:exposure_time_seconds" json:"exposure_time_seconds,omitempty"`
	ISOSpeed                *float64       `gorm:"column:iso_speed" json:"iso_speed,omitempty"`
	ExposureCompensationEV  *float64       `gorm:"column:exposure_compensation_ev" json:"exposure_compensation_ev,omitempty"`
	ExposureProgram         *string        `gorm:"column:exposure_program" json:"exposure_program,omitempty"`
	FlashFired              *bool          `gorm:"column:flash_fired" json:"flash_fired,omitempty"`
	FlashMode               *string        `gorm:"column:flash_mode" json:"flash_mode,omitempty"`
	FlashEnergyBCPS         *float64       `gorm:"column:flash_energy_bcps" json:"flash_energy_bcps,omitempty"`
	FocalLengthMM           *float64       `gorm:"column:focal_length_mm" json:"focal_length_mm,omitempty"`
	FocalLength35mmMM       *float64       `gorm:"column:focal_length_35mm_mm" json:"focal_length_35mm_mm,omitempty"`
	SubjectDistanceM        *float64       `gorm:"column:subject_distance_m" json:"subject_distance_m,omitempty"`
	MeteringMode            *string        `gorm:"column:metering_mode" json:"metering_mode,omitempty"`
	LightSource             *string        `gorm:"column:light_source" json:"light_source,omitempty"`
	WhiteBalance            *string        `gorm:"column:white_balance" json:"white_balance,omitempty"`
	Contrast                *string        `gorm:"column:contrast" json:"contrast,omitempty"`
	BrightnessEV            *float64       `gorm:"column:brightness_ev" json:"brightness_ev,omitempty"`
	Saturation              *string        `gorm:"column:saturation" json:"saturation,omitempty"`
	Sharpness               *string        `gorm:"column:sharpness" json:"sharpness,omitempty"`
	DigitalZoomRatio        *float64       `gorm:"column:digital_zoom_ratio" json:"digital_zoom_ratio,omitempty"`
	CompressionBitsPerPixel *float64       `gorm:"column:compression_bits_per_pixel" json:"compression_bits_per_pixel,omitempty"`
	EXIFVersion             *string        `gorm:"column:exif_version" json:"exif_version,omitempty"`
	GPSLatitude             *float64       `gorm:"column:gps_latitude" json:"gps_latitude,omitempty"`
	GPSLongitude            *float64       `gorm:"column:gps_longitude" json:"gps_longitude,omitempty"`
	GPSAltitudeM            *float64       `gorm:"column:gps_altitude_m" json:"gps_altitude_m,omitempty"`
	GPSTimestamp            *string        `gorm:"column:gps_timestamp" json:"gps_timestamp,omitempty"`
	Extra                   JSONB          `gorm:"column:image_metadata_extra;type:jsonb;not null;default:'{}'" json:"extra,omitempty"`
}

// InspirationStatisticBucket 表示某个图片属性分组中的数量；Value 使用稳定键或清理后的设备名称。
type InspirationStatisticBucket struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// InspirationStatistics 汇总当前用户的拍摄属性分布，不包含图片 key、坐标或设备序列号。
type InspirationStatistics struct {
	TotalImages                int64                        `json:"total_images"`                  // 当前账号拥有的图片总数。
	ImagesWithShootingMetadata int64                        `json:"images_with_shooting_metadata"` // 至少存在一项拍摄参数的图片数。
	CollectionMonths           []InspirationStatisticBucket `json:"collection_months"`             // 最近 12 个 UTC 日历月的新增数量。
	CameraModels               []InspirationStatisticBucket `json:"camera_models"`                 // 相机制造商和型号合并后的前五项。
	LensModels                 []InspirationStatisticBucket `json:"lens_models"`                   // 镜头制造商和型号合并后的前五项。
	FocalLengths               []InspirationStatisticBucket `json:"focal_lengths"`                 // 35mm 等效焦距优先的焦段分布。
	Apertures                  []InspirationStatisticBucket `json:"apertures"`                     // 最大光圈值的分布。
	ISOSpeeds                  []InspirationStatisticBucket `json:"iso_speeds"`                    // ISO 感光度分布。
	ExposureTimes              []InspirationStatisticBucket `json:"exposure_times"`                // 快门曝光时间分布。
	FlashUsage                 []InspirationStatisticBucket `json:"flash_usage"`                   // 已知闪光灯触发状态的数量。
	AspectRatios               []InspirationStatisticBucket `json:"aspect_ratios"`                 // 根据图片尺寸归类的画面方向。
	Megapixels                 []InspirationStatisticBucket `json:"megapixels"`                    // 根据像素宽高积归类的百万像素数。
	ColorSpaces                []InspirationStatisticBucket `json:"color_spaces"`                  // 图片色彩空间前五项。
}

// MarshalJSON 确保尚未持久化的元数据也以对象形式输出扩展标签，而不是 null。
func (metadata InspirationImageMetadata) MarshalJSON() ([]byte, error) {
	type plain InspirationImageMetadata
	if len(metadata.Extra) == 0 {
		metadata.Extra = JSONB(`{}`)
	}
	return json.Marshal(plain(metadata))
}

// DatabaseColumns 返回灵感图元数据对应的完整列集；代次保护写入时也会显式清空缺失字段，避免残留旧图属性。
func (metadata InspirationImageMetadata) DatabaseColumns() map[string]any {
	extra := metadata.Extra
	if len(extra) == 0 {
		extra = JSONB(`{}`)
	}
	return map[string]any{
		"image_id": metadata.ImageID, "source_file_size": metadata.SourceFileSize,
		"source_last_modified_at": metadata.SourceLastModifiedAt, "image_width": metadata.ImageWidth,
		"image_height": metadata.ImageHeight, "horizontal_resolution_dpi": metadata.HorizontalResolutionDPI,
		"vertical_resolution_dpi": metadata.VerticalResolutionDPI, "bit_depth": metadata.BitDepth,
		"color_space": metadata.ColorSpace, "compression": metadata.Compression,
		"title": metadata.Title, "subject": metadata.Subject, "rating": metadata.Rating,
		"metadata_tags": metadata.Tags, "author": metadata.Author, "comments": metadata.Comments,
		"copyright": metadata.Copyright, "software": metadata.Software, "date_acquired": metadata.DateAcquired,
		"date_time_original": metadata.DateTimeOriginal, "date_time_digitized": metadata.DateTimeDigitized,
		"camera_make": metadata.CameraMake, "camera_model": metadata.CameraModel,
		"camera_serial_number": metadata.CameraSerialNumber, "lens_make": metadata.LensMake, "lens_model": metadata.LensModel,
		"lens_serial_number": metadata.LensSerialNumber, "flash_make": metadata.FlashMake, "flash_model": metadata.FlashModel,
		"flash_serial_number": metadata.FlashSerialNumber, "aperture_f_number": metadata.ApertureFNumber,
		"maximum_aperture_f_number": metadata.MaximumApertureFNumber, "exposure_time_seconds": metadata.ExposureTimeSeconds,
		"iso_speed": metadata.ISOSpeed, "exposure_compensation_ev": metadata.ExposureCompensationEV,
		"exposure_program": metadata.ExposureProgram, "flash_fired": metadata.FlashFired,
		"flash_mode": metadata.FlashMode, "flash_energy_bcps": metadata.FlashEnergyBCPS,
		"focal_length_mm": metadata.FocalLengthMM, "focal_length_35mm_mm": metadata.FocalLength35mmMM,
		"subject_distance_m": metadata.SubjectDistanceM, "metering_mode": metadata.MeteringMode,
		"light_source": metadata.LightSource, "white_balance": metadata.WhiteBalance,
		"contrast": metadata.Contrast, "brightness_ev": metadata.BrightnessEV, "saturation": metadata.Saturation,
		"sharpness": metadata.Sharpness, "digital_zoom_ratio": metadata.DigitalZoomRatio,
		"compression_bits_per_pixel": metadata.CompressionBitsPerPixel,
		"exif_version":               metadata.EXIFVersion, "gps_latitude": metadata.GPSLatitude,
		"gps_longitude": metadata.GPSLongitude, "gps_altitude_m": metadata.GPSAltitudeM,
		"gps_timestamp": metadata.GPSTimestamp, "image_metadata_extra": extra,
	}
}

// Feedback 映射用户反馈记录；图片只保存 storekey，文件内容仍由独立文件存储管理。
type Feedback struct {
	BaseModel
	UserID    *uuid.UUID     `gorm:"column:user_id;type:uuid;index"`
	Content   string         `gorm:"column:content;type:text;not null;default:''"`
	ImageKeys pq.StringArray `gorm:"column:image_keys;type:text[];not null;default:ARRAY[]::text[]"`
	// Status 仅记录后台处理状态；新提交统一以 new 状态进入反馈中心。
	Status string `gorm:"column:feedback_status;type:varchar(16);not null;default:'new'"`
}

// TableName 固定反馈模型使用与版本化建表 SQL 相同的表名。
func (Feedback) TableName() string { return "feedbacks" }

// ConsoleUser 是后台列表查询的最小投影，不承载密码哈希、数据库 ID 或内部 user_id。
type ConsoleUser struct {
	BusinessID uuid.UUID `gorm:"column:business_id"`
	Nickname   string    `gorm:"column:nickname"`
	Phone      string    `gorm:"column:phone"`
	IsAdmin    bool      `gorm:"column:is_admin"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

// ConsoleInspiration 是全站只读图库的最小投影，不包含私有编辑文档 data。
type ConsoleInspiration struct {
	BusinessID    uuid.UUID      `gorm:"column:business_id"`
	UserID        uuid.UUID      `gorm:"column:user_id"`
	OwnerNickname string         `gorm:"column:owner_nickname"`
	TagIDs        pq.StringArray `gorm:"column:tag_ids"`
	ImageKey      string         `gorm:"column:image_key"`
	FileName      string         `gorm:"column:file_name"`
	CreatedAt     time.Time      `gorm:"column:created_at"`
	UpdatedAt     time.Time      `gorm:"column:updated_at"`
}

// ConsoleFeedback 是反馈管理页使用的投影；仅包含处理所需字段及昵称，不带手机号等用户资料。
type ConsoleFeedback struct {
	BusinessID uuid.UUID      `gorm:"column:business_id"`
	UserID     *uuid.UUID     `gorm:"column:user_id"`
	Nickname   string         `gorm:"column:nickname"`
	Content    string         `gorm:"column:content"`
	ImageKeys  pq.StringArray `gorm:"column:image_keys"`
	Status     string         `gorm:"column:feedback_status"`
	CreatedAt  time.Time      `gorm:"column:created_at"`
	UpdatedAt  time.Time      `gorm:"column:updated_at"`
}

// FeedbackNotificationJob 保存反馈提交通知的持久化后台任务；反馈正文仍只从 feedbacks 表读取。
type FeedbackNotificationJob struct {
	BaseModel
	FeedbackBusinessID uuid.UUID  `gorm:"column:feedback_business_id;type:uuid;not null;uniqueIndex"`
	Status             string     `gorm:"column:job_status;type:varchar(16);not null;default:'pending'"`
	AttemptCount       int        `gorm:"column:attempt_count;type:smallint;not null;default:0"`
	NextAttemptAt      time.Time  `gorm:"column:next_attempt_at;type:timestamptz;not null"`
	LastAttemptAt      *time.Time `gorm:"column:last_attempt_at;type:timestamptz"`
	CompletedAt        *time.Time `gorm:"column:completed_at;type:timestamptz"`
	LastErrorCode      string     `gorm:"column:last_error_code;type:varchar(64);not null;default:''"`
}

// TableName 固定反馈通知任务模型使用与版本化 SQL 迁移相同的表名。
func (FeedbackNotificationJob) TableName() string { return "feedback_notification_jobs" }

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

// Notification 保存面向单个用户的站内业务通知，事件载荷仅存放客户端展示所需的非敏感参数。
type Notification struct {
	BaseModel
	UserID             uuid.UUID  `gorm:"column:user_id;type:uuid;not null;index"`
	EventType          string     `gorm:"column:event_type;type:varchar(64);not null"`
	EventKey           string     `gorm:"column:event_key;type:varchar(180);not null"`
	ResourceType       string     `gorm:"column:resource_type;type:varchar(32);not null;default:''"`
	ResourceBusinessID *uuid.UUID `gorm:"column:resource_business_id;type:uuid"`
	Payload            JSONB      `gorm:"column:payload;type:jsonb;not null;default:'{}'"`
	ReadAt             *time.Time `gorm:"column:read_at;type:timestamptz"`
}

// TableName 固定站内通知模型使用与版本化 SQL 迁移一致的表名。
func (Notification) TableName() string { return "notifications" }

// NoticeConfig 保存用户私有的群机器人 webhook 配置；网址和签名密钥不得进入 API 响应或日志。
type NoticeConfig struct {
	BaseModel
	UserID        uuid.UUID  `gorm:"column:user_id;type:uuid;not null;index"`
	Provider      string     `gorm:"column:provider;type:varchar(16);not null"`
	Name          string     `gorm:"column:config_name;type:varchar(80);not null"`
	WebhookURL    string     `gorm:"column:webhook_url;type:text;not null" json:"-"`
	SigningSecret string     `gorm:"column:signing_secret;type:text;not null;default:''" json:"-"`
	Enabled       bool       `gorm:"column:enabled;type:boolean;not null;default:true"`
	DeletedAt     *time.Time `gorm:"column:deleted_at;type:timestamptz"`
}

// TableName 固定用户通知配置模型使用与版本化 SQL 迁移一致的表名。
func (NoticeConfig) TableName() string { return "notice_config" }

// NoticeDelivery 保存可恢复的 webhook 投递任务、重试状态和安全错误分类，不保存远端响应正文。
type NoticeDelivery struct {
	BaseModel
	UserID                 uuid.UUID  `gorm:"column:user_id;type:uuid;not null;index"`
	DeliveryKind           string     `gorm:"column:delivery_kind;type:varchar(16);not null"`
	NotificationBusinessID *uuid.UUID `gorm:"column:notification_business_id;type:uuid"`
	NoticeConfigBusinessID uuid.UUID  `gorm:"column:notice_config_business_id;type:uuid;not null"`
	Provider               string     `gorm:"column:provider;type:varchar(16);not null"`
	ConfigName             string     `gorm:"column:config_name;type:varchar(80);not null"`
	EventType              string     `gorm:"column:event_type;type:varchar(64);not null"`
	Payload                JSONB      `gorm:"column:payload;type:jsonb;not null"`
	PartIndex              int        `gorm:"column:part_index;type:smallint;not null;default:1"`
	PartCount              int        `gorm:"column:part_count;type:smallint;not null;default:1"`
	Status                 string     `gorm:"column:delivery_status;type:varchar(16);not null;default:'pending'"`
	AttemptCount           int        `gorm:"column:attempt_count;type:smallint;not null;default:0"`
	NextAttemptAt          time.Time  `gorm:"column:next_attempt_at;type:timestamptz;not null"`
	LastAttemptAt          *time.Time `gorm:"column:last_attempt_at;type:timestamptz"`
	DeliveredAt            *time.Time `gorm:"column:delivered_at;type:timestamptz"`
	LastErrorCode          string     `gorm:"column:last_error_code;type:varchar(64);not null;default:''"`
}

// TableName 固定 webhook 投递模型使用与版本化 SQL 迁移一致的表名。
func (NoticeDelivery) TableName() string { return "notice_deliveries" }

// Plan 保存当前用户的一次性计划；删除使用软删除，避免已发通知失去可追溯资源。
type Plan struct {
	BaseModel
	UserID          uuid.UUID  `gorm:"column:user_id;type:uuid;not null;index"`
	Title           string     `gorm:"column:title;type:varchar(120);not null"`
	Description     string     `gorm:"column:description;type:text;not null;default:''"`
	StartsAt        time.Time  `gorm:"column:starts_at;type:timestamptz;not null;index"`
	CoverEmoji      string     `gorm:"column:cover_emoji;type:varchar(32);not null;default:''"`
	CoverImageKey   string     `gorm:"column:cover_image_key;type:text;not null;default:''"`
	Status          string     `gorm:"column:plan_status;type:varchar(16);not null;default:'active'"`
	CompletedAt     *time.Time `gorm:"column:completed_at;type:timestamptz"`
	DeletedAt       *time.Time `gorm:"column:deleted_at;type:timestamptz"`
	ReminderOffsets []int      `gorm:"-" json:"-"`
}

// TableName 固定计划模型使用与版本化 SQL 迁移一致的表名。
func (Plan) TableName() string { return "plans" }

// PlanReminder 保存单条相对计划开始时间的持久化提醒任务和配置状态。
type PlanReminder struct {
	BaseModel
	UserID         uuid.UUID  `gorm:"column:user_id;type:uuid;not null;index"`
	PlanBusinessID uuid.UUID  `gorm:"column:plan_business_id;type:uuid;not null;index"`
	LeadMinutes    int        `gorm:"column:lead_minutes;type:integer;not null"`
	RemindAt       time.Time  `gorm:"column:remind_at;type:timestamptz;not null"`
	Status         string     `gorm:"column:reminder_status;type:varchar(16);not null;default:'pending'"`
	Configured     bool       `gorm:"column:is_configured;type:boolean;not null;default:true"`
	SentAt         *time.Time `gorm:"column:sent_at;type:timestamptz"`
}

// TableName 固定计划提醒模型使用与版本化 SQL 迁移一致的表名。
func (PlanReminder) TableName() string { return "plan_reminders" }
