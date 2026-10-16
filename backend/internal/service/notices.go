package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
)

// NoticeRepository 抽象用户隔离的站内消息、私有渠道设置和可恢复 webhook outbox 持久化。
type NoticeRepository interface {
	CreateInspirationNotification(context.Context, uuid.UUID, uuid.UUID, string, int64, string, string, model.JSONB) (model.Notification, bool, error)
	CountNoticeConfigs(context.Context, uuid.UUID) (int64, error)
	CreateNoticeConfig(context.Context, *model.NoticeConfig, int) error
	ListNoticeConfigs(context.Context, uuid.UUID) ([]model.NoticeConfig, error)
	GetNoticeConfig(context.Context, uuid.UUID, uuid.UUID) (model.NoticeConfig, error)
	UpdateNoticeConfig(context.Context, uuid.UUID, uuid.UUID, map[string]any) (model.NoticeConfig, error)
	DeleteNoticeConfig(context.Context, uuid.UUID, uuid.UUID) error
	CreateNoticeTestDelivery(context.Context, uuid.UUID, uuid.UUID, model.JSONB) (model.NoticeDelivery, error)
	ListNotifications(context.Context, uuid.UUID, bool, int, int) ([]model.Notification, int64, int64, error)
	CountUnreadNotifications(context.Context, uuid.UUID) (int64, error)
	ListNotificationDeliveries(context.Context, uuid.UUID, []uuid.UUID) ([]model.NoticeDelivery, error)
	ReadNotification(context.Context, uuid.UUID, uuid.UUID) error
	ReadAllNotifications(context.Context, uuid.UUID) (int64, error)
	GetNoticeDelivery(context.Context, uuid.UUID, uuid.UUID) (model.NoticeDelivery, error)
	RetryNoticeDelivery(context.Context, uuid.UUID, uuid.UUID) (model.NoticeDelivery, error)
	ClaimNoticeDelivery(context.Context) (model.NoticeDelivery, model.NoticeConfig, bool, error)
	FinishNoticeDelivery(context.Context, uuid.UUID, int, bool, string) error
}

// noticeLimitReader 让 webhook 数量限制始终经统一配置服务读取，而不由业务模块直接查 app_configs。
type noticeLimitReader interface {
	CurrentNoticeWebhookLimit(context.Context) (int, error)
}

// NoticeService 管理站内消息、用户 webhook 配置和独立异步投递工作循环。
type NoticeService struct {
	repository          NoticeRepository
	limits              noticeLimitReader
	client              *http.Client
	errorLogs           *ErrorLogService
	feishuFeedbackImage *feishuFeedbackImageUploader
}

// NewNoticeService 创建带有限网络超时且拒绝自动跟随重定向的通知服务。
func NewNoticeService(repository NoticeRepository, limits noticeLimitReader) *NoticeService {
	service := &NoticeService{
		repository: repository,
		limits:     limits,
		client: &http.Client{
			Timeout: 12 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	if settings, ok := limits.(feishuFeedbackImageUploadSettingsReader); ok {
		service.feishuFeedbackImage = newFeishuFeedbackImageUploader(settings, service.client)
	}
	return service
}

// SetErrorLogService 接收通知 worker 自身的失败事件，避免后台错误只停留在容器标准输出。
func (service *NoticeService) SetErrorLogService(errorLogs *ErrorLogService) {
	service.errorLogs = errorLogs
}

// PublishInspirationCompressionResult 只发布指定当前版本的终态事件，旧代次或已删除灵感图不产生任何消息。
func (service *NoticeService) PublishInspirationCompressionResult(ctx context.Context, userID, inspirationID uuid.UUID, imageKey string, version int64, succeeded bool) error {
	state := "failed"
	eventType := "inspiration.compression.failed"
	message := "灵感图图片处理失败，请检查该灵感图并重新上传图片。"
	if succeeded {
		state = "succeeded"
		eventType = "inspiration.compression.succeeded"
		message = "灵感图图片处理已完成。"
	}
	eventKey := fmt.Sprintf("inspiration.compression:%s:%d", inspirationID, version)
	payload, err := json.Marshal(map[string]any{
		"result": state, "async_version": version, "message": message,
	})
	if err != nil {
		return err
	}
	_, _, err = service.repository.CreateInspirationNotification(ctx, userID, inspirationID, imageKey, version, eventType, eventKey, model.JSONB(payload))
	if fault.Is(err, 404, "not_found") {
		// 处理过程中图片已删除或进入了新代次，这个工作结果已经过期。
		return nil
	}
	return err
}

// PublishErrorLog 为系统配置指定的管理员创建站内消息，不把内部诊断写入管理员的外部 webhook。
func (service *NoticeService) PublishErrorLog(ctx context.Context, userID uuid.UUID, entry model.ErrorLog) error {
	payload, err := json.Marshal(map[string]any{
		"error_code":  entry.ErrorCode,
		"source_file": entry.SourceFile,
		"source_line": entry.SourceLine,
	})
	if err != nil {
		return err
	}
	eventKey := fmt.Sprintf("system.error_log.created:%s", entry.BusinessID)
	repository, ok := service.repository.(interface {
		CreateErrorLogNotification(context.Context, uuid.UUID, uuid.UUID, string, model.JSONB) error
	})
	if !ok {
		return errors.New("error log notification repository is not configured")
	}
	return repository.CreateErrorLogNotification(ctx, userID, entry.BusinessID, eventKey, model.JSONB(payload))
}

const feedbackWebhookPartLimit = 3800

// PublishFeedbackSubmitted 以稳定事件键创建反馈正文、来源资料和图片链接，并为启用的 webhook 配置生成安全分段。
func (service *NoticeService) PublishFeedbackSubmitted(ctx context.Context, userID, feedbackID uuid.UUID, reporter FeedbackReporter, content string, imageURLs []string) error {
	markdown, err := feedbackMarkdown(reporter, content, imageURLs)
	if err != nil {
		return err
	}
	parts, err := splitFeedbackMarkdown(markdown, feedbackWebhookPartLimit)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return errors.New("反馈通知内容为空")
	}
	if imageURLs == nil {
		imageURLs = []string{}
	}
	payload, err := json.Marshal(map[string]any{
		"title": "收到新的用户反馈", "text": content, "markdown": markdown,
		"image_urls": imageURLs, "reporter": reporter,
	})
	if err != nil {
		return err
	}
	repository, ok := service.repository.(interface {
		CreateFeedbackNotification(context.Context, uuid.UUID, uuid.UUID, string, model.JSONB, []string) error
	})
	if !ok {
		return errors.New("feedback notification repository is not configured")
	}
	eventKey := fmt.Sprintf("feedback.submitted:%s", feedbackID)
	return repository.CreateFeedbackNotification(ctx, userID, feedbackID, eventKey, model.JSONB(payload), parts)
}

// feedbackMarkdown 展示最少反馈人资料，将用户正文作为纯文本，并为服务端图片地址生成图片与显式查看链接。
func feedbackMarkdown(reporter FeedbackReporter, content string, imageURLs []string) (string, error) {
	sections := make([]string, 0, 2+len(imageURLs))
	reporterName := strings.TrimSpace(reporter.Nickname)
	if reporter.Guest {
		if reporterName == "" {
			reporterName = "游客"
		}
		sections = append(sections, "反馈人："+escapeFeedbackMarkdown(reporterName)+"（未登录）")
	} else {
		if reporterName == "" {
			reporterName = "用户"
		}
		reporterLine := "反馈人：" + escapeFeedbackMarkdown(reporterName)
		if reporter.Phone != "" {
			reporterLine += "（手机号：" + escapeFeedbackMarkdown(reporter.Phone) + "）"
		}
		sections = append(sections, reporterLine)
	}
	if content != "" {
		sections = append(sections, escapeFeedbackMarkdown(content))
	}
	for index, imageURL := range imageURLs {
		parsed, err := url.Parse(imageURL)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", errors.New("反馈图片地址无效")
		}
		sections = append(sections, fmt.Sprintf("![反馈图片%d](%s)\n[查看反馈图片%d](%s)", index+1, imageURL, index+1, imageURL))
	}
	return strings.Join(sections, "\n\n"), nil
}

// escapeFeedbackMarkdown 转义可能触发 Markdown、链接或机器人提及的输入字符，保持用户文字按纯文本展示。
func escapeFeedbackMarkdown(value string) string {
	var escaped strings.Builder
	escaped.Grow(len(value))
	for _, character := range value {
		if strings.ContainsRune("\\`*_{}[]()#+-.!|>~@<", character) {
			escaped.WriteByte('\\')
		}
		escaped.WriteRune(character)
	}
	return escaped.String()
}

// splitFeedbackMarkdown 按字节上限分段，逐 Unicode 字符复制并保持图片引用和 Markdown 转义对完整。
func splitFeedbackMarkdown(markdown string, maximumBytes int) ([]string, error) {
	if maximumBytes < 1 {
		return nil, errors.New("反馈通知分段大小无效")
	}
	if !utf8.ValidString(markdown) {
		return nil, errors.New("反馈通知包含无效 UTF-8 字符")
	}
	parts := make([]string, 0, len(markdown)/maximumBytes+1)
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			parts = append(parts, current.String())
			current.Reset()
		}
	}
	for offset := 0; offset < len(markdown); {
		unit := ""
		if strings.HasPrefix(markdown[offset:], "![反馈图片") || strings.HasPrefix(markdown[offset:], "[查看反馈图片") {
			end := strings.IndexByte(markdown[offset:], ')')
			if end < 0 {
				return nil, errors.New("反馈图片 Markdown 格式无效")
			}
			unit = markdown[offset : offset+end+1]
		} else if markdown[offset] == '\\' {
			// 用户正文经过转义后，反斜杠与其转义字符必须处于同一分段，避免分段后重新解释 Markdown。
			_, firstSize := utf8.DecodeRuneInString(markdown[offset:])
			unit = markdown[offset : offset+firstSize]
			if offset+firstSize < len(markdown) {
				_, nextSize := utf8.DecodeRuneInString(markdown[offset+firstSize:])
				unit = markdown[offset : offset+firstSize+nextSize]
			}
		} else {
			_, size := utf8.DecodeRuneInString(markdown[offset:])
			if size < 1 {
				return nil, errors.New("反馈通知包含无效 UTF-8 字符")
			}
			unit = markdown[offset : offset+size]
		}
		if len(unit) > maximumBytes {
			return nil, errors.New("单个反馈图片引用或链接超过 webhook 消息限制")
		}
		if current.Len()+len(unit) > maximumBytes {
			flush()
		}
		current.WriteString(unit)
		offset += len(unit)
	}
	flush()
	return parts, nil
}

// NoticeConfigInput 是新增 webhook 配置的业务输入，不包含用户 ID 或数据库自增 ID。
type NoticeConfigInput struct {
	Provider      string `json:"provider"`
	Name          string `json:"name"`
	WebhookURL    string `json:"webhook_url"`
	SigningSecret string `json:"signing_secret"`
}

// NoticeConfigPatch 描述设置页的局部变更；空 signing_secret 默认保留旧值，显式 clear 才会清除。
type NoticeConfigPatch struct {
	Provider           *string `json:"provider"`
	Name               *string `json:"name"`
	WebhookURL         *string `json:"webhook_url"`
	SigningSecret      *string `json:"signing_secret"`
	ClearSigningSecret bool    `json:"clear_signing_secret"`
	Enabled            *bool   `json:"enabled"`
}

// NoticeConfigView 仅返回地址主机的遮蔽形式和是否已设签名，不暴露 webhook 凭据。
type NoticeConfigView struct {
	BusinessID       string    `json:"business_id"`
	Provider         string    `json:"provider"`
	Name             string    `json:"name"`
	WebhookMasked    string    `json:"webhook_masked"`
	SecretConfigured bool      `json:"secret_configured"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// NoticeConfigList 是配置列表与动态系统上限的安全响应。
type NoticeConfigList struct {
	Items   []NoticeConfigView `json:"items"`
	Maximum int                `json:"maximum"`
	Count   int64              `json:"count"`
}

// NoticeDeliveryView 是可供消息中心展示、重试的投递状态，不含 webhook 地址或响应原文。
type NoticeDeliveryView struct {
	BusinessID    string    `json:"business_id"`
	Provider      string    `json:"provider"`
	ConfigName    string    `json:"config_name"`
	PartIndex     int       `json:"part_index"`
	PartCount     int       `json:"part_count"`
	Status        string    `json:"status"`
	AttemptCount  int       `json:"attempt_count"`
	LastErrorCode string    `json:"last_error_code"`
	CanRetry      bool      `json:"can_retry"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// NotificationView 包含站内消息安全载荷及该消息各 webhook 的投递结果。
type NotificationView struct {
	BusinessID         string               `json:"business_id"`
	EventType          string               `json:"event_type"`
	ResourceType       string               `json:"resource_type"`
	ResourceBusinessID *string              `json:"resource_business_id"`
	Payload            json.RawMessage      `json:"payload"`
	ReadAt             *time.Time           `json:"read_at"`
	CreatedAt          time.Time            `json:"created_at"`
	Deliveries         []NoticeDeliveryView `json:"deliveries"`
}

// NotificationPage 返回消息分页数据、未读角标数量及当前筛选的总数。
type NotificationPage struct {
	Items       []NotificationView `json:"items"`
	Page        int                `json:"page"`
	PageSize    int                `json:"page_size"`
	Total       int64              `json:"total"`
	UnreadCount int64              `json:"unread_count"`
}

// ListNotifications 校验分页范围并在服务端按当前用户生成消息列表。
func (service *NoticeService) ListNotifications(ctx context.Context, userID uuid.UUID, unreadOnly bool, page, pageSize int) (NotificationPage, error) {
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return NotificationPage{}, fault.InvalidRequest("page 必须为正数且 page_size 必须在 1 至 100 之间")
	}
	items, total, unread, err := service.repository.ListNotifications(ctx, userID, unreadOnly, page, pageSize)
	if err != nil {
		return NotificationPage{}, err
	}
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.BusinessID)
	}
	deliveries, err := service.repository.ListNotificationDeliveries(ctx, userID, ids)
	if err != nil {
		return NotificationPage{}, err
	}
	byNotification := make(map[uuid.UUID][]NoticeDeliveryView, len(items))
	for _, delivery := range deliveries {
		if delivery.NotificationBusinessID == nil {
			continue
		}
		byNotification[*delivery.NotificationBusinessID] = append(byNotification[*delivery.NotificationBusinessID], deliveryView(delivery))
	}
	views := make([]NotificationView, 0, len(items))
	for _, item := range items {
		var resourceID *string
		if item.ResourceBusinessID != nil {
			value := item.ResourceBusinessID.String()
			resourceID = &value
		}
		payload := json.RawMessage(item.Payload)
		if !json.Valid(payload) {
			payload = json.RawMessage(`{}`)
		}
		itemDeliveries := byNotification[item.BusinessID]
		if itemDeliveries == nil {
			// JSON null 会让前端无法遍历；没有配置 webhook 时接口仍统一返回空数组。
			itemDeliveries = []NoticeDeliveryView{}
		}
		views = append(views, NotificationView{
			BusinessID: item.BusinessID.String(), EventType: item.EventType, ResourceType: item.ResourceType,
			ResourceBusinessID: resourceID, Payload: payload, ReadAt: item.ReadAt, CreatedAt: item.CreatedAt,
			Deliveries: itemDeliveries,
		})
	}
	return NotificationPage{Items: views, Page: page, PageSize: pageSize, Total: total, UnreadCount: unread}, nil
}

// UnreadCount 返回当前用户的未读总数供导航角标轮询。
func (service *NoticeService) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	return service.repository.CountUnreadNotifications(ctx, userID)
}

// MarkRead 将单条通知标记为已读，并保持对重复请求的幂等性。
func (service *NoticeService) MarkRead(ctx context.Context, userID, notificationID uuid.UUID) error {
	return service.repository.ReadNotification(ctx, userID, notificationID)
}

// MarkAllRead 将当前用户所有未读消息标记为已读。
func (service *NoticeService) MarkAllRead(ctx context.Context, userID uuid.UUID) (int64, error) {
	return service.repository.ReadAllNotifications(ctx, userID)
}

// ListNoticeConfigs 查询用户 webhook 配置并读取最新总数上限，响应不会包含敏感地址。
func (service *NoticeService) ListNoticeConfigs(ctx context.Context, userID uuid.UUID) (NoticeConfigList, error) {
	maximum, err := service.limits.CurrentNoticeWebhookLimit(ctx)
	if err != nil {
		return NoticeConfigList{}, fault.Unavailable(err)
	}
	configs, err := service.repository.ListNoticeConfigs(ctx, userID)
	if err != nil {
		return NoticeConfigList{}, err
	}
	views := make([]NoticeConfigView, 0, len(configs))
	for _, config := range configs {
		views = append(views, noticeConfigView(config))
	}
	return NoticeConfigList{Items: views, Maximum: maximum, Count: int64(len(configs))}, nil
}

// CreateNoticeConfig 校验平台 URL、命名和动态数量上限后保存用户私有渠道。
func (service *NoticeService) CreateNoticeConfig(ctx context.Context, userID uuid.UUID, input NoticeConfigInput) (NoticeConfigView, error) {
	provider := strings.TrimSpace(strings.ToLower(input.Provider))
	name := strings.TrimSpace(input.Name)
	webhookURL := strings.TrimSpace(input.WebhookURL)
	if err := validateNoticeConfig(provider, name, webhookURL, input.SigningSecret); err != nil {
		return NoticeConfigView{}, err
	}
	maximum, err := service.limits.CurrentNoticeWebhookLimit(ctx)
	if err != nil {
		return NoticeConfigView{}, fault.Unavailable(err)
	}
	now := time.Now().UTC()
	config := model.NoticeConfig{
		BaseModel: model.BaseModel{BusinessID: uuid.New(), CreatedAt: now, UpdatedAt: now},
		UserID:    userID, Provider: provider, Name: name, WebhookURL: webhookURL,
		SigningSecret: input.SigningSecret, Enabled: true,
	}
	if err := service.repository.CreateNoticeConfig(ctx, &config, maximum); err != nil {
		return NoticeConfigView{}, err
	}
	return noticeConfigView(config), nil
}

// UpdateNoticeConfig 校验局部变更后仅修改当前用户配置；空白签名字段不覆盖已保存秘密。
func (service *NoticeService) UpdateNoticeConfig(ctx context.Context, userID, configID uuid.UUID, patch NoticeConfigPatch) (NoticeConfigView, error) {
	current, err := service.repository.GetNoticeConfig(ctx, userID, configID)
	if err != nil {
		return NoticeConfigView{}, err
	}
	provider := current.Provider
	if patch.Provider != nil {
		provider = strings.TrimSpace(strings.ToLower(*patch.Provider))
	}
	name := current.Name
	if patch.Name != nil {
		name = strings.TrimSpace(*patch.Name)
	}
	webhookURL := current.WebhookURL
	if patch.WebhookURL != nil && strings.TrimSpace(*patch.WebhookURL) != "" {
		webhookURL = strings.TrimSpace(*patch.WebhookURL)
	}
	secret := current.SigningSecret
	if patch.ClearSigningSecret && patch.SigningSecret != nil && strings.TrimSpace(*patch.SigningSecret) != "" {
		return NoticeConfigView{}, fault.InvalidField("signing_secret", "不能同时设置密钥并清除密钥")
	}
	if patch.ClearSigningSecret {
		secret = ""
	} else if patch.SigningSecret != nil && strings.TrimSpace(*patch.SigningSecret) != "" {
		secret = *patch.SigningSecret
	}
	if err := validateNoticeConfig(provider, name, webhookURL, secret); err != nil {
		return NoticeConfigView{}, err
	}
	updates := map[string]any{"provider": provider, "config_name": name, "webhook_url": webhookURL, "signing_secret": secret}
	if patch.Enabled != nil {
		updates["enabled"] = *patch.Enabled
	}
	updated, err := service.repository.UpdateNoticeConfig(ctx, userID, configID, updates)
	if err != nil {
		return NoticeConfigView{}, err
	}
	return noticeConfigView(updated), nil
}

// DeleteNoticeConfig 软删除本人的配置，保留历史 delivery 状态供消息审计。
func (service *NoticeService) DeleteNoticeConfig(ctx context.Context, userID, configID uuid.UUID) error {
	return service.repository.DeleteNoticeConfig(ctx, userID, configID)
}

// TestNoticeConfig 只持久化测试 outbox 并立即返回，网络请求交由后台 worker 执行。
func (service *NoticeService) TestNoticeConfig(ctx context.Context, userID, configID uuid.UUID) (NoticeDeliveryView, error) {
	payload, _ := json.Marshal(map[string]string{
		"title": "Photo Nini webhook 测试", "message": "webhook 配置已成功接收测试投递请求。",
	})
	delivery, err := service.repository.CreateNoticeTestDelivery(ctx, userID, configID, model.JSONB(payload))
	if err != nil {
		return NoticeDeliveryView{}, err
	}
	return deliveryView(delivery), nil
}

// GetNoticeDelivery 查询当前用户可见的投递结果。
func (service *NoticeService) GetNoticeDelivery(ctx context.Context, userID, deliveryID uuid.UUID) (NoticeDeliveryView, error) {
	delivery, err := service.repository.GetNoticeDelivery(ctx, userID, deliveryID)
	if err != nil {
		return NoticeDeliveryView{}, err
	}
	return deliveryView(delivery), nil
}

// RetryNoticeDelivery 将本人最终失败的任务重置为待投递，不在 HTTP 请求中执行外部请求。
func (service *NoticeService) RetryNoticeDelivery(ctx context.Context, userID, deliveryID uuid.UUID) (NoticeDeliveryView, error) {
	delivery, err := service.repository.RetryNoticeDelivery(ctx, userID, deliveryID)
	if err != nil {
		return NoticeDeliveryView{}, err
	}
	return deliveryView(delivery), nil
}

// RunDeliveryWorker 循环领取持久化任务；通知创建接口只写 outbox，不等待 webhook 服务响应。
func (service *NoticeService) RunDeliveryWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		delivery, config, claimed, err := service.repository.ClaimNoticeDelivery(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// 通知队列异常进入统一错误日志；未装配记录器时仍保留安全的 stdout 诊断。
			if service.errorLogs != nil {
				service.errorLogs.CaptureBackground("notice_worker_claim_failed", err)
			} else {
				log.Printf("notice_worker claim_failed error_type=%T", err)
			}
			claimed = false
		}
		if claimed {
			errorCode := service.deliver(ctx, delivery, config)
			finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			finishErr := service.repository.FinishNoticeDelivery(finishCtx, delivery.BusinessID, delivery.AttemptCount, errorCode == "", errorCode)
			cancel()
			if finishErr != nil {
				if service.errorLogs != nil {
					service.errorLogs.CaptureBackground("notice_worker_finish_failed", finishErr)
				} else {
					log.Printf("notice_worker finish_failed error_type=%T", finishErr)
				}
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// deliver 对目标机器人使用有界 HTTP 客户端，返回稳定错误码而不返回响应正文、地址或秘密。
func (service *NoticeService) deliver(parent context.Context, delivery model.NoticeDelivery, config model.NoticeConfig) string {
	requestTimeout := 12 * time.Second
	if config.Provider == "feishu" && delivery.EventType == "feedback.submitted" {
		// 飞书原生图片路径包含令牌获取、图片上传和 webhook 投递多个有界请求。
		requestTimeout = 90 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(parent, requestTimeout)
	defer cancel()
	if err := validateNoticeConfig(config.Provider, config.Name, config.WebhookURL, config.SigningSecret); err != nil {
		return "invalid_provider_request"
	}
	requestURL, body, err := providerRequest(config, delivery)
	if err != nil {
		return "invalid_provider_request"
	}
	if config.Provider == "feishu" && delivery.EventType == "feedback.submitted" && service.feishuFeedbackImage != nil {
		imageRequestURL, imageRequestBody, handled, errorCode := service.feishuFeedbackImage.prepare(requestCtx, config, delivery)
		if errorCode != "" {
			return errorCode
		}
		if handled {
			requestURL = imageRequestURL
			body = imageRequestBody
		}
	}
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return "invalid_provider_request"
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Accept", "application/json")
	response, err := service.client.Do(request)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return "timeout"
		}
		return "network_error"
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Sprintf("http_%d", response.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return "response_read_error"
	}
	if providerRejected(responseBody) {
		return "provider_rejected"
	}
	return ""
}

// validateNoticeConfig 只接受三家平台的标准 HTTPS webhook 地址，阻断任意主机、端口和重定向入口。
func validateNoticeConfig(provider, name, rawURL, secret string) error {
	if provider != "wecom" && provider != "dingtalk" && provider != "feishu" {
		return fault.InvalidField("provider", "只支持企业微信、钉钉和飞书")
	}
	if name == "" || len([]rune(name)) > 80 {
		return fault.InvalidField("name", "名称不能为空且不能超过 80 个字符")
	}
	if len(secret) > 512 || (secret != "" && strings.TrimSpace(secret) == "") {
		return fault.InvalidField("signing_secret", "签名密钥格式无效")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Hostname() == "" {
		return fault.InvalidField("webhook_url", "webhook 地址必须是有效的 HTTPS 平台地址")
	}
	if parsed.Port() != "" && parsed.Port() != "443" {
		return fault.InvalidField("webhook_url", "webhook 地址只允许 HTTPS 默认端口")
	}
	query := parsed.Query()
	switch provider {
	case "wecom":
		if strings.ToLower(parsed.Hostname()) != "qyapi.weixin.qq.com" || parsed.EscapedPath() != "/cgi-bin/webhook/send" || len(query) != 1 || len(query["key"]) != 1 || query.Get("key") == "" {
			return fault.InvalidField("webhook_url", "企业微信地址必须使用官方机器人 webhook 格式")
		}
		if secret != "" {
			return fault.InvalidField("signing_secret", "企业微信机器人不使用此签名密钥字段")
		}
	case "dingtalk":
		if strings.ToLower(parsed.Hostname()) != "oapi.dingtalk.com" || parsed.EscapedPath() != "/robot/send" || len(query) != 1 || len(query["access_token"]) != 1 || query.Get("access_token") == "" {
			return fault.InvalidField("webhook_url", "钉钉地址必须使用官方机器人 webhook 格式")
		}
	case "feishu":
		host := strings.ToLower(parsed.Hostname())
		prefix := "/open-apis/bot/v2/hook/"
		path := parsed.EscapedPath()
		token := strings.TrimPrefix(path, prefix)
		if (host != "open.feishu.cn" && host != "open.larksuite.com") || !strings.HasPrefix(path, prefix) || token == "" || strings.Contains(token, "/") || len(query) != 0 {
			return fault.InvalidField("webhook_url", "飞书地址必须使用官方自定义机器人 webhook 格式")
		}
	}
	return nil
}

// noticeConfigView 只输出域名占位形式，防止 query token 或飞书 path token 进入 API。
func noticeConfigView(config model.NoticeConfig) NoticeConfigView {
	masked := "https://••••"
	if parsed, err := url.Parse(config.WebhookURL); err == nil && parsed.Hostname() != "" {
		masked = "https://" + strings.ToLower(parsed.Hostname()) + "/••••"
	}
	return NoticeConfigView{
		BusinessID: config.BusinessID.String(), Provider: config.Provider, Name: config.Name,
		WebhookMasked: masked, SecretConfigured: config.SigningSecret != "", Enabled: config.Enabled,
		CreatedAt: config.CreatedAt, UpdatedAt: config.UpdatedAt,
	}
}

// deliveryView 将内部模型压缩为不含 webhook 地址、签名密钥、响应正文或自增 ID 的状态 DTO。
func deliveryView(delivery model.NoticeDelivery) NoticeDeliveryView {
	return NoticeDeliveryView{
		BusinessID: delivery.BusinessID.String(), Provider: delivery.Provider, ConfigName: delivery.ConfigName,
		PartIndex: delivery.PartIndex, PartCount: delivery.PartCount,
		Status: delivery.Status, AttemptCount: delivery.AttemptCount, LastErrorCode: delivery.LastErrorCode,
		CanRetry: delivery.Status == "failed", UpdatedAt: delivery.UpdatedAt,
	}
}

// providerRequest 生成平台各自接受的中文机器人消息格式及可选标准签名。
func providerRequest(config model.NoticeConfig, delivery model.NoticeDelivery) (string, []byte, error) {
	parsed, err := url.Parse(config.WebhookURL)
	if err != nil {
		return "", nil, err
	}
	title, message := noticeMessage(delivery)
	if config.Provider == "feishu" && delivery.EventType == "feedback.submitted" {
		// 飞书自定义机器人卡片的图片控件需要飞书 image_key；只配置 webhook 时提供外链入口。
		message = feishuFeedbackLinksOnly(message)
	}
	var payload map[string]any
	switch config.Provider {
	case "wecom":
		payload = map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": "### " + title + "\n> " + message}}
	case "dingtalk":
		payload = map[string]any{"msgtype": "markdown", "markdown": map[string]string{"title": title, "text": "### " + title + "\n\n" + message}}
		if config.SigningSecret != "" {
			timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
			signature := signHMAC(config.SigningSecret, timestamp+"\n"+config.SigningSecret)
			query := parsed.Query()
			query.Set("timestamp", timestamp)
			query.Set("sign", signature)
			parsed.RawQuery = query.Encode()
		}
	case "feishu":
		payload = map[string]any{"msg_type": "interactive", "card": map[string]any{
			"schema": "2.0",
			"config": map[string]any{"update_multi": true, "style": map[string]any{"text_size": map[string]any{"normal_v2": map[string]string{"default": "normal", "pc": "normal", "mobile": "heading"}}}},
			"body":   map[string]any{"direction": "vertical", "padding": "12px 12px 12px 12px", "elements": []map[string]string{{"tag": "markdown", "content": message, "text_align": "left", "text_size": "normal_v2", "margin": "0px 0px 0px 0px"}}},
			"header": map[string]any{"title": map[string]string{"tag": "plain_text", "content": title}, "subtitle": map[string]string{"tag": "plain_text", "content": ""}, "template": "blue", "padding": "12px 12px 12px 12px"},
		}}
	default:
		return "", nil, errors.New("unsupported provider")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, err
	}
	if config.Provider == "feishu" && config.SigningSecret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(timestamp+"\n"+config.SigningSecret))
		signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		var messagePayload map[string]any
		if err := json.Unmarshal(body, &messagePayload); err != nil {
			return "", nil, err
		}
		messagePayload["timestamp"] = timestamp
		messagePayload["sign"] = signature
		body, err = json.Marshal(messagePayload)
		if err != nil {
			return "", nil, err
		}
	}
	return parsed.String(), body, nil
}

// feishuFeedbackLinksOnly 在链接回退模式中移除不会被卡片 Markdown 转成图片控件的语法，并保留同一反馈的可点击链接。
func feishuFeedbackLinksOnly(markdown string) string {
	const generatedImagePrefix = "![反馈图片"
	var result strings.Builder
	for offset := 0; offset < len(markdown); {
		relative := strings.Index(markdown[offset:], generatedImagePrefix)
		if relative < 0 {
			result.WriteString(markdown[offset:])
			break
		}
		start := offset + relative
		result.WriteString(markdown[offset:start])
		end := strings.IndexByte(markdown[start:], ')')
		if end < 0 {
			result.WriteString(markdown[start:])
			break
		}
		offset = start + end + 1
	}
	return strings.ReplaceAll(result.String(), "\n\n\n[查看反馈图片", "\n\n[查看反馈图片")
}

// noticeMessage 仅从服务端生成的安全消息载荷选择固定中文描述，不把用户内容注入外发模板。
func noticeMessage(delivery model.NoticeDelivery) (string, string) {
	if delivery.EventType == "notice.webhook_test" {
		return "Photo Nini webhook 测试", "webhook 配置已成功接收测试投递请求。"
	}
	var payload struct {
		Message  string `json:"message"`
		Title    string `json:"title"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(delivery.Payload, &payload); err == nil {
		if delivery.EventType == "feedback.submitted" && payload.Markdown != "" {
			if payload.Title == "" {
				payload.Title = "收到新的用户反馈"
			}
			return payload.Title, payload.Markdown
		}
		if payload.Message != "" && (delivery.EventType == "inspiration.compression.succeeded" || delivery.EventType == "inspiration.compression.failed") {
			return "灵感图处理通知", payload.Message
		}
	}
	return "Photo Nini 通知", "你有一条新的业务通知，请打开 Photo Nini 消息中心查看。"
}

// providerRejected 只识别平台统一错误字段，不保留或回传响应正文。
func providerRejected(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	if !json.Valid(body) {
		return true
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return false
	}
	for _, key := range []string{"errcode", "code"} {
		if raw, ok := response[key]; ok {
			var code int64
			if err := json.Unmarshal(raw, &code); err == nil && code != 0 {
				return true
			}
			var text string
			if err := json.Unmarshal(raw, &text); err == nil && text != "" && text != "0" && !strings.EqualFold(text, "ok") {
				return true
			}
		}
	}
	return false
}

// signHMAC 生成钉钉机器人要求的 HMAC-SHA256 Base64 签名。
func signHMAC(secret, content string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(content))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
