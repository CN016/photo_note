package service

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

// FeedbackRepository 定义反馈写入所需的最小持久化能力。
type FeedbackRepository interface {
	CreateFeedbackAndNotificationJob(context.Context, *model.Feedback, *model.FeedbackNotificationJob) error
}

// FeedbackRateLimiter 以可信代理解析后的客户端地址限制公开反馈提交频率。
type FeedbackRateLimiter interface {
	CheckFeedbackLimit(context.Context, string, int) (bool, error)
}

// FeedbackService 持有经统一配置服务解析的反馈上限并完成限流和数据写入。
type FeedbackService struct {
	repository FeedbackRepository
	limiter    FeedbackRateLimiter
	maxChars   int
	maxImages  int
	perHour    int
}

// NewFeedbackService 注入数据库写入、Redis 计数和 app_configs 中的私有上限。
func NewFeedbackService(repository FeedbackRepository, limiter FeedbackRateLimiter, maxChars, maxImages, perHour int) *FeedbackService {
	return &FeedbackService{repository: repository, limiter: limiter, maxChars: maxChars, maxImages: maxImages, perHour: perHour}
}

// Validate 检查正文和图片数量，并要求两类反馈内容至少出现一项。
func (service *FeedbackService) Validate(content string, imageCount int) error {
	if !utf8.ValidString(content) {
		return fault.InvalidField("content", "反馈文字必须使用有效的 UTF-8 编码")
	}
	if utf8.RuneCountInString(content) > service.maxChars {
		return fault.InvalidField("content", "反馈文字超过允许长度")
	}
	if imageCount > service.maxImages {
		return fault.InvalidField("images", "反馈图片数量超过允许上限")
	}
	if strings.TrimSpace(content) == "" && imageCount == 0 {
		return fault.InvalidRequest("请填写反馈文字或至少上传一张图片")
	}
	return nil
}

// Reserve 按可信客户端 IP 原子占用一个反馈请求额度，Redis 不可用时不接收提交。
func (service *FeedbackService) Reserve(ctx context.Context, clientIP string) error {
	if strings.TrimSpace(clientIP) == "" {
		return fault.InvalidRequest("无法确认客户端网络地址")
	}
	allowed, err := service.limiter.CheckFeedbackLimit(ctx, clientIP, service.perHour)
	if err != nil {
		return err
	}
	if !allowed {
		return fault.New(http.StatusTooManyRequests, "rate_limited", "反馈提交次数过多，请稍后重试", nil, nil)
	}
	return nil
}

// Submit 以服务端令牌 claims 生成用户关联；游客 userID 为空，正文与图片键不会被写入日志。
func (service *FeedbackService) Submit(ctx context.Context, userID *uuid.UUID, content string, imageKeys []string) error {
	if err := service.Validate(content, len(imageKeys)); err != nil {
		return err
	}
	feedback := &model.Feedback{
		BaseModel: model.BaseModel{BusinessID: uuid.New(), OrgID: tenant.GlobalOrgID},
		UserID:    userID,
		Content:   strings.TrimSpace(content),
		ImageKeys: imageKeys,
		Status:    "new",
	}
	now := time.Now().UTC()
	job := &model.FeedbackNotificationJob{
		BaseModel:          model.BaseModel{BusinessID: uuid.New(), OrgID: tenant.GlobalOrgID},
		FeedbackBusinessID: feedback.BusinessID,
		Status:             "pending",
		NextAttemptAt:      now,
	}
	return service.repository.CreateFeedbackAndNotificationJob(ctx, feedback, job)
}
