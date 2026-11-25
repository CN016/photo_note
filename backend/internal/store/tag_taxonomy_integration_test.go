package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestTagTaxonomyPostgres 验证系统标签全局可见、用户自定义标签隔离以及旧 tag_names 更新复用系统业务 UUID。
func TestTagTaxonomyPostgres(t *testing.T) {
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if dsn == "" {
		t.Skip("未配置独立 PostgreSQL 测试库")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	repository := NewPostgresStore(db)
	user, other := uuid.New(), uuid.New()
	users := []model.User{
		{BaseModel: model.BaseModel{BusinessID: user}, Nickname: "标签分类甲", Phone: fmt.Sprintf("176%08d", time.Now().UnixNano()%100000000), PasswordHash: "integration-test-placeholder"},
		{BaseModel: model.BaseModel{BusinessID: other}, Nickname: "标签分类乙", Phone: fmt.Sprintf("177%08d", time.Now().UnixNano()%100000000), PasswordHash: "integration-test-placeholder"},
	}
	for index := range users {
		if err := repository.CreateUser(tenant.System(context.Background()), &users[index]); err != nil {
			t.Fatal(err)
		}
	}
	ctx := tenant.Personal(context.Background(), user)
	otherCtx := tenant.Personal(context.Background(), other)

	var systemTag model.Tag
	if err := db.WithContext(ctx).Where("tag_type = 'system' AND content = ?", "侧逆光").First(&systemTag).Error; err != nil {
		t.Fatal(err)
	}
	customTag := model.Tag{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, Content: "松下S5M2"}
	if err := repository.CreateTag(ctx, &customTag); err != nil {
		t.Fatal(err)
	}
	otherTag := model.Tag{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: other, Content: "泡泡道具"}
	if err := repository.CreateTag(otherCtx, &otherTag); err != nil {
		t.Fatal(err)
	}

	items, total, err := repository.ListTags(ctx, user, "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	systemCount := 0
	hasCustom, hasOther := false, false
	for _, item := range items {
		if item.TagType == "system" {
			systemCount++
		}
		hasCustom = hasCustom || item.BusinessID == customTag.BusinessID
		hasOther = hasOther || item.BusinessID == otherTag.BusinessID
	}
	if systemCount != 42 || !hasCustom || hasOther || total < 43 {
		t.Fatalf("标签可见范围错误: system=%d custom=%v other=%v total=%d", systemCount, hasCustom, hasOther, total)
	}
	if err := repository.ValidateTagOwnership(ctx, user, []uuid.UUID{systemTag.BusinessID, customTag.BusinessID}); err != nil {
		t.Fatalf("系统与自定义标签组合被拒绝: %v", err)
	}
	if _, err := repository.UpdateTag(ctx, user, systemTag.BusinessID, "改名"); !fault.Is(err, 404, "not_found") {
		t.Fatalf("普通用户不应修改系统标签: %v", err)
	}

	item := model.Inspiration{
		BaseModel: model.BaseModel{BusinessID: uuid.New()},
		UserID:    user,
		ImageKey:  "unsupported:tag-taxonomy",
		Data:      model.JSONB(`{"schema_version":1}`),
	}
	if err := repository.CreateInspiration(ctx, &item); err != nil {
		t.Fatal(err)
	}
	updated, err := repository.UpdateInspirationWithTagNames(ctx, user, item.BusinessID, []string{"侧逆光"}, map[string]any{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.TagIDs) != 1 || updated.TagIDs[0] != systemTag.BusinessID.String() || len(updated.TagNames) != 1 || updated.TagNames[0] != "侧逆光" {
		t.Fatalf("旧名称接口未复用系统标签: ids=%v names=%v", updated.TagIDs, updated.TagNames)
	}
}
