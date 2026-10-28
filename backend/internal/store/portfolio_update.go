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
	"time"
)

// UpdatePortfolio 在锁定作品集的事务中核验标签和策划归属，并同步关联。
func (store *PostgresStore) UpdatePortfolio(ctx context.Context, user, id uuid.UUID, title, description string, tags []uuid.UUID, projects []uuid.UUID) (model.Portfolio, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return model.Portfolio{}, err
	}
	if scope.UserID != user {
		return model.Portfolio{}, fault.NotFound()
	}
	var item model.Portfolio
	err = store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

		if len(tags) > 0 {
			var rows []model.Tag
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("business_id").Where("user_id = ? AND business_id IN ?", user, tags).Find(&rows).Error; err != nil {
				return persistenceError(err)
			}
			if len(rows) != len(tags) {
				return fault.InvalidField("tag_ids", "标签不存在或不属于当前用户")
			}
		}
		if len(projects) > 0 {
			var rows []model.Project
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("business_id").Where("user_id = ? AND business_id IN ? AND deleted_at IS NULL", user, projects).Find(&rows).Error; err != nil {
				return persistenceError(err)
			}
			if len(rows) != len(projects) {
				return fault.InvalidField("project_ids", "策划不存在或不属于当前用户")
			}
		}

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ?", user, id).First(&item).Error; err != nil {
			return persistenceError(err)
		}

		tagStrings := make(pq.StringArray, 0, len(tags))
		for _, tag := range tags {
			tagStrings = append(tagStrings, tag.String())
		}
		if err := tx.Model(&model.Portfolio{}).Where("user_id = ? AND business_id = ?", user, id).Updates(map[string]any{"title": title, "description": description, "tag_ids": tagStrings, "updated_at": time.Now().UTC()}).Error; err != nil {
			return persistenceError(err)
		}
		if err := tx.Where("user_id = ? AND portfolio_id = ?", user, id).Delete(&model.PortfolioProject{}).Error; err != nil {
			return persistenceError(err)
		}
		for _, project := range projects {
			association := model.PortfolioProject{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, PortfolioID: id, ProjectID: project}
			if err := tx.Create(&association).Error; err != nil {
				return persistenceError(err)
			}
		}
		return persistenceError(tx.Where("user_id = ? AND business_id = ?", user, id).First(&item).Error)
	})
	return item, err
}

// ListPortfolioProjects 查询作者空间中作品集与策划的关联。
func (store *PostgresStore) ListPortfolioProjects(ctx context.Context, user, id uuid.UUID) ([]model.PortfolioProject, error) {
	if _, err := store.FindPortfolio(ctx, user, id); err != nil {
		return nil, err
	}
	var items []model.PortfolioProject
	err := store.db.WithContext(ctx).Where("user_id = ? AND portfolio_id = ?", user, id).Find(&items).Error
	return items, persistenceError(err)
}
