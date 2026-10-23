package database

import (
	"context"
	"os"
	"testing"

	"github.com/photo-nini/backend/internal/store"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestTenantSchemaStartupCheck 验证注册组织回调后仍能准确完成全部业务表的只读启动检查。
func TestTenantSchemaStartupCheck(t *testing.T) {
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if dsn == "" {
		t.Skip("未配置独立多租户迁移测试数据库")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard, DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := store.RegisterTenantCallbacks(db); err != nil {
		t.Fatal(err)
	}
	if err := checkSchema(context.Background(), db); err != nil {
		t.Fatalf("多租户 schema 无法通过启动检查:%v", err)
	}
}
