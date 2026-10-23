package service

import (
	"context"
	"io"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

// managedInspirationScope 只供已通过图库管理中间件的入口使用。先在选定组织定位记录，
// 再以记录真实所有者执行现有私有业务规则；不会赋予管理员访问其他私有业务的权限。
func (service *InspirationService) managedInspirationScope(ctx context.Context, inspirationID uuid.UUID) (context.Context, model.Inspiration, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return ctx, model.Inspiration{}, err
	}
	repository, ok := service.inspirations.(interface {
		FindConsoleInspiration(context.Context, uuid.UUID) (model.Inspiration, error)
	})
	if !ok {
		return ctx, model.Inspiration{}, fault.Unavailable(nil)
	}
	item, err := repository.FindConsoleInspiration(ctx, inspirationID)
	if err != nil {
		return ctx, model.Inspiration{}, err
	}
	if item.OrgID != scope.OrgID || item.UserID == uuid.Nil {
		return ctx, model.Inspiration{}, fault.NotFound()
	}
	scope.UserID = item.UserID
	return tenant.WithScope(ctx, scope), item, nil
}

// GetManaged 返回当前管理组织内的灵感图详情。调用入口必须先验证组织管理员或平台管理员角色。
func (service *InspirationService) GetManaged(ctx context.Context, inspirationID uuid.UUID) (model.Inspiration, error) {
	ownerCtx, item, err := service.managedInspirationScope(ctx, inspirationID)
	if err != nil {
		return model.Inspiration{}, err
	}
	return service.Get(ownerCtx, item.UserID, inspirationID)
}

// UpdateManaged 复用图像编辑和标签校验，以原所有者保留资源归属和后台压缩任务范围。
func (service *InspirationService) UpdateManaged(ctx context.Context, inspirationID uuid.UUID, input InspirationInput, tagIDsSet, tagNamesSet, imageKeySet, fileNameSet, dataSet, imageMetadataSet bool) (model.Inspiration, error) {
	ownerCtx, item, err := service.managedInspirationScope(ctx, inspirationID)
	if err != nil {
		return model.Inspiration{}, err
	}
	return service.Update(ownerCtx, item.UserID, inspirationID, input, tagIDsSet, tagNamesSet, imageKeySet, fileNameSet, dataSet, imageMetadataSet)
}

// DeleteManaged 仅删除当前管理组织内的目标图片，仍遵循项目引用保护与所有者业务约束。
func (service *InspirationService) DeleteManaged(ctx context.Context, inspirationID uuid.UUID) error {
	ownerCtx, item, err := service.managedInspirationScope(ctx, inspirationID)
	if err != nil {
		return err
	}
	return service.Delete(ownerCtx, item.UserID, inspirationID)
}

// OpenManagedFile 将管理端文件读取绑定当前图片实际引用，禁止把图库管理变成任意用户文件读取入口。
func (service *InspirationService) OpenManagedFile(ctx context.Context, inspirationID uuid.UUID, key string) (io.ReadCloser, int64, string, error) {
	ownerCtx, item, err := service.managedInspirationScope(ctx, inspirationID)
	if err != nil {
		return nil, 0, "", err
	}
	if key == "" || (key != item.ImageKey && key != sourceImageKeyFromData(item.Data)) {
		return nil, 0, "", fault.NotFound()
	}
	return OpenFile(ownerCtx, service.files, key)
}

// StoreManagedFile 将编辑中的新文件登记给原图片所有者，不改变图片或文件所属组织与用户。
func (service *InspirationService) StoreManagedFile(ctx context.Context, inspirationID uuid.UUID, storeType, name string, source io.Reader, maximum int64) (string, error) {
	ownerCtx, _, err := service.managedInspirationScope(ctx, inspirationID)
	if err != nil {
		return "", err
	}
	files, ok := service.files.(interface {
		Store(context.Context, string, string, io.Reader, int64) (string, error)
	})
	if !ok {
		return "", fault.Unavailable(nil)
	}
	return files.Store(ownerCtx, storeType, name, source, maximum)
}

// ListManagedTags 为管理编辑器提供原图片用户的标签建议，标签查询仍被限定在同一组织。
func (service *InspirationService) ListManagedTags(ctx context.Context, inspirationID uuid.UUID, query string, page PageRequest) (Page[model.Tag], error) {
	ownerCtx, item, err := service.managedInspirationScope(ctx, inspirationID)
	if err != nil {
		return Page[model.Tag]{}, err
	}
	repository, ok := service.inspirations.(TagRepository)
	if !ok {
		return Page[model.Tag]{}, fault.Unavailable(nil)
	}
	return NewTagService(repository).List(ownerCtx, item.UserID, query, page)
}
