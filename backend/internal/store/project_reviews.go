package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FindProjectReview 在验证来源策划后查找副本；不存在副本返回 nil，跨空间来源统一隐藏。
func (s *PostgresStore) FindProjectReview(ctx context.Context, user, project uuid.UUID) (*model.ProjectReview, error) {
	if err := s.requireActiveProject(ctx, user, project); err != nil {
		return nil, err
	}
	var item model.ProjectReview
	err := s.db.WithContext(ctx).Where("user_id = ? AND project_id = ?", user, project).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, persistenceError(err)
	}
	return &item, nil
}

// CreateProjectReview 锁定来源策划后幂等复制人工总结；同一来源不会因重试或并发创建多个文档。
// 已有副本优先返回；新建时 revision 必须匹配，防止复制未确认的迟到版本。
func (s *PostgresStore) CreateProjectReview(ctx context.Context, user, project uuid.UUID, revision int64, title string) (model.ProjectReview, error) {
	if err := projectUserBoundary(ctx, user); err != nil {
		return model.ProjectReview{}, err
	}
	var item model.ProjectReview
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var source model.Project
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", user, project).First(&source).Error; err != nil {
			return persistenceError(err)
		}
		err := tx.Where("user_id = ? AND project_id = ?", user, project).First(&item).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return persistenceError(err)
		}
		if source.Revision != revision {
			return fault.Conflict("策划已更新，请保存并重新完成总结")
		}
		if strings.TrimSpace(source.ManualSummary) == "" {
			return fault.InvalidField("manual_summary", "请先填写总结")
		}
		item = model.ProjectReview{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, ProjectID: project, Title: title, Content: source.ManualSummary, Revision: 1}
		return persistenceError(tx.Create(&item).Error)
	})
	return item, err
}

// ListProjectReviews 按空间与作者分页检索独立文档；列表不读取总结正文。
func (s *PostgresStore) ListProjectReviews(ctx context.Context, user uuid.UUID, query string, offset, limit int) ([]model.ProjectReview, int64, error) {
	if err := projectUserBoundary(ctx, user); err != nil {
		return nil, 0, err
	}
	base := s.db.WithContext(ctx).Model(&model.ProjectReview{}).Where("user_id = ?", user)
	if query != "" {
		base = base.Where("strpos(lower(title || ' ' || content), ?) > 0", strings.ToLower(query))
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	items := make([]model.ProjectReview, 0)
	err := base.Select("business_id", "project_id", "title", "revision", "created_at", "updated_at").Order("updated_at DESC, business_id ASC").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, persistenceError(err)
}

// GetProjectReview 来源策划即使被软删除，独立文档仍归原空间与作者所有。
func (s *PostgresStore) GetProjectReview(ctx context.Context, user, id uuid.UUID) (model.ProjectReview, error) {
	if err := projectUserBoundary(ctx, user); err != nil {
		return model.ProjectReview{}, err
	}
	var item model.ProjectReview
	err := s.db.WithContext(ctx).Where("user_id = ? AND business_id = ?", user, id).First(&item).Error
	return item, persistenceError(err)
}

// UpdateProjectReview 用独立修订号保护文档编辑，不修改来源策划或其状态。
func (s *PostgresStore) UpdateProjectReview(ctx context.Context, user, id uuid.UUID, revision int64, title, content string) (model.ProjectReview, error) {
	if err := projectUserBoundary(ctx, user); err != nil {
		return model.ProjectReview{}, err
	}
	var item model.ProjectReview
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ?", user, id).First(&item).Error; err != nil {
			return persistenceError(err)
		}
		if item.Revision != revision {
			return fault.Conflict("总结文档已更新，请重新加载")
		}
		item.Title, item.Content, item.Revision, item.UpdatedAt = title, content, item.Revision+1, time.Now().UTC()
		return persistenceError(tx.Model(&model.ProjectReview{}).Where("user_id = ? AND business_id = ?", user, id).Updates(map[string]any{"title": title, "content": content, "revision": item.Revision, "updated_at": item.UpdatedAt}).Error)
	})
	return item, err
}
