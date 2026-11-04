package store

import (
	"context"
	"sort"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ListProjectPortfolios 复用既有关系表，并同时校验关联两端，防止错误关联扩大私有数据范围。
func (s *PostgresStore) ListProjectPortfolios(ctx context.Context, user, project uuid.UUID) ([]model.Portfolio, error) {
	if err := s.requireActiveProject(ctx, user, project); err != nil {
		return nil, err
	}
	scope, err := tenant.Require(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.Portfolio, 0)
	err = s.db.WithContext(ctx).Model(&model.Portfolio{}).Where("portfolios.user_id = ?", user).
		Where("EXISTS (SELECT 1 FROM portfolio_projects x WHERE x.orgid = ? AND x.user_id = ? AND x.portfolio_id = portfolios.business_id AND x.project_id = ?)", scope.OrgID, user, project).
		Order("updated_at DESC, business_id ASC").Find(&items).Error
	return items, persistenceError(err)
}

// SetProjectPortfolios 只替换当前策划的关联，保留作品集的其他策划，所有资源均要求同空间同作者。
func (s *PostgresStore) SetProjectPortfolios(ctx context.Context, user, project uuid.UUID, ids []uuid.UUID) error {
	if err := projectUserBoundary(ctx, user); err != nil {
		return err
	}
	ordered := append([]uuid.UUID(nil), ids...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].String() < ordered[j].String() })
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var source model.Project
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("business_id").Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", user, project).First(&source).Error; err != nil {
			return persistenceError(err)
		}
		if len(ordered) > 0 {
			var found []model.Portfolio
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("business_id").Where("user_id = ? AND business_id IN ?", user, ordered).Order("business_id").Find(&found).Error; err != nil {
				return persistenceError(err)
			}
			if len(found) != len(ordered) {
				return fault.InvalidField("portfolio_ids", "作品集不存在或不属于当前用户与空间")
			}
		}
		if err := tx.Where("user_id = ? AND project_id = ?", user, project).Delete(&model.PortfolioProject{}).Error; err != nil {
			return persistenceError(err)
		}
		for _, id := range ordered {
			link := model.PortfolioProject{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, PortfolioID: id, ProjectID: project}
			if err := tx.Create(&link).Error; err != nil {
				return persistenceError(err)
			}
		}
		return nil
	})
}
