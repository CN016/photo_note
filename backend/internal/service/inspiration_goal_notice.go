package service

import (
	"context"
	"errors"

	"github.com/photo-nini/backend/internal/model"
)

// PublishInspirationGoalReminder 通过统一通知服务发布稳定每日事件，网络发送仍由既有 outbox worker 负责。
func (s *NoticeService) PublishInspirationGoalReminder(ctx context.Context, goal model.InspirationGoal) error {
	repo, ok := s.repository.(interface {
		PublishInspirationGoalNotification(context.Context, model.InspirationGoal) error
	})
	if !ok {
		return errors.New("灵感库目标通知存储未配置")
	}
	return repo.PublishInspirationGoalNotification(ctx, goal)
}
