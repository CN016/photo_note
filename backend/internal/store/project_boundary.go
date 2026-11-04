package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

// projectUserBoundary 禁止普通策划流程继承系统扫描权限；空间和创建者必须来自同一会话。
func projectUserBoundary(ctx context.Context, user uuid.UUID) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if tenant.IsSystem(ctx) || scope.UserID != user {
		return fault.Unauthorized()
	}
	return nil
}

// requireActiveProject 仅查询业务标识来核验关联入口，不为权限检查重复读取大文档正文。
func (s *PostgresStore) requireActiveProject(ctx context.Context, user, project uuid.UUID) error {
	if err := projectUserBoundary(ctx, user); err != nil {
		return err
	}
	var item model.Project
	return persistenceError(s.db.WithContext(ctx).Select("business_id").Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", user, project).First(&item).Error)
}
