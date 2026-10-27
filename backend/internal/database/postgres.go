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
	"github.com/photo-nini/backend/internal/store"
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
	if err := store.RegisterTenantCallbacks(connection); err != nil {
		return nil, nil, err
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
		"inspiration_goals":           &model.InspirationGoal{},
		"inspiration_activities":      &model.InspirationActivity{},
		"inspiration_processing_jobs": &model.InspirationProcessingJob{},
		"app_configs":                 &model.AppConfig{},
		"users":                       &model.User{},
		"tags":                        &model.Tag{},
		"inspirations":                &model.Inspiration{},
		"feedbacks":                   &model.Feedback{},
		"feedback_notification_jobs":  &model.FeedbackNotificationJob{},
		"ledger_records":              &model.LedgerRecord{},
		"schedule_items":              &model.ScheduleItem{},
		"schedule_exceptions":         &model.ScheduleException{},
		"schedule_bookings":           &model.ScheduleBooking{},
		"schedule_shares":             &model.ScheduleShare{},
		"notifications":               &model.Notification{},
		"notice_config":               &model.NoticeConfig{},
		"notice_deliveries":           &model.NoticeDelivery{},
		"error_logs":                  &model.ErrorLog{},
		"projects":                    &model.Project{},
		"plans":                       &model.Plan{},
		"plan_reminders":              &model.PlanReminder{},
		"project_inspiration_usages":  &model.ProjectInspirationUsage{},
		"project_attachments":         &model.ProjectAttachment{},
		"organizations":               &model.Organization{},
		"organization_members":        &model.OrganizationMember{},
		"file_ownerships":             &model.FileOwnership{},
	} {
		if !connection.WithContext(ctx).Migrator().HasTable(table) {
			if name == "ledger_records" {
				return fmt.Errorf("PostgreSQL schema 缺少表 ledger_records，请先执行 backend/migrations/000009_create_ledger_records.sql")
			}
			if name == "notifications" || name == "notice_config" || name == "notice_deliveries" {
				return fmt.Errorf("PostgreSQL schema 缺少消息中心表 %s，请先执行 backend/migrations/000011_create_notification_center.sql", name)
			}
			if name == "error_logs" {
				return fmt.Errorf("PostgreSQL schema 缺少错误日志表 error_logs，请先执行 backend/migrations/000012_create_error_logs.sql")
			}
			if name == "projects" || name == "project_inspiration_usages" || name == "project_attachments" {
				return fmt.Errorf("PostgreSQL schema 缺少项目文档表 %s，请先执行 backend/migrations/000020_project_documents.sql", name)
			}
			if name == "feedbacks" {
				return fmt.Errorf("PostgreSQL schema 缺少反馈表 feedbacks，请先执行 backend/migrations/000008_user_phone_feedback.sql")
			}
			if name == "feedback_notification_jobs" {
				return fmt.Errorf("PostgreSQL schema 缺少反馈通知任务表 feedback_notification_jobs，请先执行 backend/migrations/000013_feedback_notifications.sql")
			}
			return fmt.Errorf("PostgreSQL schema 缺少表 %s，请先执行 backend/migrations 中的版本化 SQL", name)
		}
		// 所有自建表都必须完成租户迁移，不能让缺少隔离列的旧 schema 启动后才失败。
		if !connection.WithContext(ctx).Migrator().HasColumn(table, "OrgID") {
			return fmt.Errorf("PostgreSQL schema 缺少 %s.orgid，请先执行 backend/migrations/000021_multitenant_organizations.sql", name)
		}
		// 新 API 启动前检查公开标记列已由增量迁移创建，避免服务运行后才在公开读取请求中暴露迁移遗漏。
		if name == "app_configs" && !connection.WithContext(ctx).Migrator().HasColumn(table, "IsPublic") {
			return fmt.Errorf("PostgreSQL schema 缺少 app_configs.is_public，请先执行 backend/migrations/000004_add_config_public_flag.sql")
		}
		if name == "app_configs" && !connection.WithContext(ctx).Migrator().HasColumn(table, "IsSecret") {
			return fmt.Errorf("PostgreSQL schema 缺少 app_configs.is_secret，请先执行 backend/migrations/000015_console_roles_feedback_and_secret_configs.sql")
		}
		if name == "users" && !connection.WithContext(ctx).Migrator().HasColumn(table, "IsAdmin") {
			return fmt.Errorf("PostgreSQL schema 缺少 users.is_admin，请先执行 backend/migrations/000015_console_roles_feedback_and_secret_configs.sql")
		}
		if name == "users" && !connection.WithContext(ctx).Migrator().HasColumn(table, "Extra") {
			return fmt.Errorf("PostgreSQL schema 缺少 users.extra，请先执行 backend/migrations/000022_user_statistics_chart_modes.sql")
		}
		if name == "feedbacks" && !connection.WithContext(ctx).Migrator().HasColumn(table, "Status") {
			return fmt.Errorf("PostgreSQL schema 缺少 feedbacks.feedback_status，请先执行 backend/migrations/000015_console_roles_feedback_and_secret_configs.sql")
		}
		if name == "feedbacks" {
			migrator := connection.WithContext(ctx).Migrator()
			if !migrator.HasColumn(table, "ReplyContent") || !migrator.HasColumn(table, "ReplyImageKeys") {
				return fmt.Errorf("PostgreSQL schema 缺少反馈回复字段，请先执行 backend/migrations/000019_feedback_replies.sql")
			}
		}
		// 灵感图文件元数据缺失时不能安全创建或更新灵感图，因此启动阶段要求先执行增量迁移。
		if name == "inspiration_processing_jobs" && !connection.WithContext(ctx).Migrator().HasColumn(table, "AnalysisReady") {
			return fmt.Errorf("PostgreSQL schema 缺少任务识别许可字段，请先执行 backend/migrations/000024_inspiration_thumbnail_triggers.sql")
		}
		if name == "inspirations" {
			migrator := connection.WithContext(ctx).Migrator()
			// 新表存在仍需核对画像列，拒绝未完整应用增量迁移的 schema。
			for _, column := range []string{"people_count", "is_empty_shot", "dominant_color", "dominant_hue", "color_bucket", "color_palette", "analysis_status", "analysis_version", "analysis_algorithm", "analyzed_at"} {
				if !migrator.HasColumn(table, column) {
					return fmt.Errorf("PostgreSQL schema 缺少灵感库画像字段，请先执行 backend/migrations/000023_inspiration_library.sql")
				}
			}

			if !migrator.HasColumn(table, "FileName") || !migrator.HasColumn(table, "FileHash") || !migrator.HasColumn(table, "FileSize") {
				return fmt.Errorf("PostgreSQL schema 缺少灵感图文件元数据列，请按顺序执行 backend/migrations/000005_add_inspiration_file_metadata.sql 和 backend/migrations/000006_add_inspiration_file_size.sql")
			}
			if !migrator.HasColumn(table, "AsyncVersion") {
				return fmt.Errorf("PostgreSQL schema 缺少 inspirations.async_version，请先执行 backend/migrations/000007_add_inspiration_async_version.sql")
			}
			if !migrator.HasColumn(table, "image_metadata_extra") {
				return fmt.Errorf("PostgreSQL schema 缺少灵感图图片属性列，请先执行 backend/migrations/000016_add_inspiration_image_metadata.sql")
			}
		}
		if name == "notice_deliveries" {
			migrator := connection.WithContext(ctx).Migrator()
			if !migrator.HasColumn(table, "PartIndex") || !migrator.HasColumn(table, "PartCount") {
				return fmt.Errorf("PostgreSQL schema 缺少 webhook 消息分段列，请先执行 backend/migrations/000013_feedback_notifications.sql")
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
