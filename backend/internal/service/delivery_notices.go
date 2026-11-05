package service

import (
	"context"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"time"
)

// PublishDeliveryEvent 使用持久化业务事件创建通知，外部发送继续由 NoticeService 独立 worker 执行。
func (s *NoticeService) PublishDeliveryEvent(ctx context.Context, event uuid.UUID) error {
	repository, ok := s.repository.(interface {
		PublishDeliveryNotification(context.Context, uuid.UUID) error
	})
	if !ok {
		return fault.Unavailable(nil)
	}
	return repository.PublishDeliveryNotification(ctx, event)
}

// RunNoticeWorker 从交付持久化事件恢复发布，不在业务 HTTP 请求里等待外部 webhook。
func (s *DeliveryService) RunNoticeWorker(ctx context.Context, notices *NoticeService) {
	repository, ok := s.repository.(interface {
		PendingDeliveryEvents(context.Context) ([]model.DeliveryEvent, error)
	})
	if !ok || notices == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		events, err := repository.PendingDeliveryEvents(ctx)
		if err != nil {
			continue
		}
		for _, event := range events {
			scoped := tenant.WithScope(ctx, tenant.Scope{OrgID: event.OrgID, UserID: event.UserID})
			_ = notices.PublishDeliveryEvent(scoped, event.BusinessID)
		}
	}
}
