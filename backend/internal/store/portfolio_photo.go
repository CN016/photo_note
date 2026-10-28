package store

import (
	"context"
	"github.com/google/uuid"
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
	if _, err = store.FindPortfolio(ctx, item.UserID, item.PortfolioID); err != nil {
		return err
	}
	return persistenceError(store.db.WithContext(ctx).Create(item).Error)
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
