package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestInspirationProjectsMissingScope 即使没有数据库连接也必须先拒绝缺失或拼接的身份。
func TestInspirationProjectsMissingScope(t *testing.T) {
	store := &PostgresStore{}
	user := uuid.New()
	for _, ctx := range []context.Context{context.Background(), tenant.Personal(context.Background(), uuid.New()), tenant.System(context.Background())} {
		if _, _, err := store.ListInspirationProjects(ctx, user, uuid.New(), 0, 20); err == nil {
			t.Fatal("缺失或不一致范围未拒绝")
		}
	}
}

// TestInspirationProjectsPostgres 在独立数据库、独立 schema 中执行全量迁移和真实关联查询。
// 数据故意包含越权关联，验证联表不能依赖关系表内容推断访问权限。
func TestInspirationProjectsPostgres(t *testing.T) {
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if dsn == "" {
		t.Skip("需要独立 PostgreSQL 测试库 PHOTO_NINI_TENANT_TEST_DSN")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	defer connection.Close()
	connection.SetMaxOpenConns(1)
	schema := "inspiration_projects_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.Exec(`CREATE SCHEMA "` + schema + `"`).Error)
	defer db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
	must(db.Exec(`SET search_path TO "` + schema + `",public`).Error)
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	must(err)
	for _, file := range files {
		body, err := os.ReadFile(file)
		must(err)
		must(db.Exec(string(body)).Error)
	}
	repository := NewPostgresStore(db)
	user, other := uuid.New(), uuid.New()
	personal := tenant.Personal(context.Background(), user)
	orgA := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 701, UserID: user})
	orgB := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 702, UserID: user})
	otherUser := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 701, UserID: other})
	for _, sample := range []struct {
		ctx  context.Context
		user uuid.UUID
	}{{personal, user}, {orgA, user}, {orgB, user}, {otherUser, other}} {
		image := model.Inspiration{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: sample.user, ImageKey: "test-image", FileName: "test.png", Data: model.JSONB(`{}`)}
		must(db.WithContext(sample.ctx).Create(&image).Error)
		now := time.Now().UTC().Truncate(time.Second)
		var projects []model.Project
		for index := 0; index < 3; index++ {
			project := model.Project{BaseModel: model.BaseModel{BusinessID: uuid.New(), UpdatedAt: now}, UserID: sample.user, Title: "关联策划", CoverEmoji: "cover", Document: model.JSONB(`{"type":"doc","content":[{"type":"paragraph"}]}`)}
			if index == 2 {
				project.DeletedAt = &now
			}
			must(db.WithContext(sample.ctx).Create(&project).Error)
			must(db.WithContext(sample.ctx).Create(&model.ProjectInspirationUsage{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: sample.user, ProjectBusinessID: project.BusinessID, InspirationBusinessID: image.BusinessID}).Error)
			projects = append(projects, project)
		}
		first, total, err := repository.ListInspirationProjects(sample.ctx, sample.user, image.BusinessID, 0, 1)
		must(err)
		second, total2, err := repository.ListInspirationProjects(sample.ctx, sample.user, image.BusinessID, 1, 1)
		must(err)
		if total != 2 || total2 != 2 || len(first) != 1 || len(second) != 1 || first[0].BusinessID == second[0].BusinessID {
			t.Fatalf("分页/删除过滤错误: %d %d %#v %#v", total, total2, first, second)
		}
		if first[0].BusinessID.String() > second[0].BusinessID.String() {
			t.Fatal("相同时间未按UUID稳定排序")
		}
		for _, denied := range []struct {
			ctx  context.Context
			user uuid.UUID
		}{{context.Background(), sample.user}, {tenant.Personal(context.Background(), uuid.New()), sample.user}} {
			if _, _, err := repository.ListInspirationProjects(denied.ctx, denied.user, image.BusinessID, 0, 20); err == nil {
				t.Fatal("缺失/不一致会话可读取图片")
			}
		}
		for _, foreign := range []struct {
			ctx  context.Context
			user uuid.UUID
		}{{personal, user}, {orgA, user}, {orgB, user}, {otherUser, other}} {
			scope, _ := tenant.FromContext(foreign.ctx)
			if scope.OrgID == image.OrgID && foreign.user == sample.user {
				continue
			}
			if _, _, err := repository.ListInspirationProjects(foreign.ctx, foreign.user, image.BusinessID, 0, 20); err == nil {
				t.Fatal("越权图片可枚举策划")
			}
		}
		// 不正确的关联用户和组织，即使指向可见项目也不应影响结果。
		scope, _ := tenant.FromContext(sample.ctx)
		foreignCtx := tenant.WithScope(context.Background(), tenant.Scope{OrgID: scope.OrgID, UserID: uuid.New()})
		foreignScope, _ := tenant.FromContext(foreignCtx)
		must(db.WithContext(foreignCtx).Create(&model.ProjectInspirationUsage{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: foreignScope.UserID, ProjectBusinessID: projects[0].BusinessID, InspirationBusinessID: image.BusinessID}).Error)
		_, count, err := repository.ListInspirationProjects(sample.ctx, sample.user, image.BusinessID, 0, 20)
		must(err)
		if count != 2 {
			t.Fatal("越权关联影响列表数量")
		}
		empty := model.Inspiration{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: sample.user, ImageKey: "empty-image", FileName: "empty.png", Data: model.JSONB(`{}`)}
		must(db.WithContext(sample.ctx).Create(&empty).Error)
		rows, count, err := repository.ListInspirationProjects(sample.ctx, sample.user, empty.BusinessID, 0, 20)
		must(err)
		if count != 0 || rows == nil || len(rows) != 0 {
			t.Fatal("未关联图片没有返回空数组")
		}
	}
}
