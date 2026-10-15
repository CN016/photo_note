package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ListScheduleData 读取当前用户的档期和单次例外；调用者必须传入来自令牌的业务 UUID。
func (store *PostgresStore) ListScheduleData(ctx context.Context, userID uuid.UUID) ([]model.ScheduleItem, []model.ScheduleException, error) {
	var items []model.ScheduleItem
	if err := store.db.WithContext(ctx).Where("user_id = ?", userID).Order("start_at, business_id").Find(&items).Error; err != nil {
		return nil, nil, persistenceError(err)
	}
	var exceptions []model.ScheduleException
	if err := store.db.WithContext(ctx).Where("user_id = ?", userID).Order("occurrence_date").Find(&exceptions).Error; err != nil {
		return nil, nil, persistenceError(err)
	}
	return items, exceptions, nil
}

// CreateScheduleItem 在用户级 advisory transaction lock 下校验冲突并插入，串行化并发的开放时段新增。
func (store *PostgresStore) CreateScheduleItem(ctx context.Context, item *model.ScheduleItem, validate func([]model.ScheduleItem, []model.ScheduleException) error) error {
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockScheduleOwner(tx, item.UserID); err != nil {
			return err
		}
		items, exceptions, err := loadScheduleData(tx, item.UserID)
		if err != nil {
			return err
		}
		if err := validate(items, exceptions); err != nil {
			return err
		}
		if err := tx.Create(item).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
}

// UpdateScheduleItem 在用户级事务锁中重新运行服务冲突校验，防止并发修改建立重叠档期。
func (store *PostgresStore) UpdateScheduleItem(ctx context.Context, userID uuid.UUID, item *model.ScheduleItem, validate func([]model.ScheduleItem, []model.ScheduleException) error) error {
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockScheduleOwner(tx, userID); err != nil {
			return err
		}
		var existing model.ScheduleItem
		if err := tx.Where("user_id = ? AND business_id = ?", userID, item.BusinessID).First(&existing).Error; err != nil {
			return persistenceError(err)
		}
		items, exceptions, err := loadScheduleData(tx, userID)
		if err != nil {
			return err
		}
		if err := validate(items, exceptions); err != nil {
			return err
		}
		updates := map[string]any{
			"item_kind": item.Kind, "title": item.Title, "content": item.Content,
			"start_at": item.StartAt, "end_at": item.EndAt, "recurrence": item.Recurrence,
			"recurrence_weekdays": item.RecurrenceWeekdays, "recurrence_end": item.RecurrenceEnd,
			"capacity": item.Capacity, "updated_at": time.Now().UTC(),
		}
		result := tx.Model(&model.ScheduleItem{}).Where("user_id = ? AND business_id = ?", userID, item.BusinessID).Updates(updates)
		if result.Error != nil {
			return persistenceError(result.Error)
		}
		if result.RowsAffected == 0 {
			return fault.NotFound()
		}
		item.CreatedAt = existing.CreatedAt
		item.UpdatedAt = time.Now().UTC()
		return nil
	})
}

// DeleteScheduleItem 删除拥有者的档期和例外；预约记录作为处理审计保留，不级联删除。
func (store *PostgresStore) DeleteScheduleItem(ctx context.Context, userID, itemID uuid.UUID) error {
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockScheduleOwner(tx, userID); err != nil {
			return err
		}
		result := tx.Where("user_id = ? AND business_id = ?", userID, itemID).Delete(&model.ScheduleItem{})
		if result.Error != nil {
			return persistenceError(result.Error)
		}
		if result.RowsAffected == 0 {
			return fault.NotFound()
		}
		if err := tx.Where("user_id = ? AND schedule_business_id = ?", userID, itemID).Delete(&model.ScheduleException{}).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
}

// SaveScheduleException 原子保存指定发生项例外，并在用户锁内重新检查覆盖时段冲突。
func (store *PostgresStore) SaveScheduleException(ctx context.Context, exception *model.ScheduleException, validate func([]model.ScheduleItem, []model.ScheduleException) error) error {
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockScheduleOwner(tx, exception.UserID); err != nil {
			return err
		}
		var item model.ScheduleItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ?", exception.UserID, exception.ScheduleBusinessID).First(&item).Error; err != nil {
			return persistenceError(err)
		}
		items, exceptions, err := loadScheduleData(tx, exception.UserID)
		if err != nil {
			return err
		}
		if err := validate(items, exceptions); err != nil {
			return err
		}
		columns := []clause.Column{{Name: "schedule_business_id"}, {Name: "occurrence_date"}}
		updates := []string{"exception_action", "title", "content", "start_at", "end_at", "capacity", "updated_at"}
		result := tx.Clauses(clause.OnConflict{Columns: columns, DoUpdates: clause.AssignmentColumns(updates)}).Create(exception)
		if result.Error != nil {
			return persistenceError(result.Error)
		}
		return nil
	})
}

// DeleteScheduleException 移除指定用户发生日期的覆盖或取消状态。
func (store *PostgresStore) DeleteScheduleException(ctx context.Context, userID, itemID uuid.UUID, date time.Time) error {
	result := store.db.WithContext(ctx).Where("user_id = ? AND schedule_business_id = ? AND occurrence_date = ?", userID, itemID, date.Format("2006-01-02")).Delete(&model.ScheduleException{})
	if result.Error != nil {
		return persistenceError(result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

// ListScheduleBookings 只返回当前拥有者的申请，并在读取时把超过 24 小时的待确认申请释放。
func (store *PostgresStore) ListScheduleBookings(ctx context.Context, userID uuid.UUID, status string) ([]model.ScheduleBooking, error) {
	query := store.db.WithContext(ctx).Model(&model.ScheduleBooking{}).Where("user_id = ?", userID)
	if status == "active" {
		query = query.Where("booking_status IN ?", []string{"pending", "confirmed"})
	} else if status != "" && status != "all" {
		query = query.Where("booking_status = ?", status)
	}
	if err := store.db.WithContext(ctx).Model(&model.ScheduleBooking{}).
		Where("user_id = ? AND booking_status = 'pending' AND expires_at <= ?", userID, time.Now().UTC()).
		Updates(map[string]any{"booking_status": "expired", "expires_at": nil, "updated_at": time.Now().UTC()}).Error; err != nil {
		return nil, persistenceError(err)
	}
	var bookings []model.ScheduleBooking
	if err := query.Order("created_at DESC, business_id DESC").Find(&bookings).Error; err != nil {
		return nil, persistenceError(err)
	}
	if bookings == nil {
		bookings = make([]model.ScheduleBooking, 0)
	}
	return bookings, nil
}

// DecideScheduleBooking 只允许拥有者处理仍有效的待确认申请，过期申请不能被确认。
func (store *PostgresStore) DecideScheduleBooking(ctx context.Context, userID, bookingID uuid.UUID, status string) error {
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var booking model.ScheduleBooking
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ?", userID, bookingID).First(&booking).Error; err != nil {
			return persistenceError(err)
		}
		now := time.Now().UTC()
		if booking.Status != "pending" || booking.ExpiresAt == nil || !booking.ExpiresAt.After(now) {
			if booking.Status == "pending" {
				_ = tx.Model(&booking).Updates(map[string]any{"booking_status": "expired", "expires_at": nil, "updated_at": now}).Error
			}
			return fault.Conflict("该申请已处理或已过期")
		}
		updates := map[string]any{"booking_status": status, "expires_at": nil, "updated_at": now}
		if err := tx.Model(&booking).Updates(updates).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
}

// CancelScheduleBooking 将拥有者的待确认或已确认申请置为 cancelled 并清除 24 小时占位到期时间。
func (store *PostgresStore) CancelScheduleBooking(ctx context.Context, userID, bookingID uuid.UUID) error {
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var booking model.ScheduleBooking
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ?", userID, bookingID).First(&booking).Error; err != nil {
			return persistenceError(err)
		}
		if booking.Status != "pending" && booking.Status != "confirmed" {
			return fault.Conflict("该申请已释放名额，不能再次取消")
		}
		if err := tx.Model(&booking).Updates(map[string]any{"booking_status": "cancelled", "expires_at": nil, "updated_at": time.Now().UTC()}).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
}

// GetScheduleShare 返回当前用户的分享配置；尚未创建时返回未启用状态。
func (store *PostgresStore) GetScheduleShare(ctx context.Context, userID uuid.UUID) (model.ScheduleShare, error) {
	var share model.ScheduleShare
	err := store.db.WithContext(ctx).Where("user_id = ?", userID).First(&share).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.ScheduleShare{UserID: userID}, nil
	}
	if err != nil {
		return model.ScheduleShare{}, persistenceError(err)
	}
	return share, nil
}

// SaveScheduleShare 以用户唯一索引原子替换令牌摘要，旧分享 URL 随即失效。
func (store *PostgresStore) SaveScheduleShare(ctx context.Context, share *model.ScheduleShare) error {
	updates := []string{"business_id", "token_hash", "enabled", "updated_at"}
	result := store.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}}, DoUpdates: clause.AssignmentColumns(updates),
	}).Create(share)
	if result.Error != nil {
		return persistenceError(result.Error)
	}
	return nil
}

// RevokeScheduleShare 立即停用當前用戶分享鏈接，配置不存在時保持冪等。
func (store *PostgresStore) RevokeScheduleShare(ctx context.Context, userID uuid.UUID) error {
	if err := store.db.WithContext(ctx).Model(&model.ScheduleShare{}).Where("user_id = ?", userID).Updates(map[string]any{"enabled": false, "updated_at": time.Now().UTC()}).Error; err != nil {
		return persistenceError(err)
	}
	return nil
}

// FindScheduleShareByHash 仅以令牌摘要查找分享，不在查询或日志中包含原始令牌。
func (store *PostgresStore) FindScheduleShareByHash(ctx context.Context, tokenHash string) (model.ScheduleShare, error) {
	var share model.ScheduleShare
	if err := store.db.WithContext(ctx).Where("token_hash = ?", tokenHash).First(&share).Error; err != nil {
		return model.ScheduleShare{}, persistenceError(err)
	}
	return share, nil
}

// CreateScheduleBooking 锁定开放时段行并在同一事务内过期旧申请、统计名额及插入新申请。
func (store *PostgresStore) CreateScheduleBooking(ctx context.Context, booking *model.ScheduleBooking, tokenHash string, match func(model.ScheduleItem, []model.ScheduleException) (int, bool)) error {
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var share model.ScheduleShare
		if err := tx.Where("user_id = ? AND token_hash = ? AND enabled = true", booking.UserID, tokenHash).First(&share).Error; err != nil {
			return fault.NotFound()
		}
		var item model.ScheduleItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ?", booking.UserID, booking.ScheduleBusinessID).First(&item).Error; err != nil {
			return fault.NotFound()
		}
		var exceptions []model.ScheduleException
		if err := tx.Where("user_id = ? AND schedule_business_id = ?", booking.UserID, booking.ScheduleBusinessID).Find(&exceptions).Error; err != nil {
			return persistenceError(err)
		}
		capacity, matches := match(item, exceptions)
		if !matches || capacity < 1 {
			return fault.Conflict("该开放时段已不可预约")
		}
		now := time.Now().UTC()
		if err := tx.Model(&model.ScheduleBooking{}).
			Where("schedule_business_id = ? AND occurrence_start = ? AND booking_status = 'pending' AND expires_at <= ?", booking.ScheduleBusinessID, booking.OccurrenceStart, now).
			Updates(map[string]any{"booking_status": "expired", "expires_at": nil, "updated_at": now}).Error; err != nil {
			return persistenceError(err)
		}
		var occupied int64
		if err := tx.Model(&model.ScheduleBooking{}).
			Where("schedule_business_id = ? AND occurrence_start = ? AND (booking_status = 'confirmed' OR (booking_status = 'pending' AND expires_at > ?))", booking.ScheduleBusinessID, booking.OccurrenceStart, now).
			Count(&occupied).Error; err != nil {
			return persistenceError(err)
		}
		if occupied >= int64(capacity) {
			return fault.Conflict("该开放时段名额已满")
		}
		booking.OccurrenceEnd = booking.OccurrenceEnd.UTC()
		booking.OccurrenceStart = booking.OccurrenceStart.UTC()
		booking.ExpiresAt = timePtr(now.Add(24 * time.Hour))
		if err := tx.Create(booking).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
}

// loadScheduleData 读取事务中的用户档期与例外，供业务回调在提交前进行一致性检查。
func loadScheduleData(tx *gorm.DB, userID uuid.UUID) ([]model.ScheduleItem, []model.ScheduleException, error) {
	var items []model.ScheduleItem
	if err := tx.Where("user_id = ?", userID).Order("start_at, business_id").Find(&items).Error; err != nil {
		return nil, nil, persistenceError(err)
	}
	var exceptions []model.ScheduleException
	if err := tx.Where("user_id = ?", userID).Order("occurrence_date").Find(&exceptions).Error; err != nil {
		return nil, nil, persistenceError(err)
	}
	return items, exceptions, nil
}

// lockScheduleOwner 通过事务级 advisory lock 串行化同一用户可能产生档期冲突的写入。
func lockScheduleOwner(tx *gorm.DB, userID uuid.UUID) error {
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", userID.String()).Error; err != nil {
		return persistenceError(err)
	}
	return nil
}

// timePtr 创建 UTC 时间指针，避免与 service 包产生跨层依赖。
func timePtr(value time.Time) *time.Time { return &value }
