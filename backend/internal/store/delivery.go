package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reflect"
)

// DeliveryBundle 是事务内工作集；任务行锁串行化发布、上传落库和客户提交，避免快照变化。
type DeliveryBundle struct {
	Task   model.DeliveryTask
	Photos []model.DeliveryPhoto
	Rounds []model.DeliveryRound
	Events []model.DeliveryEvent
}

// ListDeliveries 只查询当前会话创建者；设置上限避免无限读取历史任务。
func (s *PostgresStore) ListDeliveries(ctx context.Context) ([]model.DeliveryTask, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return nil, err
	}
	items := []model.DeliveryTask{}
	err = s.db.WithContext(ctx).Where("user_id = ?", scope.UserID).Order("updated_at DESC").Limit(500).Find(&items).Error
	return items, persistenceError(err)
}

// CreateDelivery 仅接受与可信会话一致的创建者，组织由租户回调写入。
func (s *PostgresStore) CreateDelivery(ctx context.Context, item *model.DeliveryTask) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if scope.UserID != item.UserID {
		return fault.NotFound()
	}
	return persistenceError(s.db.WithContext(ctx).Create(item).Error)
}

// ResolveDelivery 是唯一允许令牌摘要跨空间定位的入口；调用者必须随后收窄上下文。
func (s *PostgresStore) ResolveDelivery(ctx context.Context, hash string) (model.DeliveryTask, error) {
	var item model.DeliveryTask
	err := s.db.WithContext(tenant.System(ctx)).Where("share_hash = ?", hash).First(&item).Error
	return item, persistenceError(err)
}

// DeliveryTransaction 先校验租户和创建者，再锁住任务；回调只获得此任务的资产和轮次。
// 写入时仅更新已校验记录或插入新业务 UUID，不允许把其他任务的照片搬入当前任务。
func (s *PostgresStore) DeliveryTransaction(ctx context.Context, id uuid.UUID, write bool, action func(*DeliveryBundle) error) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	return persistenceError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var b DeliveryBundle
		q := tx.Where("user_id = ? AND business_id = ?", scope.UserID, id)
		if write {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := q.First(&b.Task).Error; err != nil {
			return persistenceError(err)
		}
		if err := tx.Where("user_id = ? AND task_id = ?", scope.UserID, id).Order("created_at, business_id").Find(&b.Photos).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND task_id = ?", scope.UserID, id).Order("number").Find(&b.Rounds).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND task_id = ?", scope.UserID, id).Order("created_at DESC").Limit(100).Find(&b.Events).Error; err != nil {
			return err
		}
		// 保存值副本，只回写本次实际变化的记录，避免每次客户选片更新全部照片。
		originalPhotos := make(map[uuid.UUID]model.DeliveryPhoto, len(b.Photos))
		originalRounds := make(map[uuid.UUID]model.DeliveryRound, len(b.Rounds))
		for _, p := range b.Photos {
			originalPhotos[p.BusinessID] = p
		}
		for _, r := range b.Rounds {
			originalRounds[r.BusinessID] = r
		}
		if err := action(&b); err != nil {
			return err
		}
		if !write {
			return nil
		}
		// 使用显式字段更新，禁止 Save 对不存在记录执行隐式 upsert。
		if err := tx.Model(&model.DeliveryTask{}).Where("business_id = ? AND user_id = ?", id, scope.UserID).Updates(map[string]any{"title": b.Task.Title, "customer": b.Task.Customer, "description": b.Task.Description, "internal_note": b.Task.InternalNote, "stage": b.Task.Stage, "status": b.Task.Status, "version": b.Task.Version, "limit_mode": b.Task.LimitMode, "selection_limit": b.Task.SelectionLimit, "deadline": b.Task.Deadline, "expires_at": b.Task.ExpiresAt, "allow_original": b.Task.AllowOriginal, "allow_download": b.Task.AllowDownload, "share_hash": b.Task.ShareHash, "access_hash": b.Task.AccessHash, "current_round": b.Task.CurrentRound, "received_at": b.Task.ReceivedAt}).Error; err != nil {
			return err
		}
		for _, p := range b.Photos {
			if previous, ok := originalPhotos[p.BusinessID]; ok && reflect.DeepEqual(previous, p) {
				continue
			}
			if p.TaskID != id || p.UserID != scope.UserID {
				return fault.NotFound()
			}
			if p.ID == 0 {
				if err := tx.Create(&p).Error; err != nil {
					return err
				}
			} else if err := tx.Model(&model.DeliveryPhoto{}).Where("business_id = ? AND task_id = ? AND user_id = ?", p.BusinessID, id, scope.UserID).Updates(map[string]any{"mark": p.Mark, "group_name": p.GroupName, "source_id": p.SourceID, "published_batch": p.PublishedBatch}).Error; err != nil {
				return err
			}
		}
		for _, r := range b.Rounds {
			if previous, ok := originalRounds[r.BusinessID]; ok && reflect.DeepEqual(previous, r) {
				continue
			}
			if r.TaskID != id || r.UserID != scope.UserID {
				return fault.NotFound()
			}
			if r.ID == 0 {
				if err := tx.Create(&r).Error; err != nil {
					return err
				}
			} else if err := tx.Model(&model.DeliveryRound{}).Where("business_id = ? AND task_id = ? AND user_id = ?", r.BusinessID, id, scope.UserID).Updates(map[string]any{"version": r.Version, "status": r.Status, "selected_ids": r.SelectedIDs, "notes": r.Notes, "submitted_at": r.SubmittedAt}).Error; err != nil {
				return err
			}
		}
		for _, e := range b.Events {
			if e.ID == 0 {
				if e.TaskID != id || e.UserID != scope.UserID {
					return fault.NotFound()
				}
				e.TaskVersion = b.Task.Version
				if err := tx.Create(&e).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}))
}
