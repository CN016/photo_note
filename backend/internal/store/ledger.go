package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"gorm.io/gorm"
)

// ListLedgerRecords 查询用户日期范围内正式记录，并附带该用户所有尚未完成的草稿。
func (store *PostgresStore) ListLedgerRecords(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]model.LedgerRecord, error) {
	var records []model.LedgerRecord
	err := store.db.WithContext(ctx).Where(
		"user_id = ? AND (record_date BETWEEN ? AND ? OR record_status = ? OR draft_data IS NOT NULL)",
		userID, from, to, "draft",
	).Order("record_date DESC, created_at DESC, business_id DESC").Find(&records).Error
	if err != nil {
		return nil, persistenceError(err)
	}
	return records, nil
}

// CreateLedgerRecord 保存由服务端生成业务 UUID、归属用户和初始修订号的新草稿。
func (store *PostgresStore) CreateLedgerRecord(ctx context.Context, record *model.LedgerRecord) error {
	if err := store.db.WithContext(ctx).Create(record).Error; err != nil {
		return persistenceError(err)
	}
	return nil
}

// SaveLedgerDraft 用修订号比较并交换保存快照，阻止迟到的旧请求覆盖较新的输入。
func (store *PostgresStore) SaveLedgerDraft(ctx context.Context, userID, recordID uuid.UUID, revision int64, draft model.JSONB) (model.LedgerRecord, error) {
	var saved model.LedgerRecord
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.LedgerRecord{}).
			Where("user_id = ? AND business_id = ? AND draft_version = ?", userID, recordID, revision).
			Updates(map[string]any{"draft_data": draft, "draft_version": revision + 1, "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return persistenceError(result.Error)
		}
		if result.RowsAffected == 0 {
			return fault.Conflict("草稿已被更新，请刷新后重试")
		}
		if err := tx.Where("user_id = ? AND business_id = ?", userID, recordID).First(&saved).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
	if err != nil {
		return model.LedgerRecord{}, err
	}
	return saved, nil
}

// FindLedgerRecord 通过当前用户和业务 UUID 双重限定账本记录读取。
func (store *PostgresStore) FindLedgerRecord(ctx context.Context, userID, recordID uuid.UUID) (model.LedgerRecord, error) {
	var record model.LedgerRecord
	if err := store.db.WithContext(ctx).Where("user_id = ? AND business_id = ?", userID, recordID).First(&record).Error; err != nil {
		return model.LedgerRecord{}, persistenceError(err)
	}
	return record, nil
}

// CompleteLedgerRecord 原子应用正式字段并清除草稿；版本不符时保留最新草稿供用户恢复。
func (store *PostgresStore) CompleteLedgerRecord(ctx context.Context, userID, recordID uuid.UUID, revision int64, complete model.LedgerRecord) (model.LedgerRecord, error) {
	var saved model.LedgerRecord
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"record_date": complete.RecordDate, "record_time": complete.RecordTime,
			"record_type": complete.RecordType, "amount_fen": complete.AmountFen,
			"category": complete.Category, "payment_method": complete.PaymentMethod,
			"flag": complete.Flag, "note": complete.Note, "record_status": "complete",
			"draft_data": nil, "draft_version": revision + 1, "updated_at": time.Now().UTC(),
		}
		result := tx.Model(&model.LedgerRecord{}).
			Where("user_id = ? AND business_id = ? AND draft_version = ? AND draft_data IS NOT NULL", userID, recordID, revision).
			Updates(updates)
		if result.Error != nil {
			return persistenceError(result.Error)
		}
		if result.RowsAffected == 0 {
			return fault.Conflict("草稿已被更新或删除，请刷新后重试")
		}
		if err := tx.Where("user_id = ? AND business_id = ?", userID, recordID).First(&saved).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
	if err != nil {
		return model.LedgerRecord{}, err
	}
	return saved, nil
}

// DeleteLedgerRecord 只物理删除当前用户拥有的目标业务 UUID 记录。
func (store *PostgresStore) DeleteLedgerRecord(ctx context.Context, userID, recordID uuid.UUID) error {
	result := store.db.WithContext(ctx).Where("user_id = ? AND business_id = ?", userID, recordID).Delete(&model.LedgerRecord{})
	if result.Error != nil {
		return persistenceError(result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

// ImportLedgerRecords 在单个事务中按当前用户的业务 UUID 更新，否则生成新 UUID 后创建。
func (store *PostgresStore) ImportLedgerRecords(ctx context.Context, userID uuid.UUID, records []model.LedgerRecord) (created, updated int, err error) {
	err = store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, record := range records {
			var existing model.LedgerRecord
			found := false
			if record.BusinessID != uuid.Nil {
				queryErr := tx.Where("user_id = ? AND business_id = ?", userID, record.BusinessID).First(&existing).Error
				if queryErr == nil {
					found = true
				} else if queryErr != gorm.ErrRecordNotFound {
					return persistenceError(queryErr)
				}
			}
			if found {
				result := tx.Model(&model.LedgerRecord{}).
					Where("user_id = ? AND business_id = ?", userID, record.BusinessID).
					Updates(map[string]any{
						"record_date": record.RecordDate, "record_time": record.RecordTime,
						"record_type": record.RecordType, "amount_fen": record.AmountFen,
						"category": record.Category, "payment_method": record.PaymentMethod,
						"flag": record.Flag, "note": record.Note, "record_status": "complete",
						"draft_data": nil, "draft_version": gorm.Expr("draft_version + 1"), "updated_at": time.Now().UTC(),
					})
				if result.Error != nil {
					return persistenceError(result.Error)
				}
				updated++
				continue
			}
			record.ID = 0
			record.UserID = userID
			record.BusinessID = uuid.New()
			record.Status = "complete"
			record.DraftData = nil
			record.DraftVersion = 0
			if err := tx.Create(&record).Error; err != nil {
				return persistenceError(err)
			}
			created++
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return created, updated, nil
}
