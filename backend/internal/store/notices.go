package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreateInspirationNotification 在事务内核对灵感图当前代次、插入幂等站内消息并为启用渠道创建 outbox 任务。
func (store *PostgresStore) CreateInspirationNotification(ctx context.Context, userID, inspirationID uuid.UUID, imageKey string, asyncVersion int64, eventType, eventKey string, payload model.JSONB) (model.Notification, bool, error) {
	var notification model.Notification
	created := false
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.Inspiration
		if err := tx.Select("business_id").Where("user_id = ? AND business_id = ? AND image_key = ? AND async_version = ?", userID, inspirationID, imageKey, asyncVersion).First(&current).Error; err != nil {
			return persistenceError(err)
		}
		notification = model.Notification{
			BaseModel: model.BaseModel{BusinessID: uuid.New(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
			UserID:    userID, EventType: eventType, EventKey: eventKey, ResourceType: "inspiration",
			ResourceBusinessID: &inspirationID, Payload: payload,
		}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "event_key"}}, DoNothing: true}).Create(&notification)
		if result.Error != nil {
			return persistenceError(result.Error)
		}
		if result.RowsAffected == 0 {
			created = false
			return nil
		}
		created = true
		var configs []model.NoticeConfig
		if err := tx.Where("user_id = ? AND enabled = TRUE AND deleted_at IS NULL", userID).Order("created_at ASC").Find(&configs).Error; err != nil {
			return persistenceError(err)
		}
		now := time.Now().UTC()
		for _, config := range configs {
			delivery := model.NoticeDelivery{
				BaseModel: model.BaseModel{BusinessID: uuid.New(), CreatedAt: now, UpdatedAt: now},
				UserID:    userID, DeliveryKind: "notification", NotificationBusinessID: &notification.BusinessID,
				NoticeConfigBusinessID: config.BusinessID, Provider: config.Provider, ConfigName: config.Name,
				EventType: eventType, Payload: append(model.JSONB(nil), payload...), PartIndex: 1, PartCount: 1, Status: "pending", NextAttemptAt: now,
			}
			if err := tx.Create(&delivery).Error; err != nil {
				return persistenceError(err)
			}
		}
		return nil
	})
	if err != nil {
		return model.Notification{}, false, err
	}
	if !created {
		if err := store.db.WithContext(ctx).Where("user_id = ? AND event_key = ?", userID, eventKey).First(&notification).Error; err != nil {
			return model.Notification{}, false, persistenceError(err)
		}
	}
	return notification, created, nil
}

// CreateFeedbackNotification 在事务中按稳定事件键创建站内消息，并为每个启用渠道和 Markdown 分段创建独立 outbox。
func (store *PostgresStore) CreateFeedbackNotification(ctx context.Context, userID, feedbackID uuid.UUID, eventKey string, payload model.JSONB, parts []string) error {
	if len(parts) == 0 || !json.Valid(payload) {
		return errors.New("反馈通知载荷无效")
	}
	return persistenceError(store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		notification := model.Notification{
			BaseModel:          model.BaseModel{BusinessID: uuid.New(), CreatedAt: now, UpdatedAt: now},
			UserID:             userID,
			EventType:          "feedback.submitted",
			EventKey:           eventKey,
			ResourceType:       "feedback",
			ResourceBusinessID: &feedbackID,
			Payload:            payload,
		}
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "event_key"}},
			DoNothing: true,
		}).Create(&notification)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		var configs []model.NoticeConfig
		if err := tx.Where("user_id = ? AND enabled = TRUE AND deleted_at IS NULL", userID).Order("created_at ASC").Find(&configs).Error; err != nil {
			return err
		}
		for _, config := range configs {
			for index, part := range parts {
				partIndex := index + 1
				partTitle := fmt.Sprintf("收到新的用户反馈（%d/%d）", partIndex, len(parts))
				partPayload, err := json.Marshal(map[string]any{
					"title": partTitle, "markdown": part, "part_index": partIndex, "part_count": len(parts),
				})
				if err != nil {
					return err
				}
				delivery := model.NoticeDelivery{
					BaseModel: model.BaseModel{BusinessID: uuid.New(), CreatedAt: now, UpdatedAt: now},
					UserID:    userID, DeliveryKind: "notification", NotificationBusinessID: &notification.BusinessID,
					NoticeConfigBusinessID: config.BusinessID, Provider: config.Provider, ConfigName: config.Name,
					EventType: "feedback.submitted", Payload: model.JSONB(partPayload), PartIndex: partIndex, PartCount: len(parts),
					Status: "pending", NextAttemptAt: now,
				}
				if err := tx.Create(&delivery).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}))
}

// CountNoticeConfigs 统计用户未软删除的配置；停用配置也计入可配置数量上限。
func (store *PostgresStore) CountNoticeConfigs(ctx context.Context, userID uuid.UUID) (int64, error) {
	var count int64
	err := store.db.WithContext(ctx).Model(&model.NoticeConfig{}).Where("user_id = ? AND deleted_at IS NULL", userID).Count(&count).Error
	return count, persistenceError(err)
}

// CreateNoticeConfig 串行锁定用户行后检查上限，避免并发创建跨过系统配置的数量限制。
func (store *PostgresStore) CreateNoticeConfig(ctx context.Context, config *model.NoticeConfig, maximum int) error {
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("business_id").Where("business_id = ?", config.UserID).First(&user).Error; err != nil {
			return persistenceError(err)
		}
		var count int64
		if err := tx.Model(&model.NoticeConfig{}).Where("user_id = ? AND deleted_at IS NULL", config.UserID).Count(&count).Error; err != nil {
			return persistenceError(err)
		}
		if count >= int64(maximum) {
			return fault.Conflict("webhook 配置数量已达到系统上限")
		}
		return persistenceError(tx.Create(config).Error)
	})
}

// ListNoticeConfigs 只返回当前用户未删除的配置，调用方 DTO 必须再次遮蔽地址和签名密钥。
func (store *PostgresStore) ListNoticeConfigs(ctx context.Context, userID uuid.UUID) ([]model.NoticeConfig, error) {
	var configs []model.NoticeConfig
	err := store.db.WithContext(ctx).Where("user_id = ? AND deleted_at IS NULL", userID).Order("created_at DESC").Find(&configs).Error
	return configs, persistenceError(err)
}

// UpdateNoticeConfig 只允许修改令牌用户自己的未删除配置。
func (store *PostgresStore) UpdateNoticeConfig(ctx context.Context, userID, configID uuid.UUID, updates map[string]any) (model.NoticeConfig, error) {
	updates["updated_at"] = time.Now().UTC()
	result := store.db.WithContext(ctx).Model(&model.NoticeConfig{}).Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, configID).Updates(updates)
	if result.Error != nil {
		return model.NoticeConfig{}, persistenceError(result.Error)
	}
	if result.RowsAffected == 0 {
		return model.NoticeConfig{}, fault.NotFound()
	}
	return store.findNoticeConfig(ctx, userID, configID)
}

// DeleteNoticeConfig 软删除用户自己的渠道并取消尚未发送的普通通知 outbox；历史投递仍可查询。
func (store *PostgresStore) DeleteNoticeConfig(ctx context.Context, userID, configID uuid.UUID) error {
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.Model(&model.NoticeConfig{}).Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, configID).
			Updates(map[string]any{"deleted_at": now, "enabled": false, "updated_at": now})
		if result.Error != nil {
			return persistenceError(result.Error)
		}
		if result.RowsAffected == 0 {
			return fault.NotFound()
		}
		if err := tx.Model(&model.NoticeDelivery{}).
			Where("user_id = ? AND notice_config_business_id = ? AND delivery_status IN ('pending', 'retry') AND delivery_kind = 'notification'", userID, configID).
			Updates(map[string]any{"delivery_status": "cancelled", "last_error_code": "config_deleted", "updated_at": now}).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
}

// findNoticeConfig 读取未删除的单个配置，绝不提供跨用户回退查询。
func (store *PostgresStore) findNoticeConfig(ctx context.Context, userID, configID uuid.UUID) (model.NoticeConfig, error) {
	var config model.NoticeConfig
	err := store.db.WithContext(ctx).Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, configID).First(&config).Error
	return config, persistenceError(err)
}

// GetNoticeConfig 在 service 校验部分更新之前，读取当前用户拥有的未删除配置。
func (store *PostgresStore) GetNoticeConfig(ctx context.Context, userID, configID uuid.UUID) (model.NoticeConfig, error) {
	return store.findNoticeConfig(ctx, userID, configID)
}

// CreateNoticeTestDelivery 按当前用户所有权创建不关联站内通知的异步测试 outbox。
func (store *PostgresStore) CreateNoticeTestDelivery(ctx context.Context, userID, configID uuid.UUID, payload model.JSONB) (model.NoticeDelivery, error) {
	config, err := store.findNoticeConfig(ctx, userID, configID)
	if err != nil {
		return model.NoticeDelivery{}, err
	}
	now := time.Now().UTC()
	delivery := model.NoticeDelivery{
		BaseModel: model.BaseModel{BusinessID: uuid.New(), CreatedAt: now, UpdatedAt: now},
		UserID:    userID, DeliveryKind: "test", NoticeConfigBusinessID: config.BusinessID,
		Provider: config.Provider, ConfigName: config.Name, EventType: "notice.webhook_test",
		Payload: payload, PartIndex: 1, PartCount: 1, Status: "pending", NextAttemptAt: now,
	}
	if err := store.db.WithContext(ctx).Create(&delivery).Error; err != nil {
		return model.NoticeDelivery{}, persistenceError(err)
	}
	return delivery, nil
}

// ListNotifications 按用户隔离并分页返回消息；未读筛选和总数统计在服务端执行。
func (store *PostgresStore) ListNotifications(ctx context.Context, userID uuid.UUID, unreadOnly bool, page, pageSize int) ([]model.Notification, int64, int64, error) {
	base := store.db.WithContext(ctx).Model(&model.Notification{}).Where("user_id = ?", userID)
	var unread int64
	if err := store.db.WithContext(ctx).Model(&model.Notification{}).Where("user_id = ? AND read_at IS NULL", userID).Count(&unread).Error; err != nil {
		return nil, 0, 0, persistenceError(err)
	}
	query := base
	if unreadOnly {
		query = query.Where("read_at IS NULL")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, 0, persistenceError(err)
	}
	var items []model.Notification
	if err := query.Order("created_at DESC, business_id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, 0, persistenceError(err)
	}
	return items, total, unread, nil
}

// CountUnreadNotifications 返回当前用户的未读总数，页面角标不需要读取消息正文。
func (store *PostgresStore) CountUnreadNotifications(ctx context.Context, userID uuid.UUID) (int64, error) {
	var count int64
	err := store.db.WithContext(ctx).Model(&model.Notification{}).Where("user_id = ? AND read_at IS NULL", userID).Count(&count).Error
	return count, persistenceError(err)
}

// ListNotificationDeliveries 返回选中站内消息对应的用户渠道状态，不查询其他用户的 delivery。
func (store *PostgresStore) ListNotificationDeliveries(ctx context.Context, userID uuid.UUID, notificationIDs []uuid.UUID) ([]model.NoticeDelivery, error) {
	if len(notificationIDs) == 0 {
		return []model.NoticeDelivery{}, nil
	}
	var deliveries []model.NoticeDelivery
	err := store.db.WithContext(ctx).Where("user_id = ? AND notification_business_id IN ?", userID, notificationIDs).
		Order("created_at ASC, notice_config_business_id ASC, part_index ASC").Find(&deliveries).Error
	return deliveries, persistenceError(err)
}

// ReadNotification 只将当前用户自己的消息标记为已读，重复操作幂等成功。
func (store *PostgresStore) ReadNotification(ctx context.Context, userID, notificationID uuid.UUID) error {
	now := time.Now().UTC()
	result := store.db.WithContext(ctx).Model(&model.Notification{}).
		Where("user_id = ? AND business_id = ? AND read_at IS NULL", userID, notificationID).
		Updates(map[string]any{"read_at": now, "updated_at": now})
	if result.Error != nil {
		return persistenceError(result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}
	var count int64
	if err := store.db.WithContext(ctx).Model(&model.Notification{}).Where("user_id = ? AND business_id = ?", userID, notificationID).Count(&count).Error; err != nil {
		return persistenceError(err)
	}
	if count == 0 {
		return fault.NotFound()
	}
	return nil
}

// ReadAllNotifications 将该用户当前所有未读消息统一标记为已读。
func (store *PostgresStore) ReadAllNotifications(ctx context.Context, userID uuid.UUID) (int64, error) {
	now := time.Now().UTC()
	result := store.db.WithContext(ctx).Model(&model.Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID).
		Updates(map[string]any{"read_at": now, "updated_at": now})
	if result.Error != nil {
		return 0, persistenceError(result.Error)
	}
	return result.RowsAffected, nil
}

// GetNoticeDelivery 返回当前用户自己的投递状态，业务 UUID 不会转换成数据库 id。
func (store *PostgresStore) GetNoticeDelivery(ctx context.Context, userID, deliveryID uuid.UUID) (model.NoticeDelivery, error) {
	var delivery model.NoticeDelivery
	err := store.db.WithContext(ctx).Where("user_id = ? AND business_id = ?", userID, deliveryID).First(&delivery).Error
	return delivery, persistenceError(err)
}

// RetryNoticeDelivery 只允许用户重试自己的失败任务，并在重新排队前恢复渠道配置校验。
func (store *PostgresStore) RetryNoticeDelivery(ctx context.Context, userID, deliveryID uuid.UUID) (model.NoticeDelivery, error) {
	var delivery model.NoticeDelivery
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ?", userID, deliveryID).First(&delivery).Error; err != nil {
			return persistenceError(err)
		}
		if delivery.Status != "failed" {
			return fault.Conflict("只有最终失败的投递任务可以手动重试")
		}
		var config model.NoticeConfig
		if err := tx.Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, delivery.NoticeConfigBusinessID).First(&config).Error; err != nil {
			return persistenceError(err)
		}
		if !config.Enabled && delivery.DeliveryKind != "test" {
			return fault.Conflict("请先启用通知渠道再重试业务消息")
		}
		now := time.Now().UTC()
		if err := tx.Model(&delivery).Updates(map[string]any{
			"delivery_status": "pending", "attempt_count": 0, "next_attempt_at": now,
			"last_attempt_at": nil, "delivered_at": nil, "last_error_code": "", "updated_at": now,
		}).Error; err != nil {
			return persistenceError(err)
		}
		delivery.Status = "pending"
		delivery.AttemptCount = 0
		delivery.NextAttemptAt = now
		delivery.LastAttemptAt = nil
		delivery.DeliveredAt = nil
		delivery.LastErrorCode = ""
		return nil
	})
	if err != nil {
		return model.NoticeDelivery{}, err
	}
	return delivery, nil
}

// ClaimNoticeDelivery 用 SKIP LOCKED 领取一项到期任务，进程崩溃后可回收超时的 sending 任务。
func (store *PostgresStore) ClaimNoticeDelivery(ctx context.Context) (model.NoticeDelivery, model.NoticeConfig, bool, error) {
	var delivery model.NoticeDelivery
	var config model.NoticeConfig
	claimed := false
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("(delivery_status IN ('pending', 'retry') AND next_attempt_at <= ?) OR (delivery_status = 'sending' AND updated_at <= ?)", now, now.Add(-2*time.Minute)).
			Order("next_attempt_at ASC, created_at ASC").Limit(1).Find(&delivery)
		if result.Error != nil {
			return persistenceError(result.Error)
		}
		if result.RowsAffected == 0 {
			return nil
		}
		if err := tx.Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", delivery.UserID, delivery.NoticeConfigBusinessID).First(&config).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return tx.Model(&delivery).Updates(map[string]any{"delivery_status": "cancelled", "last_error_code": "config_unavailable", "updated_at": now}).Error
			}
			return persistenceError(err)
		}
		if !config.Enabled && delivery.DeliveryKind != "test" {
			return tx.Model(&delivery).Updates(map[string]any{"delivery_status": "cancelled", "last_error_code": "config_disabled", "updated_at": now}).Error
		}
		delivery.Status = "sending"
		delivery.AttemptCount++
		delivery.LastAttemptAt = &now
		delivery.UpdatedAt = now
		if err := tx.Model(&delivery).Updates(map[string]any{
			"delivery_status": delivery.Status, "attempt_count": delivery.AttemptCount,
			"last_attempt_at": now, "updated_at": now,
		}).Error; err != nil {
			return persistenceError(err)
		}
		claimed = true
		return nil
	})
	return delivery, config, claimed, err
}

// FinishNoticeDelivery 安全记录投递结果；只保存错误分类，并为有限重试设置下一次执行时间。
func (store *PostgresStore) FinishNoticeDelivery(ctx context.Context, deliveryID uuid.UUID, attemptCount int, succeeded bool, errorCode string) error {
	now := time.Now().UTC()
	updates := map[string]any{"updated_at": now, "last_error_code": errorCode}
	if succeeded {
		updates["delivery_status"] = "delivered"
		updates["delivered_at"] = now
	} else if attemptCount >= 5 {
		updates["delivery_status"] = "failed"
	} else {
		delays := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
		updates["delivery_status"] = "retry"
		updates["next_attempt_at"] = now.Add(delays[attemptCount-1])
	}
	result := store.db.WithContext(ctx).Model(&model.NoticeDelivery{}).
		Where("business_id = ? AND delivery_status = 'sending'", deliveryID).Updates(updates)
	if result.Error != nil {
		return persistenceError(result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}
