package store

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// tenantDryRunDB 使用不会连接数据库的 PostgreSQL 编译器验证隔离谓词及拒绝条件。
func tenantDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=dryrun dbname=dryrun sslmode=disable"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := RegisterTenantCallbacks(db); err != nil {
		t.Fatal(err)
	}
	return db
}

// TestTenantCallbacksFailClosed 覆盖缺失身份、组织强制过滤、别名投影和越组织创建。
func TestTenantCallbacksFailClosed(t *testing.T) {
	db := tenantDryRunDB(t)
	var tags []model.Tag
	if result := db.WithContext(context.Background()).Find(&tags); !fault.Is(result.Error, 401, "unauthorized") {
		t.Fatalf("缺失组织上下文仍执行查询: %v", result.Error)
	}
	userID := uuid.New()
	ctx := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 42, UserID: userID})
	result := db.WithContext(ctx).Where("user_id = ?", userID).Find(&tags)
	if result.Error != nil || !strings.Contains(result.Statement.SQL.String(), `"tags"."orgid" =`) {
		t.Fatalf("查询缺少组织限制: sql=%s err=%v", result.Statement.SQL.String(), result.Error)
	}
	var images []model.ConsoleInspiration
	result = db.WithContext(ctx).Table("inspirations AS i").Select("i.business_id").Find(&images)
	if result.Error != nil || !strings.Contains(result.Statement.SQL.String(), `"i"."orgid" =`) {
		t.Fatalf("别名投影缺少组织限制: sql=%s err=%v", result.Statement.SQL.String(), result.Error)
	}
	item := model.Tag{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: userID, Content: "测试"}
	result = db.WithContext(ctx).Create(&item)
	if result.Error != nil || item.OrgID != 42 {
		t.Fatalf("新建资源未继承可信组织: org=%d err=%v", item.OrgID, result.Error)
	}
	item = model.Tag{BaseModel: model.BaseModel{BusinessID: uuid.New(), OrgID: 43}, UserID: userID, Content: "跨组织"}
	if result := db.WithContext(ctx).Create(&item); !fault.Is(result.Error, 404, "not_found") {
		t.Fatalf("跨组织创建未拒绝: %v", result.Error)
	}
	if result := db.WithContext(ctx).Model(&model.Tag{}).Where("business_id = ?", uuid.New()).Updates(map[string]any{"orgid": 43}); result.Error == nil {
		t.Fatal("资源可被迁移到其他组织")
	}
	for _, name := range []string{"public.inspirations AS i", `"public"."inspirations" AS i`} {
		var output []map[string]any
		if result := db.WithContext(context.Background()).Table(name).Find(&output); !fault.Is(result.Error, 401, "unauthorized") {
			t.Fatalf("schema限定表绕过身份校验:%s %v", name, result.Error)
		}
	}
	for _, updates := range []any{map[string]any{"OrgID": 43}, model.Tag{BaseModel: model.BaseModel{OrgID: 43}}} {
		if result := db.WithContext(ctx).Model(&model.Tag{}).Where("business_id = ?", uuid.New()).Updates(updates); result.Error == nil {
			t.Fatalf("Go字段形式可改组织:%#v", updates)
		}
	}
}

// TestSystemTablesRemainGlobal 验证全局身份查询始终落在 -1，不受当前组织或系统扫描标记影响。
func TestSystemTablesRemainGlobal(t *testing.T) {
	db := tenantDryRunDB(t)
	var users []model.User
	result := db.WithContext(tenant.System(context.Background())).Find(&users)
	if result.Error != nil || !strings.Contains(result.Statement.SQL.String(), `"users"."orgid" =`) || result.Statement.Vars[len(result.Statement.Vars)-1] != int64(-1) {
		t.Fatalf("全局账号未固定系统域: sql=%s vars=%v err=%v", result.Statement.SQL.String(), result.Statement.Vars, result.Error)
	}
	var plans []model.Plan
	result = db.WithContext(tenant.System(context.Background())).Find(&plans)
	if result.Error != nil || strings.Contains(result.Statement.SQL.String(), `"orgid" =`) {
		t.Fatalf("显式系统worker不能扫描队列: %s %v", result.Statement.SQL.String(), result.Error)
	}
}
