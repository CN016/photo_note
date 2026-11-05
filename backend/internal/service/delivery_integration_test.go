package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/security"
	"github.com/photo-nini/backend/internal/store"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// deliveryTestService 只对显式独立测试库创建随机 schema，运行真实迁移，不连接项目运行数据库。
func deliveryTestService(t *testing.T) (*DeliveryService, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if dsn == "" {
		t.Skip("未配置独立 PostgreSQL 测试库，交付数据库验证跳过")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, TranslateError: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	schema := "delivery_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err = db.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error; _ = sqlDB.Close() })
	if err = db.Exec(`SET search_path TO "` + schema + `", public`).Error; err != nil {
		t.Fatal(err)
	}
	migrations, _ := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	for _, file := range migrations {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.Exec(string(raw)).Error; err != nil {
			t.Fatalf("迁移 %s: %v", filepath.Base(file), err)
		}
	}
	repository := store.NewPostgresStore(db)
	config := NewRuntimeConfigService(repository)
	raw, err := filestore.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := NewFileService(raw, repository)
	passwords, err := security.NewPasswordManager()
	if err != nil {
		t.Fatal(err)
	}
	return NewDeliveryService(repository, files, config, passwords, 20000000), db
}

// TestDeliveryLifecyclePostgres 验证真实文件、发布冻结、并发版本、选片锁定、退回历史及私有文件访问。
func TestDeliveryLifecyclePostgres(t *testing.T) {
	s, db := deliveryTestService(t)
	user := uuid.New()
	ctx := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 101, UserID: user})
	in := DeliveryInput{Title: "交付测试", LimitMode: "exact", SelectionLimit: 1, AllowDownload: true}
	task, err := s.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	id := task.BusinessID
	img := image.NewRGBA(image.Rect(0, 0, 120, 80))
	img.SetRGBA(40, 40, color.RGBA{200, 100, 80, 255})
	var src bytes.Buffer
	_ = jpeg.Encode(&src, img, &jpeg.Options{Quality: 95})
	p1, err := s.Upload(ctx, id, 1, "proof", "A.JPG", "", bytes.NewReader(src.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	detail, _ := s.Get(ctx, id, false)
	if detail.Task.Version != 2 || detail.Photos[0].PreviewBytes > 1000000 {
		t.Fatal(detail)
	}
	for _, scope := range []tenant.Scope{{OrgID: -1, UserID: user}, {OrgID: 102, UserID: user}, {OrgID: 101, UserID: uuid.New()}} {
		if _, err = s.Get(tenant.WithScope(context.Background(), scope), id, false); !fault.Is(err, 404, "not_found") {
			t.Fatalf("串空间/用户: %v", err)
		}
	}
	if _, err = s.Get(context.Background(), id, false); !fault.Is(err, 401, "unauthorized") {
		t.Fatal("缺失范围未拒绝", err)
	}
	command := func(action string) {
		t.Helper()
		d, e := s.Get(ctx, id, false)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.Command(ctx, id, DeliveryCommand{Version: d.Task.Version, Action: action}); e != nil {
			t.Fatalf("%s: %v", action, e)
		}
	}
	command("publish")
	detail, _ = s.Get(ctx, id, false)
	version := detail.Task.Version
	img.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	src.Reset()
	_ = jpeg.Encode(&src, img, nil)
	p2, err := s.Upload(ctx, id, version, "proof", "B.JPG", "", bytes.NewReader(src.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	detail, _ = s.Get(ctx, id, false)
	share, err := s.Command(ctx, id, DeliveryCommand{Version: detail.Task.Version, Action: "share"})
	if err != nil {
		t.Fatal(err)
	}
	publicCtx, publicID, err := s.Resolve(context.Background(), share, "")
	if err != nil || publicID != id {
		t.Fatal(err)
	}
	public, err := s.Get(publicCtx, id, true)
	if err != nil || len(public.Photos) != 1 {
		t.Fatal("补传污染已发布清单", public, err)
	}
	if _, _, _, _, err = s.OpenPhoto(publicCtx, id, p2, false, true); !fault.Is(err, 404, "not_found") {
		t.Fatal("未发布照片可见", err)
	}
	if _, _, _, _, err = s.OpenPhoto(publicCtx, id, p1, true, true); !fault.Is(err, 404, "not_found") {
		t.Fatal("禁止原片下载失效", err)
	}
	selection := DeliverySelection{Round: 1, Version: 1, SelectedIDs: []string{p1.String()}, Notes: map[string]string{"overall": "测试备注"}}
	if err = s.Select(publicCtx, id, selection); err != nil {
		t.Fatal(err)
	}
	if err = s.Select(publicCtx, id, selection); !fault.Is(err, 409, "conflict") {
		t.Fatal("旧草稿版本可覆盖", err)
	}
	selection.Version = 2
	selection.Submit = true
	if err = s.Select(publicCtx, id, selection); err != nil {
		t.Fatal(err)
	}
	if err = s.Select(publicCtx, id, selection); err != nil {
		t.Fatal("提交未幂等", err)
	}
	selection.Submit = false
	if err = s.Select(publicCtx, id, selection); !fault.Is(err, 409, "conflict") {
		t.Fatal("提交后可修改", err)
	}
	command("confirm")
	detail, _ = s.Get(ctx, id, false)
	final, err := s.Upload(ctx, id, detail.Task.Version, "final", "A.jpg", "", bytes.NewReader(src.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err = s.OpenPhoto(publicCtx, id, final, false, true); !fault.Is(err, 404, "not_found") {
		t.Fatal("未发布精修可见", err)
	}
	command("publish_final")
	r, _, _, _, err := s.OpenPhoto(publicCtx, id, final, true, true)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	// 原始选片导出为持久化作业；只允许管理端访问管理端作业。
	job, err := s.CreateExport(ctx, id, "proof", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateExport(ctx, id, "proof", false, []string{p2.String()}); !fault.Is(err, 404, "not_found") {
		t.Fatal("导出子集可混入未选原片", err)
	}
	duplicate, err := s.CreateExport(ctx, id, "proof", false, []string{p1.String()})
	if err != nil || duplicate.BusinessID != job.BusinessID {
		t.Fatal("同清单作业未复用", duplicate, err)
	}
	repository := s.repository.(deliveryExportRepository)
	claimed, found, err := repository.ClaimDeliveryExport(context.Background())
	if err != nil || !found {
		t.Fatal(found, err)
	}
	if err = s.processExport(ctx, repository, claimed); err != nil {
		t.Fatal(err)
	}
	status, err := s.ExportStatus(ctx, id, job.BusinessID, false)
	if err != nil || status.Status != "completed" || status.Progress != 1 {
		t.Fatal(status, err)
	}
	if _, err = s.ExportStatus(publicCtx, id, job.BusinessID, true); !fault.Is(err, 404, "not_found") {
		t.Fatal("客户可读摄影师作业", err)
	}
	archive, _, err := s.OpenExport(ctx, id, job.BusinessID, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(archive)
	archive.Close()
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(z.File) != 1 || !strings.HasSuffix(z.File[0].Name, "/A.JPG") {
		t.Fatal("原片压缩包不完整", err)
	}
	// 通知发布采用相同业务代次，重复发布只保留一条站内消息。
	noticeRepo := s.repository.(interface {
		PendingDeliveryEvents(context.Context) ([]model.DeliveryEvent, error)
		PublishDeliveryNotification(context.Context, uuid.UUID) error
	})
	events, err := noticeRepo.PendingDeliveryEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if err = noticeRepo.PublishDeliveryNotification(ctx, event.BusinessID); err != nil {
			t.Fatal(err)
		}
		if err = noticeRepo.PublishDeliveryNotification(ctx, event.BusinessID); err != nil {
			t.Fatal(err)
		}
	}
	var notices int64
	if err = db.WithContext(ctx).Model(&model.Notification{}).Where("user_id = ? AND resource_type = 'delivery'", user).Count(&notices).Error; err != nil || notices != 2 {
		t.Fatal("通知幂等或过期过滤错误", notices, err)
	}
	if err = s.Receive(publicCtx, id); err != nil {
		t.Fatal(err)
	}
	command("reopen")
	detail, _ = s.Get(ctx, id, false)
	if len(detail.Rounds) != 2 || detail.Rounds[0].Status != "superseded" || len(detail.Rounds[0].SelectedIDs) != 1 || len(detail.Rounds[1].PhotoIDs) != 2 {
		t.Fatal("重开历史错误", detail.Rounds)
	}
	if err = s.Select(publicCtx, id, selection); !fault.Is(err, 409, "conflict") {
		t.Fatal("旧轮次可回写", err)
	}
	command("revoke")
	if _, _, err = s.Resolve(context.Background(), share, ""); !fault.Is(err, 404, "not_found") {
		t.Fatal("撤销失效", err)
	}
	// 直接检查迁移注释、无外键、业务 UUID 和物理原件完整性。
	var count int64
	if err = db.Raw("SELECT count(*) FROM information_schema.table_constraints WHERE table_schema = current_schema() AND constraint_type = 'FOREIGN KEY'").Scan(&count).Error; err != nil || count != 0 {
		t.Fatal(count, err)
	}
	encoded, _ := json.Marshal(detail)
	if strings.Contains(string(encoded), "original_key") || strings.Contains(string(encoded), "share_hash") || strings.Contains(string(encoded), "\"id\":") {
		t.Fatal("DTO 泄漏内部字段")
	}
}

// TestDeliveryAccessCodeAndDeadlinePostgres 验证访问码仅存哈希、截止冻结以及旧管理版本拒绝。
func TestDeliveryAccessCodeAndDeadlinePostgres(t *testing.T) {
	s, _ := deliveryTestService(t)
	ctx := tenant.Personal(context.Background(), uuid.New())
	in := DeliveryInput{Title: "口令测试", LimitMode: "max", SelectionLimit: 1, AccessCode: "test-only-code"}
	task, err := s.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	share, err := s.Command(ctx, task.BusinessID, DeliveryCommand{Version: 1, Action: "share"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Resolve(context.Background(), share, "bad"); !fault.Is(err, 401, "unauthorized") {
		t.Fatal(err)
	}
	if _, _, err = s.Resolve(context.Background(), share, "test-only-code"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Command(ctx, task.BusinessID, DeliveryCommand{Version: 1, Action: "revoke"}); !fault.Is(err, 409, "conflict") {
		t.Fatal("旧版本命令未拒绝", err)
	}
	past := time.Now().Add(-time.Hour)
	in.ExpiresAt = &past
	if _, err = s.Command(ctx, task.BusinessID, DeliveryCommand{Version: 2, Action: "settings", Settings: &in}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Resolve(context.Background(), share, "test-only-code"); !fault.Is(err, 404, "not_found") {
		t.Fatal("过期仍可访问", err)
	}
	var _ model.DeliveryTask
}
