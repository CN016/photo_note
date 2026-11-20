package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestPostgresSimpleProtocolSurvivesColumnAddition 复现扩展协议增列后的 SQLSTATE 0A000，
// 并确认应用实际使用的简单协议连接可以继续查询新增列。
func TestPostgresSimpleProtocolSurvivesColumnAddition(t *testing.T) {
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if dsn == "" {
		t.Skip("需要独立 PostgreSQL 测试库 PHOTO_NINI_TENANT_TEST_DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	raw, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close(context.Background()) })

	table := fmt.Sprintf("cache_repro_%d", time.Now().UnixNano())
	quotedTable := pgx.Identifier{table}.Sanitize()
	if _, err := raw.Exec(ctx, "CREATE TABLE "+quotedTable+" (a integer NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = raw.Exec(cleanupCtx, "DROP TABLE IF EXISTS "+quotedTable)
	})
	if _, err := raw.Exec(ctx, "INSERT INTO "+quotedTable+" (a) VALUES (1)"); err != nil {
		t.Fatal(err)
	}

	query := "SELECT * FROM " + quotedTable + " WHERE a = $1"
	var value int
	if err := raw.QueryRow(ctx, query, 1).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(ctx, "ALTER TABLE "+quotedTable+" ADD COLUMN b text"); err != nil {
		t.Fatal(err)
	}

	// 扩展协议复用旧结果描述时，PostgreSQL 会报告 cached plan must not change result type。
	err = raw.QueryRow(ctx, query, 1).Scan(&value)
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != "0A000" {
		t.Fatalf("未复现预期 SQLSTATE 0A000: %v", err)
	}

	db, err := gorm.Open(newPostgresDialector(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()

	var row struct {
		A int
		B *string
	}
	if err := db.WithContext(ctx).Raw("SELECT * FROM "+quotedTable+" WHERE a = ?", 1).Scan(&row).Error; err != nil {
		t.Fatalf("简单协议连接仍因陈旧结果描述失败: %v", err)
	}
	if row.A != 1 || row.B != nil {
		t.Fatalf("简单协议查询结果异常: %+v", row)
	}
}
