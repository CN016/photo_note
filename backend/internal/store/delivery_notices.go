package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PendingDeliveryEvents 仅通知 worker 可跨空间扫描；随后逐条使用原始空间上下文处理。
func (s *PostgresStore) PendingDeliveryEvents(ctx context.Context) ([]model.DeliveryEvent, error) {
	events := []model.DeliveryEvent{}
	err := s.db.WithContext(tenant.System(ctx)).Where("notified = false AND action IN ?", []string{"selection_submitted", "reopen", "publish_final", "received", "export_completed", "export_failed"}).Order("created_at").Limit(30).Find(&events).Error
	return events, persistenceError(err)
}

// PublishDeliveryNotification 在同一事务锁定任务和事件，写站内消息与 webhook outbox 并标记事件完成。
// 若业务代次已变化，旧事件只标记处理，不把过期后台完成事件推送到新业务状态。
func (s *PostgresStore) PublishDeliveryNotification(ctx context.Context, id uuid.UUID) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	return persistenceError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var event model.DeliveryEvent
		if err := tx.Where("business_id = ? AND user_id = ?", id, scope.UserID).First(&event).Error; err != nil {
			return err
		}
		var task model.DeliveryTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("business_id = ? AND user_id = ?", event.TaskID, scope.UserID).First(&task).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("business_id = ? AND user_id = ?", id, scope.UserID).First(&event).Error; err != nil {
			return err
		}
		if event.Notified {
			return nil
		}
		if task.Version == event.TaskVersion && task.Status != "archived" && task.Status != "cancelled" {
			payload, _ := json.Marshal(map[string]string{"title": "交付任务状态已更新", "text": "请打开交付模块查看最新处理结果。"})
			notification := model.Notification{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: scope.UserID, EventType: "delivery." + event.Action, EventKey: fmt.Sprintf("delivery:%s", event.BusinessID), ResourceType: "delivery", ResourceBusinessID: &task.BusinessID, Payload: model.JSONB(payload)}
			result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "orgid"}, {Name: "user_id"}, {Name: "event_key"}}, DoNothing: true}).Create(&notification)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				var configs []model.NoticeConfig
				if err := tx.Where("user_id = ? AND enabled = true AND deleted_at IS NULL", scope.UserID).Find(&configs).Error; err != nil {
					return err
				}
				for _, config := range configs {
					delivery := model.NoticeDelivery{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: scope.UserID, DeliveryKind: "notification", NotificationBusinessID: &notification.BusinessID, NoticeConfigBusinessID: config.BusinessID, Provider: config.Provider, ConfigName: config.Name, EventType: notification.EventType, Payload: model.JSONB(payload), PartIndex: 1, PartCount: 1, Status: "pending", NextAttemptAt: time.Now()}
					if err := tx.Create(&delivery).Error; err != nil {
						return err
					}
				}
			}
		}
		return tx.Model(&model.DeliveryEvent{}).Where("business_id = ? AND user_id = ?", id, scope.UserID).Updates(map[string]any{"notified": true}).Error
	}))
}
