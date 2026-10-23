package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

// fileOwnershipMemory 在测试中独立模拟登记存储，既检查组织也检查用户，不能靠 key 随机性授权。
type fileOwnershipMemory struct {
	items       map[string]tenant.Scope
	registerErr error
	lastKey     string
}

func (repository *fileOwnershipMemory) RegisterFileOwnership(ctx context.Context, key string, userID uuid.UUID) error {
	repository.lastKey = key
	if repository.registerErr != nil {
		return repository.registerErr
	}
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if repository.items == nil {
		repository.items = make(map[string]tenant.Scope)
	}
	scope.UserID = userID
	repository.items[key] = scope
	return nil
}
func (repository *fileOwnershipMemory) CanAccessFile(ctx context.Context, key string, userID uuid.UUID) (bool, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return false, err
	}
	owner, exists := repository.items[key]
	return exists && owner.OrgID == scope.OrgID && owner.UserID != uuid.Nil && owner.UserID == userID, nil
}

// TestPrivateFileTenantAndUserIsolation 覆盖同用户跨组织、同组织跨用户和个人空间共用 -1 的隔离。
func TestPrivateFileTenantAndUserIsolation(t *testing.T) {
	raw, err := filestore.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository := &fileOwnershipMemory{}
	files := NewFileService(raw, repository)
	user, other := uuid.New(), uuid.New()
	ownerCtx := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 17, UserID: user})
	key, err := files.Store(ownerCtx, "local", "private.txt", bytes.NewBufferString("private data"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	reader, _, _, err := files.OpenContext(ownerCtx, key)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	for _, scope := range []tenant.Scope{{OrgID: 18, UserID: user}, {OrgID: 17, UserID: other}, {OrgID: -1, UserID: user}} {
		ctx := tenant.WithScope(context.Background(), scope)
		if _, _, _, err := files.OpenContext(ctx, key); !fault.Is(err, 404, "not_found") {
			t.Fatalf("跨组织或用户读取未拒绝：%+v %v", scope, err)
		}
		if err := files.Replace(ctx, key, []byte("tampered")); !fault.Is(err, 404, "not_found") {
			t.Fatalf("跨组织覆盖未拒绝：%v", err)
		}
		if err := files.Delete(ctx, key); !fault.Is(err, 404, "not_found") {
			t.Fatalf("跨组织删除未拒绝：%v", err)
		}
	}
	personal := tenant.Personal(context.Background(), user)
	personalKey, err := files.Store(personal, "local", "personal.txt", bytes.NewBufferString("mine"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := files.OpenContext(tenant.Personal(context.Background(), other), personalKey); !fault.Is(err, 404, "not_found") {
		t.Fatalf("个人 -1 共享值不应共享资源：%v", err)
	}
	if _, _, _, err := files.Open(key); !fault.Is(err, 404, "not_found") {
		t.Fatalf("无上下文读取私有文件未拒绝：%v", err)
	}
	if _, err := files.Store(context.Background(), "local", "anonymous.txt", bytes.NewBufferString("x"), 1024); !fault.Is(err, 401, "unauthorized") {
		t.Fatalf("无上下文上传未拒绝：%v", err)
	}
	delete(repository.items, key)
	if _, _, _, err := files.OpenContext(ownerCtx, key); !fault.Is(err, 404, "not_found") {
		t.Fatalf("未登记文件不应可读：%v", err)
	}
	repository.items[key] = tenant.Scope{OrgID: 17}
	if _, _, _, err := files.OpenContext(ownerCtx, key); !fault.Is(err, 404, "not_found") {
		t.Fatalf("未知所有者文件不应可读：%v", err)
	}
}

// TestPrivateUploadRegistrationFailureAndPublicCompatibility 验证登记失败回收私有文件，公开反馈图片保持原读取能力。
func TestPrivateUploadRegistrationFailureAndPublicCompatibility(t *testing.T) {
	raw, err := filestore.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository := &fileOwnershipMemory{registerErr: errors.New("database unavailable")}
	files := NewFileService(raw, repository)
	ctx := tenant.Personal(context.Background(), uuid.New())
	if _, err := files.Store(ctx, "local", "private.txt", bytes.NewBufferString("x"), 1024); err == nil {
		t.Fatal("登记失败仍返回成功")
	}
	if _, _, _, err := raw.Open(repository.lastKey); !errors.Is(err, filestore.ErrFileNotFound) {
		t.Fatalf("登记失败未回收物理文件：%v", err)
	}
	key, err := files.StorePublic(context.Background(), "local", "feedback.png", bytes.NewReader(tinyPNG(t)), 1024)
	if err != nil {
		t.Fatal(err)
	}
	reader, _, _, err := files.Open(key)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	reader, _, _, err = files.OpenContext(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
}

// tenantMetadataRepository 记录压缩与管理操作实际使用的组织用户作用域，避免测试只验证参数形状。
type tenantMetadataRepository struct {
	metadataTestRepository
	seen tenant.Scope
}

func (repository *tenantMetadataRepository) FindInspirationByBusinessID(ctx context.Context, userID, id uuid.UUID) (model.Inspiration, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return model.Inspiration{}, err
	}
	repository.seen = scope
	if scope.OrgID != repository.record.OrgID || scope.UserID != repository.record.UserID {
		return model.Inspiration{}, fault.NotFound()
	}
	return repository.metadataTestRepository.FindInspirationByBusinessID(ctx, userID, id)
}
func (repository *tenantMetadataRepository) FindConsoleInspiration(ctx context.Context, id uuid.UUID) (model.Inspiration, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return model.Inspiration{}, err
	}
	if id != repository.record.BusinessID || scope.OrgID != repository.record.OrgID {
		return model.Inspiration{}, fault.NotFound()
	}
	return repository.record, nil
}

func (repository *tenantMetadataRepository) UpdateInspiration(ctx context.Context, userID, id uuid.UUID, _ map[string]any) (model.Inspiration, error) {
	return repository.FindInspirationByBusinessID(ctx, userID, id)
}

func (repository *tenantMetadataRepository) DeleteInspiration(ctx context.Context, userID, id uuid.UUID) error {
	_, err := repository.FindInspirationByBusinessID(ctx, userID, id)
	return err
}

// TestManagedGalleryBindsOwnerAndResource 验证管理端可访问同组织目标图，但不能读取其所有者其他私有文件或其他组织图片。
func TestManagedGalleryBindsOwnerAndResource(t *testing.T) {
	raw, err := filestore.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner, admin := uuid.New(), uuid.New()
	ownerCtx := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 17, UserID: owner})
	files := NewFileService(raw, &fileOwnershipMemory{})
	key, err := files.Store(ownerCtx, "local", "image.png", bytes.NewReader(tinyPNG(t)), 1024)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := files.Store(ownerCtx, "local", "other.txt", bytes.NewBufferString("secret"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	repository := &tenantMetadataRepository{metadataTestRepository: metadataTestRepository{record: model.Inspiration{BaseModel: model.BaseModel{BusinessID: uuid.New(), OrgID: 17}, UserID: owner, ImageKey: key, Data: model.JSONB(`{}`)}}}
	service := NewInspirationService(repository, files, 1024)
	ctx := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 17, UserID: admin})
	item, err := service.GetManaged(ctx, repository.record.BusinessID)
	if err != nil || item.UserID != owner || repository.seen.UserID != owner {
		t.Fatalf("管理读取未保持所有者：%+v %v", repository.seen, err)
	}
	reader, _, _, err := service.OpenManagedFile(ctx, item.BusinessID, key)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()
	if _, _, _, err := service.OpenManagedFile(ctx, item.BusinessID, otherKey); !fault.Is(err, 404, "not_found") {
		t.Fatalf("管理图库不应读取未关联私有文件：%v", err)
	}
	foreign := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 18, UserID: admin})
	if _, err := service.GetManaged(foreign, item.BusinessID); !fault.Is(err, 404, "not_found") {
		t.Fatalf("管理入口跨组织读取未拒绝：%v", err)
	}
	if _, err := service.UpdateManaged(ctx, item.BusinessID, InspirationInput{Data: []byte(`{"schema_version":1}`)}, false, false, false, false, true, false); err != nil || repository.seen.UserID != owner {
		t.Fatalf("管理编辑未使用原所有者：%+v %v", repository.seen, err)
	}
	if err := service.DeleteManaged(ctx, item.BusinessID); err != nil || repository.seen.UserID != owner {
		t.Fatalf("管理删除未使用原所有者：%+v %v", repository.seen, err)
	}
}

// TestCompressionWorkerRestoresCapturedTenant 验证后台重建上下文后仍按入队组织和用户读取、更新文件。
func TestCompressionWorkerRestoresCapturedTenant(t *testing.T) {
	raw, err := filestore.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	user := uuid.New()
	ctx := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 23, UserID: user})
	files := NewFileService(raw, &fileOwnershipMemory{})
	key, err := files.Store(ctx, "local", "image.png", bytes.NewReader(tinyPNG(t)), 1024)
	if err != nil {
		t.Fatal(err)
	}
	repository := &tenantMetadataRepository{metadataTestRepository: metadataTestRepository{record: model.Inspiration{BaseModel: model.BaseModel{BusinessID: uuid.New(), OrgID: 23}, UserID: user, ImageKey: key, AsyncVersion: 3, Data: model.JSONB(`{}`)}}}
	service := NewInspirationService(repository, files, 1024)
	service.readImageMetadata = func(context.Context, []byte) (model.InspirationImageMetadata, error) {
		return model.InspirationImageMetadata{}, nil
	}
	scope, _ := tenant.FromContext(ctx)
	job := inspirationCompressionJob{scope: scope, scoped: true, userID: user, inspirationID: repository.record.BusinessID, imageKey: key, asyncVersion: 3}
	if err := service.processCompressionJob(job); err != nil {
		t.Fatal(err)
	}
	if repository.seen.OrgID != 23 || repository.seen.UserID != user || repository.fileMetadataWrites != 1 {
		t.Fatalf("后台任务丢失租户或未完成写回：scope=%+v writes=%d", repository.seen, repository.fileMetadataWrites)
	}
	job.scope.OrgID = 24
	if err := service.processCompressionJob(job); err != nil {
		t.Fatalf("已无法访问的任务应按过期任务丢弃：%v", err)
	}
	if repository.fileMetadataWrites != 1 {
		t.Fatal("错误组织任务仍写回了元数据")
	}
}

// TestEditorRejectsForeignSourceReference 验证编辑文档中隐藏的原图引用也必须属于当前组织和用户。
func TestEditorRejectsForeignSourceReference(t *testing.T) {
	raw, err := filestore.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := NewFileService(raw, &fileOwnershipMemory{})
	user := uuid.New()
	foreign := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 18, UserID: user})
	key, err := files.Store(foreign, "local", "source.png", bytes.NewReader(tinyPNG(t)), 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 17, UserID: user})
	service := NewInspirationService(&metadataTestRepository{}, files, 1024)
	data := []byte(`{"schema_version":1,"source_image_key":"` + key + `"}`)
	if _, err := service.Create(ctx, user, InspirationInput{ImageKey: "sample-image", Data: data}); !fault.Is(err, 404, "not_found") {
		t.Fatalf("伪造跨组织原图引用未拒绝：%v", err)
	}
}
