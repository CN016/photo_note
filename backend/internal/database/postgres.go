package database

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/photo-nini/backend/internal/config"
	"github.com/photo-nini/backend/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenPostgres 建立 GORM 與底層連線池，確認 V1 schema 已由版本化 SQL 建立且不存在外鍵。
func OpenPostgres(ctx context.Context, cfg config.Config) (*gorm.DB, *sql.DB, error) {
	dsn := postgresDSN(cfg)
	connection, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// 嚴禁 GORM 自動建立外鍵；本服務不執行 AutoMigrate，資料結構只由版本化 SQL 管理。
		DisableForeignKeyConstraintWhenMigrating: true,
		TranslateError:                           true,
		// SQL 可能包含敏感欄位值，關閉 GORM SQL 日誌以避免密碼哈希或用戶資料泄漏。
		Logger: logger.Discard,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("初始化 PostgreSQL 驱动失败: %w", err)
	}

	sqlDB, err := connection.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("获取 PostgreSQL 连接池失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.DBMaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.DBMaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.DBConnMaxLifetime)

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("PostgreSQL 连通性检查失败: %w", err)
	}
	if err := checkSchema(ctx, connection); err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	return connection, sqlDB, nil
}

// postgresDSN 使用 URL 编码保存账号密码，避免密码中的保留字符破坏 PostgreSQL DSN。
func postgresDSN(cfg config.Config) string {
	connectionURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.DBUser, cfg.DBPassword),
		Host:   net.JoinHostPort(cfg.DBHost, strconv.Itoa(cfg.DBPort)),
		Path:   "/" + cfg.DBName,
	}
	query := connectionURL.Query()
	query.Set("sslmode", cfg.DBSSLMode)
	query.Set("timezone", "UTC")
	connectionURL.RawQuery = query.Encode()
	return connectionURL.String()
}

// checkSchema 只读取数据库目录，不自动建表；缺表或发现外键时让进程启动失败并提示修复 SQL。
func checkSchema(ctx context.Context, connection *gorm.DB) error {
	for name, table := range map[string]any{
		"app_configs":  &model.AppConfig{},
		"users":        &model.User{},
		"tags":         &model.Tag{},
		"inspirations": &model.Inspiration{},
	} {
		if !connection.WithContext(ctx).Migrator().HasTable(table) {
			return fmt.Errorf("PostgreSQL schema 缺少表 %s，请先执行 backend/migrations 中的版本化 SQL", name)
		}
		// 新 API 启动前检查公开标记列已由增量迁移创建，避免服务运行后才在公开读取请求中暴露迁移遗漏。
		if name == "app_configs" && !connection.WithContext(ctx).Migrator().HasColumn(table, "IsPublic") {
			return fmt.Errorf("PostgreSQL schema 缺少 app_configs.is_public，请先执行 backend/migrations/000004_add_config_public_flag.sql")
		}
		// 灵感图文件元数据缺失时不能安全创建或更新灵感图，因此启动阶段要求先执行增量迁移。
		if name == "inspirations" {
			migrator := connection.WithContext(ctx).Migrator()
			if !migrator.HasColumn(table, "FileName") || !migrator.HasColumn(table, "FileHash") {
				return fmt.Errorf("PostgreSQL schema 缺少 inspirations.file_name 或 inspirations.file_hash，请先执行 backend/migrations/000005_add_inspiration_file_metadata.sql")
			}
		}
	}
	var foreignKeyCount int64
	err := connection.WithContext(ctx).Raw(`
		SELECT count(*)
		FROM information_schema.table_constraints
		WHERE constraint_type = 'FOREIGN KEY'
		  AND table_schema = current_schema()
	`).Scan(&foreignKeyCount).Error
	if err != nil {
		return fmt.Errorf("检查 PostgreSQL 外键约束失败: %w", err)
	}
	if foreignKeyCount != 0 {
		return fmt.Errorf("PostgreSQL schema 中检测到 %d 个外键约束，项目规则禁止数据库外键", foreignKeyCount)
	}
	return nil
}

// Ping 使用调用方的超时上下文检查数据库，供健康检查和优雅停机前的状态观测使用。
func Ping(ctx context.Context, connection *gorm.DB) error {
	sqlDB, err := connection.DB()
	if err != nil {
		return err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return sqlDB.PingContext(pingCtx)
}
