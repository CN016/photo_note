package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/model"
)

// ErrFeedbackNotificationConfigInvalid 表示私有反馈通知配置值不符合业务约束。
var ErrFeedbackNotificationConfigInvalid = errors.New("反馈通知配置无效")

// FeedbackNotificationRepository 管理持久化任务，并读取接收用户和反馈人的最少资料。
type FeedbackNotificationRepository interface {
	ClaimFeedbackNotificationJob(context.Context) (model.FeedbackNotificationJob, model.Feedback, bool, error)
	CompleteFeedbackNotificationJob(context.Context, uuid.UUID, int, string, string) error
	FindUserBusinessIDByPhone(context.Context, string) (uuid.UUID, error)
	FindFeedbackReporter(context.Context, uuid.UUID) (string, string, error)
}

// FeedbackNotificationSettingsReader 从统一配置服务读取开关、专用接收手机号和允许路径前缀的 HTTPS 公共地址。
type FeedbackNotificationSettingsReader interface {
	FeedbackNotificationSettings(context.Context) (bool, string, string, error)
}

// FeedbackNotificationPublisher 将反馈安全载荷写入站内消息，并为目标用户创建 webhook outbox。
type FeedbackNotificationPublisher interface {
	PublishFeedbackSubmitted(context.Context, uuid.UUID, uuid.UUID, FeedbackReporter, string, []string) error
}

// FeedbackReporter 只向指定反馈接收人展示昵称、手机号或游客状态，不暴露账户业务 UUID。
type FeedbackReporter struct {
	Nickname string `json:"nickname"`
	Phone    string `json:"phone,omitempty"`
	Guest    bool   `json:"guest"`
}

// FeedbackNotificationService 持续领取已持久化的反馈通知任务；任何失败都不会回滚已保存反馈。
type FeedbackNotificationService struct {
	repository FeedbackNotificationRepository
	settings   FeedbackNotificationSettingsReader
	notices    FeedbackNotificationPublisher
	errorLogs  *ErrorLogService
}

// NewFeedbackNotificationService 装配反馈通知任务 worker，接收用户资料和通知发布能力。
func NewFeedbackNotificationService(repository FeedbackNotificationRepository, settings FeedbackNotificationSettingsReader, notices FeedbackNotificationPublisher) *FeedbackNotificationService {
	return &FeedbackNotificationService{repository: repository, settings: settings, notices: notices}
}

// SetErrorLogService 为 worker 领取或完成任务的基础设施故障接入脱敏后台错误记录。
func (service *FeedbackNotificationService) SetErrorLogService(errorLogs *ErrorLogService) {
	service.errorLogs = errorLogs
}

// Run 持续领取任务；数据库记录是唯一队列来源，进程重启后可继续处理未完成任务。
func (service *FeedbackNotificationService) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		claimCtx, cancelClaim := context.WithTimeout(ctx, 5*time.Second)
		job, feedback, claimed, err := service.repository.ClaimFeedbackNotificationJob(claimCtx)
		cancelClaim()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			service.captureWorkerError("feedback_notification_claim_failed")
			claimed = false
		}
		if claimed {
			processCtx, cancelProcess := context.WithTimeout(ctx, 20*time.Second)
			outcome, errorCode := service.process(processCtx, feedback)
			cancelProcess()
			finishCtx, cancelFinish := context.WithTimeout(context.Background(), 5*time.Second)
			finishErr := service.repository.CompleteFeedbackNotificationJob(finishCtx, job.BusinessID, job.AttemptCount, outcome, errorCode)
			cancelFinish()
			if finishErr != nil {
				service.captureWorkerError("feedback_notification_finish_failed")
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

// process 读取独立通知设置、定位已有用户，并在全部关键输入通过校验后发布幂等站内通知。
func (service *FeedbackNotificationService) process(ctx context.Context, feedback model.Feedback) (string, string) {
	if service.settings == nil || service.notices == nil || service.repository == nil {
		return "failed", "service_unavailable"
	}
	enabled, phone, publicBaseURL, err := service.settings.FeedbackNotificationSettings(ctx)
	if err != nil {
		if errors.Is(err, ErrFeedbackNotificationConfigInvalid) {
			return "failed", "configuration_invalid"
		}
		return "retry", "configuration_unavailable"
	}
	if !enabled {
		return "skipped", "notifications_disabled"
	}
	userID, err := service.repository.FindUserBusinessIDByPhone(ctx, phone)
	if err != nil {
		if fault.Is(err, 404, "not_found") {
			return "failed", "recipient_not_found"
		}
		return "retry", "recipient_lookup_failed"
	}
	reporter := FeedbackReporter{Nickname: "游客", Guest: true}
	if feedback.UserID != nil {
		nickname, reporterPhone, reporterErr := service.repository.FindFeedbackReporter(ctx, *feedback.UserID)
		if reporterErr != nil {
			if fault.Is(reporterErr, 404, "not_found") {
				reporter = FeedbackReporter{Nickname: "账号已不存在"}
			} else {
				return "retry", "feedback_reporter_lookup_failed"
			}
		} else {
			reporter = FeedbackReporter{Nickname: strings.TrimSpace(nickname), Phone: reporterPhone}
			if reporter.Nickname == "" {
				reporter.Nickname = "用户"
			}
		}
	}
	imageURLs := make([]string, 0, len(feedback.ImageKeys))
	for _, imageKey := range feedback.ImageKeys {
		key := string(imageKey)
		parsed, parseErr := filestore.ParseKey(key)
		if parseErr != nil || !parsed.Public {
			return "failed", "public_image_key_invalid"
		}
		imageURLs = append(imageURLs, strings.TrimRight(publicBaseURL, "/")+"/api/v1/public-feedback-images/"+url.PathEscape(key))
	}
	if err := service.notices.PublishFeedbackSubmitted(ctx, userID, feedback.BusinessID, reporter, feedback.Content, imageURLs); err != nil {
		return "retry", "notification_publish_failed"
	}
	return "succeeded", ""
}

// captureWorkerError 只记录固定错误摘要，不传递配置值、手机号、反馈文字或图片 key。
func (service *FeedbackNotificationService) captureWorkerError(code string) {
	if service.errorLogs != nil {
		service.errorLogs.CaptureBackground(code, errors.New("反馈通知任务基础设施操作失败"))
		return
	}
	log.Printf("event=%s", code)
}

// feedbackNotificationSettingsError 将输入错误转换为可安全持久化的固定配置分类。
func feedbackNotificationSettingsError() error {
	return fmt.Errorf("%w", ErrFeedbackNotificationConfigInvalid)
}
