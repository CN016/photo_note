package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecordErrorLog 在 PostgreSQL 事务中按去重摘要串行处理；一分钟内的重复错误只增加计数，不新增日志行。
func (store *PostgresStore) RecordErrorLog(ctx context.Context, entry model.ErrorLog) (model.ErrorLog, bool, error) {
	created := false
	if entry.BusinessID == uuid.Nil {
		entry.BusinessID = uuid.New()
	}
	now := time.Now().UTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	entry.LastSeenAt = now
	if entry.Occurrences < 1 {
		entry.Occurrences = 1
	}
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// PostgreSQL advisory lock 让多 API 实例也会串行判断同一去重键，避免并发首错插入重复行。
		var lockAcquired int
		if err := tx.Raw(`SELECT 1 FROM (SELECT pg_advisory_xact_lock(hashtextextended(?, 0))) AS error_log_lock`, entry.DedupKey).
			Scan(&lockAcquired).Error; err != nil {
			return persistenceError(err)
		}

		var previous model.ErrorLog
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("dedup_key = ? AND last_seen_at >= ?", entry.DedupKey, now.Add(-time.Minute)).
			Order("last_seen_at DESC").First(&previous)
		if result.Error == nil {
			if err := tx.Model(&model.ErrorLog{}).Where("business_id = ?", previous.BusinessID).Updates(map[string]any{
				"occurrence_count": gorm.Expr("occurrence_count + 1"),
				"last_seen_at":     now,
				"updated_at":       now,
			}).Error; err != nil {
				return persistenceError(err)
			}
			previous.Occurrences++
			previous.LastSeenAt = now
			previous.UpdatedAt = now
			entry = previous
			return nil
		}
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return persistenceError(result.Error)
		}
		if err := tx.Create(&entry).Error; err != nil {
			return persistenceError(err)
		}
		created = true
		return nil
	})
	if err != nil {
		return model.ErrorLog{}, false, err
	}
	return entry, created, nil
}

// FindUserBusinessIDByPhone 只读取管理员手机号对应的业务 UUID，不加载密码哈希或其他用户资料。
func (store *PostgresStore) FindUserBusinessIDByPhone(ctx context.Context, phone string) (uuid.UUID, error) {
	var user model.User
	if err := store.db.WithContext(ctx).Select("business_id").Where("phone = ?", phone).First(&user).Error; err != nil {
		return uuid.Nil, persistenceError(err)
	}
	return user.BusinessID, nil
}

// CreateErrorLogNotification 只创建管理员站内消息，不把内部错误摘要转发到用户 webhook 渠道。
func (store *PostgresStore) CreateErrorLogNotification(ctx context.Context, userID, errorLogID uuid.UUID, eventKey string, payload model.JSONB) error {
	now := time.Now().UTC()
	notification := model.Notification{
		BaseModel:          model.BaseModel{BusinessID: uuid.New(), CreatedAt: now, UpdatedAt: now},
		UserID:             userID,
		EventType:          "system.error_log.created",
		EventKey:           eventKey,
		ResourceType:       "error_log",
		ResourceBusinessID: &errorLogID,
		Payload:            payload,
	}
	result := store.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "event_key"}},
		DoNothing: true,
	}).Create(&notification)
	return persistenceError(result.Error)
}
