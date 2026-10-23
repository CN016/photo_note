package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

// ConsoleRepository 声明管理工作台的数据访问能力；角色判定和配置操作均由服务端执行。
type ConsoleRepository interface {
	IsAdministrator(context.Context, uuid.UUID) (bool, error)
	ListConsoleUsers(context.Context, string, int, int) ([]model.ConsoleUser, int64, error)
	SetUserAdministrator(context.Context, uuid.UUID, bool) error
	ListConsoleConfigs(context.Context) ([]model.AppConfig, error)
	FindConsoleConfig(context.Context, string) (model.AppConfig, error)
	CreateConsoleConfig(context.Context, *model.AppConfig) error
	UpdateConsoleConfig(context.Context, string, map[string]any) (model.AppConfig, error)
	ListConsoleInspirations(context.Context, string, string, int, int) ([]model.ConsoleInspiration, int64, error)
	ListConsoleFeedback(context.Context, string, int, int) ([]model.ConsoleFeedback, int64, error)
	GetConsoleFeedback(context.Context, uuid.UUID) (model.Feedback, error)
	UpdateConsoleFeedbackStatus(context.Context, uuid.UUID, string) error
	UpdateConsoleFeedbackReply(context.Context, uuid.UUID, string, []string, []string, string, model.JSONB, []string) error
}

var consoleConfigKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)

// ConsoleService 封装全站管理权限、分页查询和配置秘密脱敏规则。
type ConsoleService struct {
	repository          ConsoleRepository
	replyMaxTextChars   int
	feedbackReplyNotice *NoticeService
}

// NewConsoleService 注入 PostgreSQL repository；所有管理请求仍会单独读取当前角色。
func NewConsoleService(repository ConsoleRepository) *ConsoleService {
	return &ConsoleService{repository: repository, replyMaxTextChars: 2000}
}

// ConfigureFeedbackReplies 注入反馈回复沿用的文字上限与站内通知发布服务。
func (service *ConsoleService) ConfigureFeedbackReplies(maxTextChars int, notices *NoticeService) {
	if maxTextChars > 0 {
		service.replyMaxTextChars = maxTextChars
	}
	service.feedbackReplyNotice = notices
}

// IsAdministrator 每次从数据库读取当前权限，确保角色撤销会立即影响后续请求。
func (service *ConsoleService) IsAdministrator(ctx context.Context, userID uuid.UUID) (bool, error) {
	return service.repository.IsAdministrator(tenant.System(ctx), userID)
}

// ListUsers 按昵称或手机号检索用户并保留稳定分页。
func (service *ConsoleService) ListUsers(ctx context.Context, query string, page PageRequest) (Page[model.ConsoleUser], error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) > 100 {
		return Page[model.ConsoleUser]{}, fault.InvalidRequest("q 长度不能超过 100 个字符")
	}
	items, total, err := service.repository.ListConsoleUsers(tenant.System(ctx), query, page.Offset, page.PageSize)
	if err != nil {
		return Page[model.ConsoleUser]{}, err
	}
	return Page[model.ConsoleUser]{Items: items, Page: page.Page, PageSize: page.PageSize, Total: total}, nil
}

// SetUserAdministrator 设置目标用户角色；保留最后一位管理员的规则由事务 repository 保证。
func (service *ConsoleService) SetUserAdministrator(ctx context.Context, userID uuid.UUID, isAdmin bool) error {
	return service.repository.SetUserAdministrator(tenant.System(ctx), userID, isAdmin)
}

// ConsoleConfig 是管理页可读取的配置项；秘密值只通过 IsConfigured 表示是否存在。
type ConsoleConfig struct {
	BusinessID   uuid.UUID `json:"business_id"`
	Key          string    `json:"key"`
	Description  string    `json:"description"`
	Value        *string   `json:"value,omitempty"`
	IsPublic     bool      `json:"is_public"`
	IsSecret     bool      `json:"is_secret"`
	IsConfigured bool      `json:"is_configured"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ListConfigs 返回配置描述；秘密配置值只输出已配置状态，公开标志仍可见。
func (service *ConsoleService) ListConfigs(ctx context.Context) ([]ConsoleConfig, error) {
	rows, err := service.repository.ListConsoleConfigs(tenant.System(ctx))
	if err != nil {
		return nil, err
	}
	items := make([]ConsoleConfig, 0, len(rows))
	for _, row := range rows {
		items = append(items, consoleConfigDTO(row))
	}
	return items, nil
}

// ConsoleConfigInput 描述新增配置时允许管理者设置的业务字段。
type ConsoleConfigInput struct {
	Key         string
	Value       string
	Description string
	IsPublic    bool
	IsSecret    bool
}

// CreateConfig 校验配置 key、说明长度和公开/秘密冲突后创建新项。
func (service *ConsoleService) CreateConfig(ctx context.Context, input ConsoleConfigInput) (ConsoleConfig, error) {
	key := strings.TrimSpace(input.Key)
	description := strings.TrimSpace(input.Description)
	if !consoleConfigKeyPattern.MatchString(key) {
		return ConsoleConfig{}, fault.InvalidField("key", "配置键只能由小写字母、数字、点、下划线和连字符组成，且以字母开头")
	}
	if utf8.RuneCountInString(description) < 1 || utf8.RuneCountInString(description) > 500 {
		return ConsoleConfig{}, fault.InvalidField("description", "配置说明长度必须为 1 至 500 个字符")
	}
	if len(input.Value) > 100000 {
		return ConsoleConfig{}, fault.InvalidField("value", "配置值不能超过 100000 个字节")
	}
	if input.IsSecret && input.IsPublic {
		return ConsoleConfig{}, fault.InvalidField("is_public", "秘密配置不能设为公开")
	}
	row := model.AppConfig{
		BaseModel: model.BaseModel{BusinessID: uuid.New()}, ConfigKey: key, ConfigValue: input.Value,
		Description: description, IsPublic: input.IsPublic, IsSecret: input.IsSecret,
	}
	if err := service.repository.CreateConsoleConfig(tenant.System(ctx), &row); err != nil {
		return ConsoleConfig{}, err
	}
	return consoleConfigDTO(row), nil
}

// ConsoleConfigPatch 保留字段存在性，以区分未修改、替换和显式清除秘密值。
type ConsoleConfigPatch struct {
	ValueSet       bool
	Value          string
	ClearValue     bool
	DescriptionSet bool
	Description    string
	IsPublicSet    bool
	IsPublic       bool
}

// UpdateConfig 更新配置项；秘密值空白表示保留，只有 clear_value=true 才清除。
func (service *ConsoleService) UpdateConfig(ctx context.Context, key string, patch ConsoleConfigPatch) (ConsoleConfig, error) {
	if !consoleConfigKeyPattern.MatchString(key) {
		return ConsoleConfig{}, fault.InvalidField("key", "配置键格式无效")
	}
	if !patch.ValueSet && !patch.ClearValue && !patch.DescriptionSet && !patch.IsPublicSet {
		return ConsoleConfig{}, fault.InvalidRequest("至少提供一个需要更新的字段")
	}
	current, err := service.repository.FindConsoleConfig(tenant.System(ctx), key)
	if err != nil {
		return ConsoleConfig{}, err
	}
	if patch.ValueSet && len(patch.Value) > 100000 {
		return ConsoleConfig{}, fault.InvalidField("value", "配置值不能超过 100000 个字节")
	}
	if patch.ClearValue && patch.ValueSet && patch.Value != "" {
		return ConsoleConfig{}, fault.InvalidRequest("clear_value=true 时 value 必须为空")
	}
	if patch.IsPublicSet && patch.IsPublic && current.IsSecret {
		return ConsoleConfig{}, fault.InvalidField("is_public", "秘密配置不能设为公开")
	}
	updates := make(map[string]any)
	if patch.ValueSet && (!current.IsSecret || strings.TrimSpace(patch.Value) != "") {
		updates["config_value"] = patch.Value
	}
	if patch.ClearValue {
		updates["config_value"] = ""
	}
	if patch.DescriptionSet {
		description := strings.TrimSpace(patch.Description)
		if utf8.RuneCountInString(description) < 1 || utf8.RuneCountInString(description) > 500 {
			return ConsoleConfig{}, fault.InvalidField("description", "配置说明长度必须为 1 至 500 个字符")
		}
		updates["description"] = description
	}
	if patch.IsPublicSet {
		updates["is_public"] = patch.IsPublic
	}
	if len(updates) == 0 {
		return consoleConfigDTO(current), nil
	}
	updated, err := service.repository.UpdateConsoleConfig(tenant.System(ctx), key, updates)
	if err != nil {
		return ConsoleConfig{}, err
	}
	return consoleConfigDTO(updated), nil
}

// consoleConfigDTO 在唯一的序列化边界隐藏秘密原值，避免任何处理器意外回传。
func consoleConfigDTO(row model.AppConfig) ConsoleConfig {
	result := ConsoleConfig{
		BusinessID: row.BusinessID, Key: row.ConfigKey, Description: row.Description,
		IsPublic: row.IsPublic, IsSecret: row.IsSecret,
		IsConfigured: strings.TrimSpace(row.ConfigValue) != "", UpdatedAt: row.UpdatedAt,
	}
	if !row.IsSecret {
		value := row.ConfigValue
		result.Value = &value
	}
	return result
}

// ListInspirations 只查询路由已授权的单一组织数据域；平台与组织管理不得省略范围。
func (service *ConsoleService) ListInspirations(ctx context.Context, query, sort string, page PageRequest) (Page[model.ConsoleInspiration], error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) > 200 {
		return Page[model.ConsoleInspiration]{}, fault.InvalidRequest("q 长度不能超过 200 个字符")
	}
	if sort == "" {
		sort = "created_desc"
	}
	if sort != "created_desc" && sort != "created_asc" && sort != "updated_desc" {
		return Page[model.ConsoleInspiration]{}, fault.InvalidRequest("sort 只支持 created_desc、created_asc 或 updated_desc")
	}
	items, total, err := service.repository.ListConsoleInspirations(ctx, query, sort, page.Offset, page.PageSize)
	if err != nil {
		return Page[model.ConsoleInspiration]{}, err
	}
	return Page[model.ConsoleInspiration]{Items: items, Page: page.Page, PageSize: page.PageSize, Total: total}, nil
}

// ListFeedback 按可选状态分页；空状态表示读取全部反馈。
func (service *ConsoleService) ListFeedback(ctx context.Context, status string, page PageRequest) (Page[model.ConsoleFeedback], error) {
	if status != "" && status != "new" && status != "processed" {
		return Page[model.ConsoleFeedback]{}, fault.InvalidRequest("status 只支持 new 或 processed")
	}
	items, total, err := service.repository.ListConsoleFeedback(tenant.System(ctx), status, page.Offset, page.PageSize)
	if err != nil {
		return Page[model.ConsoleFeedback]{}, err
	}
	return Page[model.ConsoleFeedback]{Items: items, Page: page.Page, PageSize: page.PageSize, Total: total}, nil
}

// UpdateFeedbackStatus 仅允许把反馈标记为新反馈或已处理，状态与回复内容独立保存。
func (service *ConsoleService) UpdateFeedbackStatus(ctx context.Context, feedbackID uuid.UUID, status string) error {
	if status != "new" && status != "processed" {
		return fault.InvalidField("status", "反馈状态只支持 new 或 processed")
	}
	return service.repository.UpdateConsoleFeedbackStatus(tenant.System(ctx), feedbackID, status)
}

// UpdateFeedbackReply 校验可保留图片并保存管理员回复；注册用户的非空回复由通知服务生成事务载荷。
func (service *ConsoleService) UpdateFeedbackReply(ctx context.Context, feedbackID uuid.UUID, content string, retainedKeys, newKeys []string) ([]string, error) {
	if !utf8.ValidString(content) {
		return nil, fault.InvalidField("reply_content", "回复文字必须使用有效的 UTF-8 编码")
	}
	content = strings.TrimSpace(content)
	if utf8.RuneCountInString(content) > service.replyMaxTextChars {
		return nil, fault.InvalidField("reply_content", "回复文字超过反馈文字允许长度")
	}
	if len(retainedKeys)+len(newKeys) > 5 {
		return nil, fault.InvalidField("images", "回复图片最多五张")
	}
	feedback, err := service.repository.GetConsoleFeedback(tenant.System(ctx), feedbackID)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]struct{}, len(feedback.ReplyImageKeys))
	for _, key := range feedback.ReplyImageKeys {
		existing[key] = struct{}{}
	}
	seen := make(map[string]struct{}, len(retainedKeys)+len(newKeys))
	for _, key := range retainedKeys {
		if _, ok := existing[key]; !ok {
			return nil, fault.InvalidField("kept_image_keys", "只能保留当前反馈已有的回复图片")
		}
		parsed, parseErr := filestore.ParseKey(key)
		if parseErr != nil || !parsed.Public {
			return nil, fault.InvalidField("kept_image_keys", "只能保留当前反馈已有的公开图片")
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, fault.InvalidField("kept_image_keys", "回复图片列表不能包含重复项目")
		}
		seen[key] = struct{}{}
	}
	for _, key := range newKeys {
		parsed, parseErr := filestore.ParseKey(key)
		if parseErr != nil || !parsed.Public {
			return nil, fault.InvalidField("images", "回复图片必须使用服务端生成的公开图片 key")
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, fault.InvalidField("images", "回复图片列表不能包含重复项目")
		}
		seen[key] = struct{}{}
	}
	imageKeys := append(append(make([]string, 0, len(seen)), retainedKeys...), newKeys...)
	shouldNotify := content != "" || len(imageKeys) > 0
	eventKey := ""
	var payload model.JSONB
	var parts []string
	if shouldNotify && feedback.UserID != nil {
		if service.feedbackReplyNotice == nil {
			return nil, fault.Unavailable(errors.New("反馈回复通知服务未配置"))
		}
		eventKey, payload, parts, err = service.feedbackReplyNotice.PrepareFeedbackReply(tenant.System(ctx), feedbackID, content, imageKeys)
		if err != nil {
			return nil, fault.Unavailable(err)
		}
	}
	if err := service.repository.UpdateConsoleFeedbackReply(tenant.System(ctx), feedbackID, content, retainedKeys, newKeys, eventKey, payload, parts); err != nil {
		return nil, err
	}
	return imageKeys, nil
}
