package store

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// CreateDeliveryExport 锁住任务后校验快照版本；同版本同用途打包请求复用，避免重复占用磁盘。
func (s *PostgresStore) CreateDeliveryExport(ctx context.Context, job *model.DeliveryExport) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if scope.UserID != job.UserID {
		return fault.NotFound()
	}
	return persistenceError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.DeliveryTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("business_id = ? AND user_id = ?", job.TaskID, scope.UserID).First(&task).Error; err != nil {
			return persistenceError(err)
		}
		if task.Version != job.TaskVersion {
			return fault.Conflict("任务已更新，请重新导出")
		}
		var existing model.DeliveryExport
		err := tx.Where("user_id = ? AND task_id = ? AND task_version = ? AND kind = ? AND public = ? AND photo_ids = ? AND status IN ?", scope.UserID, job.TaskID, job.TaskVersion, job.Kind, job.Public, job.PhotoIDs, []string{"pending", "running", "completed"}).First(&existing).Error
		if err == nil {
			*job = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(job).Error
	}))
}

// FindDeliveryExport 即使知道作业 UUID，仍须匹配原任务、空间和创建者。
func (s *PostgresStore) FindDeliveryExport(ctx context.Context, task, id uuid.UUID) (model.DeliveryExport, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return model.DeliveryExport{}, err
	}
	var job model.DeliveryExport
	err = s.db.WithContext(ctx).Where("user_id = ? AND task_id = ? AND business_id = ?", scope.UserID, task, id).First(&job).Error
	return job, persistenceError(err)
}

// ClaimDeliveryExport 仅 worker 系统入口跨空间领取；五分钟租约失效可恢复，最多三次。
func (s *PostgresStore) ClaimDeliveryExport(ctx context.Context) (model.DeliveryExport, bool, error) {
	var job model.DeliveryExport
	found := false
	err := s.db.WithContext(tenant.System(ctx)).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status = 'pending' OR (status = 'running' AND updated_at < ?)", time.Now().Add(-5*time.Minute)).Order("created_at").First(&job)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if result.Error != nil {
			return result.Error
		}
		found = true
		job.Attempts++
		job.Status = "running"
		job.Progress = 0
		if job.Attempts > 3 {
			job.Status = "failed"
			job.ErrorCode = "interrupted"
			found = false
		}
		return tx.Model(&model.DeliveryExport{}).Where("business_id = ?", job.BusinessID).Updates(map[string]any{"status": job.Status, "attempts": job.Attempts, "progress": 0, "error_code": job.ErrorCode}).Error
	})
	return job, found, persistenceError(err)
}

// UpdateDeliveryExport 使用租约代次条件回写；另一个 worker 已重领时旧 worker 必须停止。
func (s *PostgresStore) UpdateDeliveryExport(ctx context.Context, job model.DeliveryExport, changes map[string]any) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if scope.UserID != job.UserID {
		return fault.NotFound()
	}
	return persistenceError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.DeliveryExport{}).Where("business_id = ? AND user_id = ? AND task_id = ? AND attempts = ? AND status = 'running'", job.BusinessID, job.UserID, job.TaskID, job.Attempts).Updates(changes)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fault.Conflict("打包作业代次已失效")
		}
		if status, ok := changes["status"].(string); ok && (status == "completed" || status == "failed") {
			event := model.DeliveryEvent{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: job.UserID, TaskID: job.TaskID, Action: "export_" + status, Stage: "export", TaskVersion: job.TaskVersion}
			return tx.Create(&event).Error
		}
		return nil
	}))
}

// ExpiredDeliveryExports 仅 worker 扫描已结束的临时包，原始照片不参与自动清理。
func (s *PostgresStore) ExpiredDeliveryExports(ctx context.Context, before time.Time) ([]model.DeliveryExport, error) {
	items := []model.DeliveryExport{}
	err := s.db.WithContext(tenant.System(ctx)).Where("status IN ? AND updated_at < ?", []string{"completed", "failed"}, before).Limit(20).Find(&items).Error
	return items, persistenceError(err)
}

// DeleteDeliveryExport 在物理文件成功清理后移除对应记录；必须显式携带原空间和创建者。
func (s *PostgresStore) DeleteDeliveryExport(ctx context.Context, job model.DeliveryExport) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if scope.UserID != job.UserID {
		return fault.NotFound()
	}
	return persistenceError(s.db.WithContext(ctx).Where("business_id = ? AND user_id = ? AND task_id = ? AND status IN ?", job.BusinessID, job.UserID, job.TaskID, []string{"completed", "failed"}).Delete(&model.DeliveryExport{}).Error)
}
