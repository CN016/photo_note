package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
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
