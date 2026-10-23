package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestTenantStorePostgres 验证真实 PostgreSQL 上的组织、用户私有、索引冲突、文件归属及成员事务边界。
func TestTenantStorePostgres(t *testing.T) {
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if dsn == "" {
		t.Skip("设置独立迁移测试库 PHOTO_NINI_TENANT_TEST_DSN 后运行多租户集成测试")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, TranslateError: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	repository := NewPostgresStore(db)
	global := tenant.System(context.Background())
	users := make([]model.User, 2)
	for index := range users {
		users[index] = model.User{BaseModel: model.BaseModel{BusinessID: uuid.New()}, Nickname: fmt.Sprintf("租户测试%d", index), Phone: fmt.Sprintf("17%09d", time.Now().UnixNano()%1000000000+int64(index)), PasswordHash: "integration-test-placeholder"}
		if err := repository.CreateUser(global, &users[index]); err != nil {
			t.Fatal(err)
		}
	}
	orgA := model.Organization{Name: "集成组织甲", Status: "active"}
	orgB := model.Organization{Name: "集成组织乙", Status: "active"}
	if err := repository.CreateOrganization(global, &orgA, users[0].BusinessID); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateOrganization(global, &orgB, users[0].BusinessID); err != nil {
		t.Fatal(err)
	}
	ctxA := tenant.WithScope(context.Background(), tenant.Scope{OrgID: orgA.OrgID, UserID: users[0].BusinessID})
	ctxB := tenant.WithScope(context.Background(), tenant.Scope{OrgID: orgB.OrgID, UserID: users[0].BusinessID})
	personal := tenant.Personal(context.Background(), users[0].BusinessID)
	for _, ctx := range []context.Context{ctxA, ctxB, personal} {
		item := model.Tag{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: users[0].BusinessID, Content: "同名标签"}
		if err := repository.CreateTag(ctx, &item); err != nil {
			t.Fatalf("跨空间同名唯一索引错误: %v", err)
		}
	}
	items, _, err := repository.ListTags(ctxA, users[0].BusinessID, "", 0, 20)
	if err != nil || len(items) != 1 || items[0].OrgID != orgA.OrgID {
		t.Fatalf("组织标签串租户: %#v %v", items, err)
	}
	if _, err := repository.FindTagByBusinessID(ctxB, users[0].BusinessID, items[0].BusinessID); !fault.Is(err, 404, "not_found") {
		t.Fatalf("跨组织详情未隐藏: %v", err)
	}
	if _, _, err := repository.ListTags(context.Background(), users[0].BusinessID, "", 0, 20); !fault.Is(err, 401, "unauthorized") {
		t.Fatalf("缺失范围未fail closed: %v", err)
	}
	other := model.Tag{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: users[1].BusinessID, Content: "成员私有"}
	if err := repository.CreateTag(ctxA, &other); err != nil {
		t.Fatal(err)
	}
	items, _, err = repository.ListTags(ctxA, users[0].BusinessID, "", 0, 20)
	if err != nil || len(items) != 1 {
		t.Fatalf("同组织其他成员资料可见: count=%d err=%v", len(items), err)
	}
	otherPersonal := model.Tag{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: users[1].BusinessID, Content: "另一人个人版"}
	if err := repository.CreateTag(tenant.Personal(context.Background(), users[1].BusinessID), &otherPersonal); err != nil {
		t.Fatal(err)
	}
	if personalItems, _, err := repository.ListTags(personal, users[0].BusinessID, "", 0, 20); err != nil || len(personalItems) != 1 {
		t.Fatalf("个人-1域跨用户可见: %#v %v", personalItems, err)
	}
	imageA := model.Inspiration{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: users[0].BusinessID, TagIDs: pq.StringArray{items[0].BusinessID.String()}, ImageKey: "https://example.invalid/a.png", Data: model.JSONB(`{"schema_version":1}`)}
	imageB := model.Inspiration{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: users[0].BusinessID, TagIDs: pq.StringArray{items[0].BusinessID.String()}, ImageKey: "https://example.invalid/b.png", Data: model.JSONB(`{"schema_version":1}`)}
	if err := repository.CreateInspiration(ctxA, &imageA); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateInspiration(ctxB, &imageB); err != nil {
		t.Fatal(err)
	}
	statistics, err := repository.GetInspirationStatistics(ctxA, users[0].BusinessID)
	if err != nil || statistics.TotalImages != 1 {
		t.Fatalf("Raw统计串租户: total=%d err=%v", statistics.TotalImages, err)
	}
	consoleImages, total, err := repository.ListConsoleInspirations(ctxA, "", "created_desc", 0, 20)
	if err != nil || total != 1 || len(consoleImages) != 1 || consoleImages[0].BusinessID != imageA.BusinessID {
		t.Fatalf("图库别名JOIN串租户: total=%d rows=%#v err=%v", total, consoleImages, err)
	}
	if err := repository.DeleteTagAndCleanInspirations(ctxA, users[0].BusinessID, items[0].BusinessID); err != nil {
		t.Fatal(err)
	}
	var retained model.Inspiration
	if err := db.WithContext(ctxB).Where("business_id = ?", imageB.BusinessID).First(&retained).Error; err != nil || len(retained.TagIDs) != 1 {
		t.Fatalf("Raw标签清理修改其他组织: %#v %v", retained.TagIDs, err)
	}
	usage := model.ProjectInspirationUsage{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: users[1].BusinessID, ProjectBusinessID: uuid.New(), InspirationBusinessID: imageA.BusinessID}
	if err := db.WithContext(ctxA).Create(&usage).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteInspiration(ctxA, users[0].BusinessID, imageA.BusinessID); !fault.Is(err, 409, "conflict") {
		t.Fatalf("组织管理员可删掉其他成员项目引用图: %v", err)
	}
	if err := db.WithContext(ctxA).Delete(&usage).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteInspiration(ctxA, users[0].BusinessID, imageA.BusinessID); err != nil {
		t.Fatal(err)
	}
	key := "local_20261022_" + uuid.NewString() + ".png"
	if err := repository.RegisterFileOwnership(ctxA, key, users[0].BusinessID); err != nil {
		t.Fatal(err)
	}
	if ok, err := repository.CanAccessFile(ctxB, key, users[0].BusinessID); err != nil || ok {
		t.Fatalf("文件跨组织可读: %t %v", ok, err)
	}
	if ok, err := repository.CanAccessFile(ctxA, key, users[1].BusinessID); err != nil || ok {
		t.Fatalf("同组织文件跨用户可读: %t %v", ok, err)
	}
	if err := repository.RegisterFileOwnership(ctxB, key, users[0].BusinessID); !fault.Is(err, 404, "not_found") {
		t.Fatalf("旧文件可被重新认领: %v", err)
	}
	conflictKey := "local_20261022_" + uuid.NewString() + ".png"
	quarantined := model.FileOwnership{BaseModel: model.BaseModel{BusinessID: uuid.New()}, StoreKey: conflictKey}
	if err := db.WithContext(ctxA).Create(&quarantined).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.RegisterFileOwnership(ctxA, conflictKey, users[0].BusinessID); !fault.Is(err, 404, "not_found") {
		t.Fatalf("空归属隔离文件被认领: %v", err)
	}
	if err := repository.RemoveOrganizationMember(ctxA, orgA.OrgID, users[0].BusinessID); !fault.Is(err, 409, "conflict") {
		t.Fatalf("可移除最后管理员: %v", err)
	}
	if err := repository.UpsertOrganizationMember(ctxA, orgA.OrgID, users[1].BusinessID, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := repository.RemoveOrganizationMember(ctxA, orgA.OrgID, users[0].BusinessID); err != nil {
		t.Fatal(err)
	}
	members, err := repository.ListOrganizationMemberships(global, users[0].BusinessID)
	if err != nil || len(members) != 1 || members[0].OrgID != orgB.OrgID {
		t.Fatalf("移除后仍返回组织环境: %#v %v", members, err)
	}
	if _, _, err := repository.ListOrganizationMembers(ctxA, orgB.OrgID, 0, 20); !fault.Is(err, 404, "not_found") {
		t.Fatalf("管理成员可以串组织: %v", err)
	}
}

// TestTenantLegacyMigration 用独立随机 schema 运行已发布迁移，验证旧文件授权回填与冲突隔离。
func TestTenantLegacyMigration(t *testing.T) {
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if dsn == "" {
		t.Skip("未配置独立多租户迁移测试库")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	schema := "tenant_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := db.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error; _ = sqlDB.Close() })
	if err := db.Exec(`SET search_path TO "` + schema + `", public`).Error; err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasPrefix(filepath.Base(file), "000021_") {
			continue
		}
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(body)).Error; err != nil {
			t.Fatalf("旧迁移%s失败:%v", filepath.Base(file), err)
		}
	}
	first, second := uuid.New(), uuid.New()
	avatarKey := "local_20261022_" + uuid.NewString() + ".png"
	sharedKey := "local_20261022_" + uuid.NewString() + ".png"
	if err := db.Exec("INSERT INTO users (business_id,nickname,phone,password_hash,avatar_key) VALUES (?, '迁移甲','17500000001','test-only-hash',?), (?, '迁移乙','17500000002','test-only-hash',NULL)", first, avatarKey, second).Error; err != nil {
		t.Fatal(err)
	}
	for _, userID := range []uuid.UUID{first, second} {
		if err := db.Exec("INSERT INTO inspirations (business_id,user_id,image_key,data) VALUES (?,?,?,'{\"schema_version\":1}'::jsonb)", uuid.New(), userID, sharedKey).Error; err != nil {
			t.Fatal(err)
		}
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000021_multitenant_organizations.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(body)).Error; err != nil {
		t.Fatalf("租户增量迁移失败:%v", err)
	}
	var owner model.FileOwnership
	if err := db.Where("storekey = ?", avatarKey).First(&owner).Error; err != nil || owner.OrgID != -1 || owner.UserID == nil || *owner.UserID != first {
		t.Fatalf("历史头像归属错误:%#v %v", owner, err)
	}
	var conflict model.FileOwnership
	if err := db.Where("storekey = ?", sharedKey).First(&conflict).Error; err != nil || conflict.OrgID != -1 || conflict.UserID != nil {
		t.Fatalf("多用户历史key未隔离:%#v %v", conflict, err)
	}
	var count int64
	if err := db.Raw("SELECT count(*) FROM information_schema.columns WHERE table_schema = ? AND column_name = 'orgid'", schema).Scan(&count).Error; err != nil || count != 23 {
		t.Fatalf("未覆盖全部表orgid:count=%d err=%v", count, err)
	}
	if err := db.Raw("SELECT count(*) FROM information_schema.table_constraints WHERE table_schema = ? AND constraint_type = 'FOREIGN KEY'", schema).Scan(&count).Error; err != nil || count != 0 {
		t.Fatalf("迁移生成了外键:count=%d err=%v", count, err)
	}
	if err := db.Raw(`SELECT count(*) FROM information_schema.columns AS c WHERE c.table_schema = ? AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_description AS d JOIN pg_catalog.pg_class AS cl ON cl.oid = d.objoid JOIN pg_catalog.pg_namespace AS n ON n.oid = cl.relnamespace JOIN pg_catalog.pg_attribute AS a ON a.attrelid = cl.oid AND a.attnum = d.objsubid WHERE n.nspname = c.table_schema AND cl.relname = c.table_name AND a.attname = c.column_name AND d.description <> '')`, schema).Scan(&count).Error; err != nil || count != 0 {
		t.Fatalf("表字段缺少中文说明:count=%d err=%v", count, err)
	}
}
