package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/imagecompress"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

const (
	planDefaultReminderMinutes  = 60
	planMaximumTitleRunes       = 120
	planMaximumDescriptionRunes = 4000
	planMaximumReminderMinutes  = 525600
)

// PlanRepository 封装按用户隔离的计划 CRUD、提醒重排和数据库时间驱动的到期任务读取。
type PlanRepository interface {
	ListPlans(context.Context, uuid.UUID) ([]model.Plan, error)
	FindPlan(context.Context, uuid.UUID, uuid.UUID) (model.Plan, error)
	CreatePlan(context.Context, *model.Plan, []int) error
	UpdatePlan(context.Context, uuid.UUID, uuid.UUID, map[string]any, []int, bool) (model.Plan, error)
	CompletePlan(context.Context, uuid.UUID, uuid.UUID) (model.Plan, error)
	DeletePlan(context.Context, uuid.UUID, uuid.UUID) error
	DuePlanReminderIDs(context.Context, int) ([]uuid.UUID, error)
}

// PlanFileReader 仅提供经 Access Token 上传文件的读取能力，不创建文件归属或元数据记录。
type PlanFileReader interface {
	Open(string) (io.ReadCloser, int64, string, error)
}

// PlanCoverEmojiReader 通过统一运行配置服务读取公开 emoji 列表，计划业务不直接查询 app_configs。
type PlanCoverEmojiReader interface {
	ListPublicConfigValues(context.Context, []string) ([]PublicConfigValue, error)
}

// PlanService 负责计划字段校验、封面内容校验和每分钟执行的持久化提醒队列。
type PlanService struct {
	repository  PlanRepository
	files       PlanFileReader
	maxBytes    int64
	notices     *NoticeService
	coverEmojis PlanCoverEmojiReader
	errorLogs   *ErrorLogService
}

// NewPlanService 注入计划 repository、鉴权文件存储、公开封面配置和通知服务。
func NewPlanService(repository PlanRepository, files PlanFileReader, maxBytes int64, notices *NoticeService, coverEmojis PlanCoverEmojiReader) *PlanService {
	return &PlanService{repository: repository, files: files, maxBytes: maxBytes, notices: notices, coverEmojis: coverEmojis}
}

// SetErrorLogService 将后台提醒错误交给现有安全错误日志服务记录。
func (service *PlanService) SetErrorLogService(errorLogs *ErrorLogService) {
	service.errorLogs = errorLogs
}

// PlanFields 是创建计划时可写入的字段；提醒偏移为提前分钟数，空数组采用默认的一小时。
type PlanFields struct {
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	StartsAt        time.Time `json:"starts_at"`
	CoverEmoji      string    `json:"cover_emoji"`
	CoverImageKey   string    `json:"cover_image_key"`
	ReminderOffsets []int     `json:"reminder_offsets"`
}

// PlanPatch 只应用显式提供的字段；ReminderOffsets 为 nil 表示保持现有提醒。
type PlanPatch struct {
	Title           *string
	Description     *string
	StartsAt        *time.Time
	CoverEmoji      *string
	CoverImageKey   *string
	ReminderOffsets *[]int
}

// PlanView 是客户端可见的计划字段，不包含用户 UUID、自增 ID 或提醒任务数据库标识。
type PlanView struct {
	BusinessID      uuid.UUID  `json:"business_id"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	StartsAt        time.Time  `json:"starts_at"`
	CoverEmoji      string     `json:"cover_emoji"`
	CoverImageKey   string     `json:"cover_image_key"`
	ReminderOffsets []int      `json:"reminder_offsets"`
	Status          string     `json:"status"`
	CompletedAt     *time.Time `json:"completed_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// List 仅读取当前令牌用户拥有且未删除的计划。
func (service *PlanService) List(ctx context.Context, userID uuid.UUID) ([]PlanView, error) {
	items, err := service.repository.ListPlans(ctx, userID)
	if err != nil {
		return nil, err
	}
	views := make([]PlanView, 0, len(items))
	for _, item := range items {
		views = append(views, planView(item))
	}
	return views, nil
}

// Get 按用户 UUID 与计划业务 UUID 双重限定资源读取。
func (service *PlanService) Get(ctx context.Context, userID, planID uuid.UUID) (PlanView, error) {
	item, err := service.repository.FindPlan(ctx, userID, planID)
	if err != nil {
		return PlanView{}, err
	}
	return planView(item), nil
}

// Create 校验计划与封面后创建计划和默认或自定义提醒任务。
func (service *PlanService) Create(ctx context.Context, userID uuid.UUID, fields PlanFields) (PlanView, error) {
	clean, err := normalizePlanFields(fields)
	if err != nil {
		return PlanView{}, err
	}
	if err := service.validatePlanCover(ctx, clean.CoverEmoji, clean.CoverImageKey); err != nil {
		return PlanView{}, err
	}
	item := model.Plan{
		BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: userID,
		Title: clean.Title, Description: clean.Description, StartsAt: clean.StartsAt.UTC(),
		CoverEmoji: clean.CoverEmoji, CoverImageKey: clean.CoverImageKey, Status: "active",
	}
	if err := service.repository.CreatePlan(ctx, &item, clean.ReminderOffsets); err != nil {
		return PlanView{}, err
	}
	item.ReminderOffsets = clean.ReminderOffsets
	return planView(item), nil
}

// Update 合并显式 patch 字段，提醒或时间变化时事务重排所有尚未发送的提醒。
func (service *PlanService) Update(ctx context.Context, userID, planID uuid.UUID, patch PlanPatch) (PlanView, error) {
	current, err := service.repository.FindPlan(ctx, userID, planID)
	if err != nil {
		return PlanView{}, err
	}
	fields := PlanFields{
		Title: current.Title, Description: current.Description, StartsAt: current.StartsAt,
		CoverEmoji: current.CoverEmoji, CoverImageKey: current.CoverImageKey,
		ReminderOffsets: append([]int(nil), current.ReminderOffsets...),
	}
	updates := make(map[string]any)
	if patch.Title != nil {
		fields.Title = *patch.Title
		updates["title"] = *patch.Title
	}
	if patch.Description != nil {
		fields.Description = *patch.Description
		updates["description"] = *patch.Description
	}
	if patch.StartsAt != nil {
		fields.StartsAt = *patch.StartsAt
		updates["starts_at"] = patch.StartsAt.UTC()
	}
	if patch.CoverEmoji != nil {
		fields.CoverEmoji = *patch.CoverEmoji
		updates["cover_emoji"] = *patch.CoverEmoji
	}
	if patch.CoverImageKey != nil {
		fields.CoverImageKey = *patch.CoverImageKey
		updates["cover_image_key"] = *patch.CoverImageKey
	}
	if patch.ReminderOffsets != nil {
		fields.ReminderOffsets = *patch.ReminderOffsets
	}
	clean, err := normalizePlanFields(fields)
	if err != nil {
		return PlanView{}, err
	}
	if patch.Title != nil {
		updates["title"] = clean.Title
	}
	if patch.Description != nil {
		updates["description"] = clean.Description
	}
	if patch.CoverEmoji != nil {
		updates["cover_emoji"] = clean.CoverEmoji
	}
	if patch.CoverImageKey != nil {
		updates["cover_image_key"] = clean.CoverImageKey
	}
	coverChanged := clean.CoverEmoji != current.CoverEmoji || clean.CoverImageKey != current.CoverImageKey
	if coverChanged {
		if err := service.validatePlanCover(ctx, clean.CoverEmoji, clean.CoverImageKey); err != nil {
			return PlanView{}, err
		}
	}
	reschedule := (patch.StartsAt != nil && !clean.StartsAt.Equal(current.StartsAt)) ||
		(patch.ReminderOffsets != nil && !samePlanReminderOffsets(clean.ReminderOffsets, current.ReminderOffsets))
	if patch.ReminderOffsets != nil {
		updates["updated_at"] = time.Now().UTC()
	}
	updated, err := service.repository.UpdatePlan(ctx, userID, planID, updates, clean.ReminderOffsets, reschedule)
	if err != nil {
		return PlanView{}, err
	}
	return planView(updated), nil
}

// Complete 标记计划完成；store 在同一事务中取消所有未发送的提醒。
func (service *PlanService) Complete(ctx context.Context, userID, planID uuid.UUID) (PlanView, error) {
	item, err := service.repository.CompletePlan(ctx, userID, planID)
	if err != nil {
		return PlanView{}, err
	}
	return planView(item), nil
}

// Delete 软删除计划并在同一事务中取消所有未发送的提醒。
func (service *PlanService) Delete(ctx context.Context, userID, planID uuid.UUID) error {
	return service.repository.DeletePlan(ctx, userID, planID)
}

// RunReminderWorker 启动后先补查一次，再每分钟按数据库时间读取到期任务；失败任务保持 pending 供后续轮询重试。
func (service *PlanService) RunReminderWorker(ctx context.Context) {
	service.processDuePlanReminders(ctx)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			service.processDuePlanReminders(ctx)
		}
	}
}

// processDuePlanReminders 每批按业务 UUID 调用通知服务；通知事务失败时不改变任务状态。
func (service *PlanService) processDuePlanReminders(ctx context.Context) {
	// 仅调度器可全局发现任务；每个提醒事务再从持久化记录恢复组织作用域。
	ctx = tenant.System(ctx)
	if service.notices == nil {
		service.captureWorkerError(errors.New("计划提醒通知服务未初始化"))
		return
	}
	ids, err := service.repository.DuePlanReminderIDs(ctx, 100)
	if err != nil {
		service.captureWorkerError(err)
		return
	}
	for _, reminderID := range ids {
		if ctx.Err() != nil {
			return
		}
		if _, err := service.notices.PublishPlanReminder(ctx, reminderID); err != nil {
			service.captureWorkerError(err)
		}
	}
}

// captureWorkerError 只记录安全分类，避免将计划标题、正文、文件 key 或通知载荷写入后台日志。
func (service *PlanService) captureWorkerError(err error) {
	if service.errorLogs != nil {
		service.errorLogs.CaptureBackground("plan_reminder_worker_failed", err)
		return
	}
	code, _ := SafeErrorSummary(err)
	log.Printf("event=plan_reminder_worker_failed error_code=%q", code)
}

// validatePlanCover 验证用户上传图片对应的私有 storekey 和实际图片字节，拒绝公开文件 key 与伪造扩展名。
func (service *PlanService) validatePlanCover(ctx context.Context, emoji, imageKey string) error {
	if (emoji == "") == (imageKey == "") {
		return fault.InvalidRequest("封面必须且只能选择一个 emoji 或图片")
	}
	if emoji != "" {
		if utf8.RuneCountInString(emoji) > 8 || strings.ContainsAny(emoji, "\r\n") {
			return fault.InvalidField("cover_emoji", "封面 emoji 格式无效")
		}
		if service.coverEmojis != nil {
			values, err := service.coverEmojis.ListPublicConfigValues(ctx, []string{"plans.cover_emojis"})
			if err != nil {
				return fault.Unavailable(err)
			}
			var allowed []string
			for _, value := range values {
				if value.Key == "plans.cover_emojis" {
					if err := json.Unmarshal([]byte(value.Value), &allowed); err != nil {
						return fault.Unavailable(errors.New("计划封面 emoji 配置格式无效"))
					}
				}
			}
			for _, candidate := range allowed {
				if candidate == emoji {
					return nil
				}
			}
			return fault.InvalidField("cover_emoji", "封面 emoji 不在系统当前允许列表中")
		}
		return nil
	}
	parsed, err := filestore.ParseKey(imageKey)
	if err != nil || parsed.Public {
		return fault.InvalidField("cover_image_key", "封面图片必须来自已鉴权上传的文件")
	}
	if service.files == nil || service.maxBytes < 1 {
		return fault.Unavailable(errors.New("计划图片存储或大小配置未初始化"))
	}
	if err := authorizeFileReference(ctx, service.files, imageKey); err != nil {
		return err
	}
	reader, size, _, err := OpenFile(ctx, service.files, imageKey)
	if err != nil {
		if errors.Is(err, filestore.ErrFileNotFound) || errors.Is(err, filestore.ErrInvalidStoreKey) {
			return fault.InvalidField("cover_image_key", "封面图片不存在或格式无效")
		}
		return fault.Internal(err)
	}
	if reader == nil {
		return fault.Internal(errors.New("计划图片存储未返回内容流"))
	}
	defer reader.Close()
	if size < 1 || size > service.maxBytes {
		return fault.InvalidField("cover_image_key", "封面图片超过文件大小限制")
	}
	content, err := io.ReadAll(io.LimitReader(reader, service.maxBytes+1))
	if err != nil {
		return fault.Internal(err)
	}
	if int64(len(content)) > service.maxBytes || imagecompress.Validate(content) != nil {
		return fault.InvalidField("cover_image_key", "封面必须是有效的 JPEG、PNG、GIF 或 WebP 图片")
	}
	return nil
}

// normalizePlanFields 清理标题和描述，并统一默认提醒、提醒数量、偏移范围及封面字段约束。
func normalizePlanFields(fields PlanFields) (PlanFields, error) {
	fields.Title = strings.TrimSpace(fields.Title)
	fields.Description = strings.TrimSpace(fields.Description)
	fields.CoverEmoji = strings.TrimSpace(fields.CoverEmoji)
	fields.CoverImageKey = strings.TrimSpace(fields.CoverImageKey)
	if fields.Title == "" || utf8.RuneCountInString(fields.Title) > planMaximumTitleRunes {
		return PlanFields{}, fault.InvalidField("title", "计划标题不能为空且不能超过 120 个字符")
	}
	if utf8.RuneCountInString(fields.Description) > planMaximumDescriptionRunes {
		return PlanFields{}, fault.InvalidField("description", "计划描述不能超过 4000 个字符")
	}
	if fields.StartsAt.IsZero() {
		return PlanFields{}, fault.InvalidField("starts_at", "计划时间不能为空")
	}
	if len(fields.ReminderOffsets) == 0 {
		fields.ReminderOffsets = []int{planDefaultReminderMinutes}
	}
	if len(fields.ReminderOffsets) > 3 {
		return PlanFields{}, fault.InvalidField("reminder_offsets", "最多只能设置三条提醒")
	}
	seen := make(map[int]struct{}, len(fields.ReminderOffsets))
	for _, offset := range fields.ReminderOffsets {
		if offset < 1 || offset > planMaximumReminderMinutes {
			return PlanFields{}, fault.InvalidField("reminder_offsets", "提醒提前时间必须为 1 至 525600 分钟")
		}
		if _, exists := seen[offset]; exists {
			return PlanFields{}, fault.InvalidField("reminder_offsets", "提醒时间不能重复")
		}
		seen[offset] = struct{}{}
	}
	fields.ReminderOffsets = append([]int(nil), fields.ReminderOffsets...)
	sort.Ints(fields.ReminderOffsets)
	if (fields.CoverEmoji == "") == (fields.CoverImageKey == "") {
		return PlanFields{}, fault.InvalidRequest("封面必须且只能选择一个 emoji 或图片")
	}
	return fields, nil
}

// samePlanReminderOffsets 比较规范化后的有序提前分钟数组，避免只修改标题时重建提醒任务。
func samePlanReminderOffsets(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// planView 显式映射对外字段，确保 user_id、数据库 id 和提醒任务 ID 永远不会序列化。
func planView(item model.Plan) PlanView {
	offsets := append([]int(nil), item.ReminderOffsets...)
	if offsets == nil {
		offsets = []int{planDefaultReminderMinutes}
	}
	return PlanView{
		BusinessID: item.BusinessID, Title: item.Title, Description: item.Description,
		StartsAt: item.StartsAt, CoverEmoji: item.CoverEmoji, CoverImageKey: item.CoverImageKey,
		ReminderOffsets: offsets, Status: item.Status, CompletedAt: item.CompletedAt,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}
