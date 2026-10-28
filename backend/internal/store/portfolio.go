package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

// ListPortfolios 按可信空间和创建者读取作品集。
func (store *PostgresStore) ListPortfolios(ctx context.Context, user uuid.UUID) ([]model.Portfolio, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return nil, err
	}
	if scope.UserID != user {
		return nil, fault.NotFound()
	}
	var items []model.Portfolio
	err = store.db.WithContext(ctx).Where("user_id = ?", user).Order("updated_at DESC, business_id DESC").Find(&items).Error
	return items, persistenceError(err)
}

// FindPortfolio 依据空间、创建者和业务 UUID 查找作品集。
func (store *PostgresStore) FindPortfolio(ctx context.Context, user, id uuid.UUID) (model.Portfolio, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return model.Portfolio{}, err
	}
	if scope.UserID != user {
		return model.Portfolio{}, fault.NotFound()
	}
	var item model.Portfolio
	err = store.db.WithContext(ctx).Where("user_id = ? AND business_id = ?", user, id).First(&item).Error
	return item, persistenceError(err)
}

// CreatePortfolio 保存受信任服务构造的作品集；租户由数据库回调写入。
func (store *PostgresStore) CreatePortfolio(ctx context.Context, item *model.Portfolio) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if scope.UserID != item.UserID {
		return fault.NotFound()
	}
	return persistenceError(store.db.WithContext(ctx).Create(item).Error)
}
