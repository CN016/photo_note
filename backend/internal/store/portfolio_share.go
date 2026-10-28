package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

// SetPortfolioShareHash 仅修改当前空间中创建者的作品集，nil 表示撤销分享。
func (store *PostgresStore) SetPortfolioShareHash(ctx context.Context, userID, portfolioID uuid.UUID, hash *string) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if scope.UserID != userID {
		return fault.NotFound()
	}
	result := store.db.WithContext(ctx).Model(&model.Portfolio{}).Where("user_id = ? AND business_id = ?", userID, portfolioID).Update("share_token_hash", hash)
	if result.Error != nil {
		return persistenceError(result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

// FindPortfolioByShareHash 仅供公开分享入口按高熵令牌摘要定位，后续业务必须收窄至记录的租户。
func (store *PostgresStore) FindPortfolioByShareHash(ctx context.Context, hash string) (model.Portfolio, error) {
	if len(hash) != 64 {
		return model.Portfolio{}, fault.NotFound()
	}
	var item model.Portfolio
	err := store.db.WithContext(tenant.System(ctx)).Where("share_token_hash = ?", hash).First(&item).Error
	if err != nil {
		return model.Portfolio{}, persistenceError(err)
	}
	return item, nil
}
