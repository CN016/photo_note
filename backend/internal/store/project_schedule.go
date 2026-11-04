package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SyncProjectSchedule 在既有档期用户锁和策划行锁下同步，所有重试共用持久化关联。
// validate 在同一事务内重新检查候选和预约冲突，避免查询候选后并发创建造成重复档期。
func (s *PostgresStore) SyncProjectSchedule(ctx context.Context, user, project uuid.UUID, revision int64, selected *uuid.UUID, detach bool, candidate model.ScheduleItem, validate func(model.ScheduleItem, []model.ScheduleItem, []model.ScheduleException, bool) error) (model.Project, error) {
	if err := projectUserBoundary(ctx, user); err != nil {
		return model.Project{}, err
	}
	var source model.Project
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockScheduleOwner(tx, user); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", user, project).First(&source).Error; err != nil {
			return persistenceError(err)
		}
		if source.Revision != revision {
			return fault.Conflict("策划已更新，请先保存再关联档期")
		}
		if detach {
			source.ScheduleID, source.ScheduleOwned = nil, false
			source.ScheduleFingerprint = source.Shooting.Fingerprint()
		} else {
			items, exceptions, err := loadScheduleData(tx, user)
			if err != nil {
				return err
			}
			if source.ScheduleID != nil {
				if selected != nil && *selected != *source.ScheduleID {
					return fault.Conflict("请先解除已有档期关联")
				}
				var existing *model.ScheduleItem
				for i := range items {
					if items[i].BusinessID == *source.ScheduleID {
						existing = &items[i]
						break
					}
				}
				if existing == nil {
					return fault.New(409, "project_schedule_missing", "关联档期已删除，请解除关联后重试", nil, nil)
				}
				if !source.ScheduleOwned {
					return nil
				}
				if existing.Kind != "work" || existing.Recurrence != "none" {
					return fault.Conflict("关联档期已改变类型或重复规则，请解除关联后重新选择")
				}
				candidate.BusinessID = *source.ScheduleID
				if err := validate(candidate, items, exceptions, true); err != nil {
					return err
				}
				if err := tx.Model(&model.ScheduleItem{}).Where("user_id = ? AND business_id = ?", user, candidate.BusinessID).Updates(map[string]any{"title": candidate.Title, "content": candidate.Content, "start_at": candidate.StartAt, "end_at": candidate.EndAt, "updated_at": time.Now().UTC()}).Error; err != nil {
					return persistenceError(err)
				}
			} else {
				if err := validate(candidate, items, exceptions, false); err != nil {
					return err
				}
				if selected != nil {
					var found bool
					for _, item := range items {
						if item.BusinessID == *selected && item.Kind == "work" {
							found = true
							break
						}
					}
					if !found {
						return fault.NotFound()
					}
					source.ScheduleID, source.ScheduleOwned = selected, false
				} else {
					candidate.BusinessID = uuid.New()
					if err := tx.Create(&candidate).Error; err != nil {
						return persistenceError(err)
					}
					source.ScheduleID, source.ScheduleOwned = &candidate.BusinessID, true
				}
			}
			source.ScheduleFingerprint = source.Shooting.Fingerprint()
		}
		// 关联字段不接受普通策划快照写入，也不消耗正文修订号，避免迟到响应覆盖编辑草稿。
		return persistenceError(tx.Model(&model.Project{}).Where("user_id = ? AND business_id = ?", user, project).Updates(map[string]any{"schedule_id": source.ScheduleID, "schedule_owned": source.ScheduleOwned, "schedule_fingerprint": source.ScheduleFingerprint}).Error)
	})
	return source, err
}
