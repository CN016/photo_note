package service

import (
	"context"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/tenant"
)

// FileOwnershipRepository 只登记服务端新上传文件的归属，不提供客户端认领既有 key 的能力。
type FileOwnershipRepository interface {
	RegisterFileOwnership(context.Context, string, uuid.UUID) error
	CanAccessFile(context.Context, string, uuid.UUID) (bool, error)
}

// FileBackend 是物理存储适配器；业务必须经 FileService 校验组织和用户后才能访问私有内容。
type FileBackend interface {
	Store(context.Context, string, string, io.Reader, int64) (string, error)
	StorePublic(context.Context, string, string, io.Reader, int64) (string, error)
	Open(string) (io.ReadCloser, int64, string, error)
	Replace(context.Context, string, []byte) error
	Delete(context.Context, string) error
}

// FileService 将数据库登记的组织及用户归属作为私有文件的可信授权边界。
type FileService struct {
	backend         FileBackend
	ownership       FileOwnershipRepository
	thumbnailConfig func(context.Context) (ThumbnailConfig, error)
	thumbnailSlots  chan struct{}
	thumbnailLocks  storeKeyLocks
}

// NewFileService 包装物理存储；启动装配必须将此服务注入所有读取私有文件的业务。
func NewFileService(backend FileBackend, ownership FileOwnershipRepository) *FileService {
	return &FileService{backend: backend, ownership: ownership}
}

// Store 在可信作用域内上传并登记归属；登记失败时回收新文件，避免生成无主可用文件。
func (service *FileService) Store(ctx context.Context, storeType, name string, source io.Reader, maximum int64) (string, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return "", err
	}
	if service == nil || service.backend == nil || service.ownership == nil {
		return "", fault.Unavailable(errors.New("文件归属服务未初始化"))
	}
	key, err := service.backend.Store(ctx, storeType, name, source, maximum)
	if err != nil {
		return "", err
	}
	if err := service.ownership.RegisterFileOwnership(ctx, key, scope.UserID); err != nil {
		_ = service.backend.Delete(context.WithoutCancel(ctx), key)
		return "", err
	}
	return key, nil
}

// StorePublic 保留反馈专用公开目录行为；公共上传只能由既有反馈业务调用。
func (service *FileService) StorePublic(ctx context.Context, storeType, name string, source io.Reader, maximum int64) (string, error) {
	if service == nil || service.backend == nil {
		return "", fault.Unavailable(errors.New("文件存储未初始化"))
	}
	return service.backend.StorePublic(ctx, storeType, name, source, maximum)
}

// AuthorizePrivateFile 拒绝未登记、归属未知、其他组织或其他用户的私有 key，统一隐藏资源存在性。
func (service *FileService) AuthorizePrivateFile(ctx context.Context, key string) error {
	parsed, err := filestore.ParseKey(key)
	if err != nil || parsed.Public {
		return fault.NotFound()
	}
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if service == nil || service.backend == nil || service.ownership == nil {
		return fault.Unavailable(errors.New("文件归属服务未初始化"))
	}
	allowed, err := service.ownership.CanAccessFile(ctx, key, scope.UserID)
	if err != nil {
		return err
	}
	if !allowed {
		return fault.NotFound()
	}
	return nil
}

// OpenContext 在打开私有内容前强制检查作用域；公开反馈 key 保持原公开能力。
func (service *FileService) OpenContext(ctx context.Context, key string) (io.ReadCloser, int64, string, error) {
	parsed, err := filestore.ParseKey(key)
	if err != nil {
		return nil, 0, "", filestore.ErrInvalidStoreKey
	}
	if !parsed.Public {
		if err := service.AuthorizePrivateFile(ctx, key); err != nil {
			return nil, 0, "", err
		}
	}
	if service == nil || service.backend == nil {
		return nil, 0, "", fault.NotFound()
	}
	return service.backend.Open(key)
}

// Open 仅兼容无需认证的公开反馈图片；缺少上下文的私有读取一律拒绝。
func (service *FileService) Open(key string) (io.ReadCloser, int64, string, error) {
	parsed, err := filestore.ParseKey(key)
	if err != nil || !parsed.Public {
		return nil, 0, "", fault.NotFound()
	}
	return service.OpenContext(context.Background(), key)
}

// Replace 在覆盖文件前重新检查租户及用户，防止异步任务使用伪造 key 修改其他资源。
func (service *FileService) Replace(ctx context.Context, key string, content []byte) error {
	if err := service.AuthorizePrivateFile(ctx, key); err != nil {
		return err
	}
	// 与缩略图生成互斥，避免读取覆盖中间态或删除后遗留刚生成的副本。
	unlock := service.thumbnailLocks.acquire(key)
	defer unlock()
	return service.backend.Replace(ctx, key, content)
}

// Delete 对私有文件执行与读取相同的归属检查，公开反馈文件只用于失败上传回收。
func (service *FileService) Delete(ctx context.Context, key string) error {
	parsed, err := filestore.ParseKey(key)
	if err != nil {
		return err
	}
	if !parsed.Public {
		if err := service.AuthorizePrivateFile(ctx, key); err != nil {
			return err
		}
	}
	if service == nil || service.backend == nil {
		return fault.NotFound()
	}
	unlock := service.thumbnailLocks.acquire(key)
	defer unlock()
	return service.backend.Delete(ctx, key)
}

// OpenFile 保留上下文贯穿业务读取；无作用域的旧测试适配器仍可提供最小 Open 接口。
func OpenFile(ctx context.Context, files interface {
	Open(string) (io.ReadCloser, int64, string, error)
}, key string) (io.ReadCloser, int64, string, error) {
	if contextual, ok := files.(interface {
		OpenContext(context.Context, string) (io.ReadCloser, int64, string, error)
	}); ok {
		return contextual.OpenContext(ctx, key)
	}
	if _, scoped := tenant.FromContext(ctx); scoped {
		return nil, 0, "", fault.Unavailable(errors.New("私有文件存储未装配租户授权"))
	}
	return files.Open(key)
}

// authorizeFileReference 在保存客户端文件引用前检查权限，防止登记后的业务关联被用来绕过上传归属。
func authorizeFileReference(ctx context.Context, files any, key string) error {
	if authorizer, ok := files.(interface {
		AuthorizePrivateFile(context.Context, string) error
	}); ok {
		return authorizer.AuthorizePrivateFile(ctx, key)
	}
	if _, scoped := tenant.FromContext(ctx); scoped {
		return fault.Unavailable(errors.New("私有文件存储未装配租户授权"))
	}
	return nil
}
