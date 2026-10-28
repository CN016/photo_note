package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ListPortfolioPhotos 只读取当前作者所拥有作品集内的照片。
func (store *PostgresStore) ListPortfolioPhotos(ctx context.Context, user, id uuid.UUID) ([]model.PortfolioPhoto, error) {
	if _, err := store.FindPortfolio(ctx, user, id); err != nil {
		return nil, err
	}
	var items []model.PortfolioPhoto
	err := store.db.WithContext(ctx).Where("user_id = ? AND portfolio_id = ?", user, id).Order("sort_order ASC, created_at ASC, business_id ASC").Find(&items).Error
	return items, persistenceError(err)
}

// CreatePortfolioPhoto 限定可信空间及已授权的作品集；调用方负责新文件失败回收。
func (store *PostgresStore) CreatePortfolioPhoto(ctx context.Context, item *model.PortfolioPhoto) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if scope.UserID != item.UserID {
		return fault.NotFound()
	}
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parent model.Portfolio
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ?", item.UserID, item.PortfolioID).First(&parent).Error; err != nil {
			return persistenceError(err)
		}
		return persistenceError(tx.Create(item).Error)
	})
}

// DeletePortfolioPhoto 在行锁保护下删除公开文件并移除照片记录；文件失败时保留记录供重试。
func (store *PostgresStore) DeletePortfolioPhoto(ctx context.Context, user, portfolioID, photoID uuid.UUID, remove func(string) error) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if scope.UserID != user {
		return fault.NotFound()
	}
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var photo model.PortfolioPhoto
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND portfolio_id = ? AND business_id = ?", user, portfolioID, photoID).First(&photo).Error; err != nil {
			return persistenceError(err)
		}
		if err := remove(photo.ImageKey); err != nil {
			return err
		}
		result := tx.Where("user_id = ? AND portfolio_id = ? AND business_id = ?", user, portfolioID, photoID).Delete(&model.PortfolioPhoto{})
		if result.Error != nil {
			return persistenceError(result.Error)
		}
		if result.RowsAffected != 1 {
			return fault.NotFound()
		}
		return nil
	})
}

// UpdatePortfolioPhoto 校验标签归属后更新照片标题、标签和排序值。
func (store *PostgresStore) UpdatePortfolioPhoto(ctx context.Context, user, portfolioID, photoID uuid.UUID, title string, tags []uuid.UUID, order int64) (model.PortfolioPhoto, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return model.PortfolioPhoto{}, err
	}
	if scope.UserID != user {
		return model.PortfolioPhoto{}, fault.NotFound()
	}
	var photo model.PortfolioPhoto
	err = store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND portfolio_id = ? AND business_id = ?", user, portfolioID, photoID).First(&photo).Error; err != nil {
			return persistenceError(err)
		}

		if len(tags) > 0 {
			var rows []model.Tag
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("business_id").Where("user_id = ? AND business_id IN ?", user, tags).Find(&rows).Error; err != nil {
				return persistenceError(err)
			}
			if len(rows) != len(tags) {
				return fault.InvalidField("tag_ids", "标签不存在或不属于当前用户")
			}
		}

		values := pq.StringArray{}
		for _, id := range tags {
			values = append(values, id.String())
		}
		if err := tx.Model(&model.PortfolioPhoto{}).Where("user_id = ? AND portfolio_id = ? AND business_id = ?", user, portfolioID, photoID).Updates(map[string]any{"title": title, "tag_ids": values, "sort_order": order}).Error; err != nil {
			return persistenceError(err)
		}
		return persistenceError(tx.Where("user_id = ? AND portfolio_id = ? AND business_id = ?", user, portfolioID, photoID).First(&photo).Error)
	})
	return photo, err
}

// DeletePortfolio 在锁定作品集后清理所有公开副本及关联；失败回滚记录，允许重试。
func (store *PostgresStore) DeletePortfolio(ctx context.Context, user, id uuid.UUID, remove func(string) error) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if scope.UserID != user {
		return fault.NotFound()
	}
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parent model.Portfolio
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ?", user, id).First(&parent).Error; err != nil {
			return persistenceError(err)
		}
		var photos []model.PortfolioPhoto
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND portfolio_id = ?", user, id).Find(&photos).Error; err != nil {
			return persistenceError(err)
		}
		for _, photo := range photos {
			if err := remove(photo.ImageKey); err != nil {
				return err
			}
		}
		if err := tx.Where("user_id = ? AND portfolio_id = ?", user, id).Delete(&model.PortfolioPhoto{}).Error; err != nil {
			return persistenceError(err)
		}
		if err := tx.Where("user_id = ? AND portfolio_id = ?", user, id).Delete(&model.PortfolioProject{}).Error; err != nil {
			return persistenceError(err)
		}
		return persistenceError(tx.Where("user_id = ? AND business_id = ?", user, id).Delete(&model.Portfolio{}).Error)
	})
}
