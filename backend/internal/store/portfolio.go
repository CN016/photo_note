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
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(item.TagIDs) > 0 {
			var tags []model.Tag
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("business_id").Where("user_id = ? AND business_id IN ?", item.UserID, []string(item.TagIDs)).Find(&tags).Error; err != nil {
				return persistenceError(err)
			}
			if len(tags) != len(item.TagIDs) {
				return fault.InvalidField("tag_ids", "标签不存在或不属于当前用户")
			}
		}
		return persistenceError(tx.Create(item).Error)
	})
}

// PortfolioTagLabels 仅返回作品集中实际使用且仍由当前作者拥有的标签名称。
func (store *PostgresStore) PortfolioTagLabels(ctx context.Context, user uuid.UUID, ids []uuid.UUID) ([]model.Tag, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return nil, err
	}
	if scope.UserID != user {
		return nil, fault.NotFound()
	}
	if len(ids) == 0 {
		return []model.Tag{}, nil
	}
	var tags []model.Tag
	err = store.db.WithContext(ctx).Where("user_id = ? AND business_id IN ?", user, ids).Find(&tags).Error
	return tags, persistenceError(err)
}

// ListPortfolioCovers 按空间、用户及作品集范围批量获取首图，避免列表逐条查询。
func (store *PostgresStore) ListPortfolioCovers(ctx context.Context, user uuid.UUID, ids []uuid.UUID) ([]model.PortfolioCover, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return nil, err
	}
	if scope.UserID != user {
		return nil, fault.NotFound()
	}
	if len(ids) == 0 {
		return []model.PortfolioCover{}, nil
	}
	var covers []model.PortfolioCover
	err = store.db.WithContext(ctx).Model(&model.PortfolioPhoto{}).Select("DISTINCT ON (portfolio_id) portfolio_id, image_key").Where("user_id = ? AND portfolio_id IN ?", user, ids).Order("portfolio_id, sort_order, created_at, business_id").Scan(&covers).Error
	return covers, persistenceError(err)
}
