package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PostgresStore 把所有 GORM 查询集中在 repository 层，service 不直接拼接或执行数据库语句。
type PostgresStore struct {
	db                   *gorm.DB
	inspirationAlgorithm string
}

// NewPostgresStore 创建只封装查询能力的 repository，不会触发 AutoMigrate 或其他 DDL。
func NewPostgresStore(db *gorm.DB) *PostgresStore {
	if err := RegisterTenantCallbacks(db); err != nil {
		panic(err)
	}
	return &PostgresStore{db: db}
}

// ReadConfigValues 通过配置 repository 一次读取所需 key，业务 service 不直接查询配置表。
func (store *PostgresStore) ReadConfigValues(ctx context.Context, keys []string) (map[string]string, error) {
	var rows []model.AppConfig
	if err := store.db.WithContext(ctx).Select("config_key", "config_value").
		Where("config_key IN ?", keys).Find(&rows).Error; err != nil {
		return nil, persistenceError(err)
	}
	values := make(map[string]string, len(rows))
	for _, row := range rows {
		values[row.ConfigKey] = row.ConfigValue
	}
	return values, nil
}

// ReadPublicConfigValues 只读取显式开放的配置值；即使调用者指定私有 key，也不会返回对应记录。
func (store *PostgresStore) ReadPublicConfigValues(ctx context.Context, keys []string) (map[string]string, error) {
	var rows []model.AppConfig
	query := store.db.WithContext(ctx).
		Select("config_key", "config_value").
		Where("is_public = ? AND is_secret = ?", true, false)
	if len(keys) > 0 {
		query = query.Where("config_key IN ?", keys)
	}
	if err := query.Order("config_key ASC").Find(&rows).Error; err != nil {
		return nil, persistenceError(err)
	}
	values := make(map[string]string, len(rows))
	for _, row := range rows {
		values[row.ConfigKey] = row.ConfigValue
	}
	return values, nil
}

// FindUserByPhone 按唯一手机号读取认证所需字段；找不到时统一映射为资源不存在。
func (store *PostgresStore) FindUserByPhone(ctx context.Context, phone string) (model.User, error) {
	var user model.User
	err := store.db.WithContext(ctx).Where("phone = ?", phone).First(&user).Error
	if err != nil {
		return model.User{}, persistenceError(err)
	}
	return user, nil
}

// FindUserByBusinessID 通过对外业务 UUID 查询用户资料，不接受或返回数据库自增 ID。
func (store *PostgresStore) FindUserByBusinessID(ctx context.Context, businessID uuid.UUID) (model.User, error) {
	var user model.User
	err := store.db.WithContext(ctx).
		Select("business_id", "nickname", "phone", "avatar_key", "is_admin", "created_at", "updated_at").
		Where("business_id = ?", businessID).First(&user).Error
	if err != nil {
		return model.User{}, persistenceError(err)
	}
	return user, nil
}

// FindUserPasswordHash 仅供密码修改验证当前凭据，返回字段不会进入用户资料 DTO。
func (store *PostgresStore) FindUserPasswordHash(ctx context.Context, businessID uuid.UUID) (model.User, error) {
	var user model.User
	err := store.db.WithContext(ctx).Select("business_id", "nickname", "phone", "avatar_key", "is_admin", "created_at", "updated_at", "password_hash").
		Where("business_id = ?", businessID).First(&user).Error
	if err != nil {
		return model.User{}, persistenceError(err)
	}
	return user, nil
}

// UpdateUserPasswordHash 只保存 Argon2id 编码哈希并更新资料修改时间。
func (store *PostgresStore) UpdateUserPasswordHash(ctx context.Context, businessID uuid.UUID, passwordHash string) error {
	result := store.db.WithContext(ctx).Model(&model.User{}).
		Where("business_id = ?", businessID).
		Updates(map[string]any{"password_hash": passwordHash, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return persistenceError(result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

// CreateFeedbackAndNotificationJob 在同一事务中写入反馈和持久化通知任务，避免成功反馈丢失异步通知。
func (store *PostgresStore) CreateFeedbackAndNotificationJob(ctx context.Context, feedback *model.Feedback, job *model.FeedbackNotificationJob) error {
	if feedback == nil || job == nil || feedback.BusinessID == uuid.Nil || job.FeedbackBusinessID != feedback.BusinessID {
		return errors.New("反馈及通知任务参数无效")
	}
	return persistenceError(store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(feedback).Error; err != nil {
			return err
		}
		if err := tx.Create(job).Error; err != nil {
			return err
		}
		return nil
	}))
}

// CreateUser 保存服务器生成业务 UUID 和 Argon2id 密码哈希后的新用户。
func (store *PostgresStore) CreateUser(ctx context.Context, user *model.User) error {
	if err := store.db.WithContext(ctx).Create(user).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return fault.Conflict("该手机号已注册")
		}
		return persistenceError(err)
	}
	return nil
}

// UpdateUserProfile 仅更新 service 已验证的资料字段，并在同一语句中刷新修改时间。
func (store *PostgresStore) UpdateUserProfile(ctx context.Context, userID uuid.UUID, updates map[string]any) (model.User, error) {
	updates["updated_at"] = time.Now().UTC()
	result := store.db.WithContext(ctx).Model(&model.User{}).
		Where("business_id = ?", userID).Updates(updates)
	if result.Error != nil {
		return model.User{}, persistenceError(result.Error)
	}
	return store.FindUserByBusinessID(ctx, userID)
}

// UpdateStatisticsChartMode 用单条 SQL 更新 JSONB 的一个键，避免并发切换覆盖其他卡片偏好。
func (store *PostgresStore) UpdateStatisticsChartMode(ctx context.Context, userID uuid.UUID, dimension, mode string) (model.User, error) {
	result := store.db.WithContext(ctx).Model(&model.User{}).Where("business_id = ?", userID).Updates(map[string]any{
		"extra":      gorm.Expr("jsonb_set(jsonb_set(COALESCE(extra, '{}'::jsonb), '{statistics_chart_modes}', CASE WHEN jsonb_typeof(extra->'statistics_chart_modes') = 'object' THEN extra->'statistics_chart_modes' ELSE '{}'::jsonb END, true), ARRAY['statistics_chart_modes', ?]::text[], to_jsonb(?::text), true)", dimension, mode),
		"updated_at": time.Now().UTC(),
	})
	if result.Error != nil {
		return model.User{}, persistenceError(result.Error)
	}
	if result.RowsAffected == 0 {
		return model.User{}, fault.NotFound()
	}
	return store.FindUserByBusinessID(ctx, userID)
}

// ListTags 返回全局系统预设和当前用户私有自定义标签，并统计当前用户的实际引用频次。
func (store *PostgresStore) ListTags(ctx context.Context, userID uuid.UUID, search string, offset, limit int) ([]model.Tag, int64, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return nil, 0, err
	}
	systemOwner := uuid.Nil.String()
	visible := `((t.orgid = -1 AND t.user_id = ?::uuid AND t.tag_type = 'system') OR (t.orgid = ? AND t.user_id = ? AND t.tag_type = 'custom'))`
	countArgs := []any{systemOwner, scope.OrgID, userID}
	countSQL := `SELECT count(*) FROM tags t WHERE ` + visible
	if search != "" {
		pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(search) + "%"
		countSQL += " AND t.content ILIKE ? ESCAPE E'\\\\'"
		countArgs = append(countArgs, pattern)
	}
	var total int64
	if err := store.db.WithContext(ctx).Raw(countSQL, countArgs...).Scan(&total).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	type tagUsageRow struct {
		ID         int        `gorm:"column:id"`
		OrgID      int64      `gorm:"column:orgid"`
		BusinessID uuid.UUID  `gorm:"column:business_id"`
		CreatedAt  time.Time  `gorm:"column:created_at"`
		UpdatedAt  time.Time  `gorm:"column:updated_at"`
		UserID     uuid.UUID  `gorm:"column:user_id"`
		Content    string     `gorm:"column:content"`
		TagType    string     `gorm:"column:tag_type"`
		Category   string     `gorm:"column:category"`
		Source     string     `gorm:"column:source"`
		UsageCount int64      `gorm:"column:usage_count"`
		LastUsedAt *time.Time `gorm:"column:last_used_at"`
	}
	rows := make([]tagUsageRow, 0)
	searchSQL, args := "", []any{scope.OrgID, userID, systemOwner, scope.OrgID, userID}
	if search != "" {
		searchSQL = " AND t.content ILIKE ? ESCAPE E'\\\\'"
		args = append(args, search)
	}
	args = append(args, offset, limit)
	// 系统标签固定使用 orgid=-1 和全零 user_id；用户自定义标签仍按组织与创建者隔离。
	if err := store.db.WithContext(ctx).Raw(`
		SELECT t.id,t.orgid,t.business_id,t.created_at,t.updated_at,t.user_id,t.content,
		       t.tag_type,t.category,COALESCE(usage.usage_count,0) AS usage_count,usage.last_used_at,
		       CASE
		         WHEN usage.has_ai AND usage.has_manual THEN 'mixed'
		         WHEN usage.has_ai THEN 'ai'
		         WHEN usage.has_manual THEN 'manual'
		         ELSE 'unused'
		       END AS source
		FROM tags t
		LEFT JOIN LATERAL (
		    SELECT count(DISTINCT i.business_id) AS usage_count,
		           max(i.updated_at) AS last_used_at,
		           bool_or(assignment.auto_assigned) AS has_ai,
		           bool_or(NOT assignment.auto_assigned) AS has_manual
		    FROM inspirations i
		    CROSS JOIN LATERAL (
		        SELECT EXISTS(
		            SELECT 1
		            FROM jsonb_array_elements(COALESCE(i.auto_tags->'items','[]'::jsonb)) auto_tag
		            WHERE auto_tag->>'name' = t.content
		        ) AS auto_assigned
		    ) assignment
		    WHERE i.orgid = ? AND i.user_id = ? AND t.business_id = ANY(i.tag_ids)
		) usage ON TRUE
		WHERE ((t.orgid = -1 AND t.user_id = ?::uuid AND t.tag_type = 'system') OR (t.orgid = ? AND t.user_id = ? AND t.tag_type = 'custom'))`+searchSQL+`
		ORDER BY CASE WHEN t.tag_type = 'system' THEN 0 ELSE 1 END,
		         CASE t.category WHEN 'subject' THEN 0 WHEN 'composition' THEN 1 WHEN 'lighting' THEN 2 WHEN 'style' THEN 3 ELSE 4 END,
		         usage.usage_count DESC,usage.last_used_at DESC NULLS LAST,t.created_at ASC,t.business_id ASC
		OFFSET ? LIMIT ?`, args...).Scan(&rows).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	items := make([]model.Tag, 0, len(rows))
	for _, row := range rows {
		items = append(items, model.Tag{
			BaseModel:  model.BaseModel{ID: row.ID, OrgID: row.OrgID, BusinessID: row.BusinessID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt},
			UserID:     row.UserID,
			Content:    row.Content,
			TagType:    row.TagType,
			Category:   row.Category,
			Source:     row.Source,
			UsageCount: row.UsageCount,
			LastUsedAt: row.LastUsedAt,
		})
	}
	return items, total, nil
}

// CreateTag 保存归属于当前用户业务 UUID 的标签。
func (store *PostgresStore) CreateTag(ctx context.Context, tag *model.Tag) error {
	if tag.TagType == "" {
		tag.TagType = "custom"
	}
	if tag.Category == "" {
		tag.Category = "custom"
	}
	if err := store.db.WithContext(ctx).Create(tag).Error; err != nil {
		return persistenceError(err)
	}
	return nil
}

// FindTagByBusinessID 通过标签业务 UUID 和用户业务 UUID 双重限定资源访问范围。
func (store *PostgresStore) FindTagByBusinessID(ctx context.Context, userID, tagID uuid.UUID) (model.Tag, error) {
	var tag model.Tag
	err := store.db.WithContext(ctx).Where("user_id = ? AND business_id = ?", userID, tagID).First(&tag).Error
	if err != nil {
		return model.Tag{}, persistenceError(err)
	}
	return tag, nil
}

// UpdateTag 只更新当前用户拥有的标签；后续读取继续按双业务 UUID 限定归属。
func (store *PostgresStore) UpdateTag(ctx context.Context, userID, tagID uuid.UUID, content string) (model.Tag, error) {
	result := store.db.WithContext(ctx).Model(&model.Tag{}).
		Where("user_id = ? AND business_id = ?", userID, tagID).
		Updates(map[string]any{"content": content, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return model.Tag{}, persistenceError(result.Error)
	}
	return store.FindTagByBusinessID(ctx, userID, tagID)
}

// DeleteTagAndCleanInspirations 在一个 PostgreSQL 事务中清除数组引用并删除标签，不依赖外键级联。
func (store *PostgresStore) DeleteTagAndCleanInspirations(ctx context.Context, userID, tagID uuid.UUID) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tag model.Tag
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ?", userID, tagID).First(&tag).Error; err != nil {
			return persistenceError(err)
		}
		if err := tx.Exec(`
			UPDATE inspirations
			SET tag_ids = array_remove(tag_ids, ?::uuid), updated_at = CURRENT_TIMESTAMP
			WHERE orgid = ? AND user_id = ? AND ?::uuid = ANY(tag_ids)
		`, tagID.String(), scope.OrgID, userID, tagID.String()).Error; err != nil {
			return persistenceError(err)
		}
		// 标签也用于独立作品集；删除前按组织和创建者清除两张表中的引用。
		for _, table := range []string{"portfolios", "portfolio_photos"} {
			if err := tx.Exec("UPDATE "+table+" SET tag_ids = array_remove(tag_ids, ?::uuid), updated_at = CURRENT_TIMESTAMP WHERE orgid = ? AND user_id = ? AND ?::uuid = ANY(tag_ids)", tagID.String(), scope.OrgID, userID, tagID.String()).Error; err != nil {
				return persistenceError(err)
			}
		}
		if err := tx.Delete(&tag).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
}

// ValidateTagOwnership 确认标签是全局系统预设或属于当前用户，不创建数据库外键。
func (store *PostgresStore) ValidateTagOwnership(ctx context.Context, userID uuid.UUID, tagIDs []uuid.UUID) error {
	if len(tagIDs) == 0 {
		return nil
	}
	if _, err := tenant.Require(ctx); err != nil {
		return err
	}
	var customCount, systemCount int64
	if err := store.db.WithContext(ctx).Model(&model.Tag{}).
		Where("business_id IN ? AND user_id = ? AND tag_type = 'custom'", tagIDs, userID).
		Distinct("business_id").Count(&customCount).Error; err != nil {
		return persistenceError(err)
	}
	if err := store.db.WithContext(tenant.System(ctx)).Model(&model.Tag{}).
		Where("business_id IN ? AND user_id = ? AND tag_type = 'system'", tagIDs, uuid.Nil).
		Distinct("business_id").Count(&systemCount).Error; err != nil {
		return persistenceError(err)
	}
	if customCount+systemCount != int64(len(tagIDs)) {
		return fault.InvalidField("tag_ids", "存在无效、重复或无权使用的标签")
	}
	return nil
}

// CreateInspiration 保存灵感图及其版本化 JSONB 数据。
func (store *PostgresStore) CreateInspiration(ctx context.Context, inspiration *model.Inspiration) error {
	return persistenceError(store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(inspiration).Error; err != nil {
			return err
		}
		if err := recordInspirationActivity(tx, inspiration); err != nil {
			return err
		}
		if err := store.enqueueInspirationProcessing(tx, inspiration, true); err != nil {
			return err
		}
		items := []model.Inspiration{*inspiration}
		if err := populateInspirationTagNames(ctx, tx, inspiration.UserID, items); err != nil {
			return err
		}
		*inspiration = items[0]
		return nil
	}))
}

// ListInspirations 按用户、标签任一/全部匹配规则和稳定排序执行分页检索。
func (store *PostgresStore) ListInspirations(ctx context.Context, userID uuid.UUID, tagIDs []uuid.UUID, match, search, sort string, offset, limit int) ([]model.Inspiration, int64, error) {
	query := store.db.WithContext(ctx).Model(&model.Inspiration{}).Where("user_id = ?", userID)
	query = applyInspirationFilters(query, ctx, userID)
	filters := model.InspirationFiltersFromContext(ctx)
	if search != "" {
		pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(search) + "%"
		// 文本搜索覆盖文件名、整图备注和锚点文字；非数组锚点按空集合处理，避免脏历史文档阻断查询。
		query = query.Where(`(
			file_name ILIKE ? ESCAPE E'\\'
			OR COALESCE(data->>'image_note', '') ILIKE ? ESCAPE E'\\'
			OR EXISTS (
				SELECT 1
				FROM jsonb_array_elements(
					CASE WHEN jsonb_typeof(data->'anchors') = 'array' THEN data->'anchors' ELSE '[]'::jsonb END
				) AS anchor(item)
				WHERE COALESCE(item->>'note', '') ILIKE ? ESCAPE E'\\'
			)
		)`, pattern, pattern, pattern)
	}
	if len(tagIDs) > 0 {
		values := make(pq.StringArray, 0, len(tagIDs))
		for _, tagID := range tagIDs {
			values = append(values, tagID.String())
		}
		if match == "all" {
			query = query.Where("tag_ids @> ?::uuid[]", values)
		} else {
			query = query.Where("tag_ids && ?::uuid[]", values)
		}
		if filters.TagSource != "" {
			query = applyInspirationTagSource(query, ctx, userID, values, match, filters.TagSource)
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	items := make([]model.Inspiration, 0)
	if sort == "color_match" {
		if expression, ok := inspirationColorDistance(filters.Color); ok {
			query = query.Order(expression).Order("created_at DESC, business_id DESC")
		} else {
			query = query.Order("created_at DESC, business_id DESC")
		}
	}
	order := map[string]string{"created_desc": "created_at DESC, business_id DESC", "created_asc": "created_at ASC, business_id ASC", "updated_desc": "updated_at DESC, business_id DESC"}[sort]
	if order == "" {
		if sort != "color_match" {
			order = "created_at DESC, business_id DESC"
		}
	}
	if order != "" {
		query = query.Order(order)
	}
	if err := query.Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	if err := populateInspirationTagNames(ctx, store.db, userID, items); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// applyInspirationTagSource 只比较当前图片已绑定的所选标签；自动标签按 auto_tags 名称匹配。
func applyInspirationTagSource(query *gorm.DB, ctx context.Context, userID uuid.UUID, tagIDs pq.StringArray, match, source string) *gorm.DB {
	scope, err := tenant.Require(ctx)
	if err != nil {
		query.AddError(err)
		return query
	}
	autoMatch := `EXISTS (
		SELECT 1
		FROM jsonb_array_elements(COALESCE(inspirations.auto_tags->'items','[]'::jsonb)) auto_tag
		WHERE auto_tag->>'name' = tag.content
	)`
	condition := autoMatch
	if source == "manual" {
		condition = "NOT " + autoMatch
	}
	if match == "all" {
		return query.Where(`NOT EXISTS (
			SELECT 1
			FROM unnest(?::uuid[]) selected_tag(id)
			LEFT JOIN tags tag ON tag.business_id = selected_tag.id AND tag.orgid = ? AND tag.user_id = ?
			WHERE tag.business_id IS NULL
			   OR NOT (tag.business_id = ANY(inspirations.tag_ids) AND `+condition+`)
		)`, tagIDs, scope.OrgID, userID)
	}
	return query.Where(`EXISTS (
		SELECT 1
		FROM tags tag
		WHERE tag.orgid = ? AND tag.user_id = ? AND tag.business_id = ANY(?::uuid[])
		  AND tag.business_id = ANY(inspirations.tag_ids)
		  AND `+condition+`
	)`, scope.OrgID, userID, tagIDs)
}

// inspirationColorDistance 生成前三种代表色的加权 RGB 距离排序表达式；色值在服务层已验证。
func inspirationColorDistance(value string) (string, bool) {
	if len(value) != 7 || value[0] != '#' {
		return "", false
	}
	red, ok := parseHexByte(value[1:3])
	if !ok {
		return "", false
	}
	green, ok := parseHexByte(value[3:5])
	if !ok {
		return "", false
	}
	blue, ok := parseHexByte(value[5:7])
	if !ok {
		return "", false
	}
	return fmt.Sprintf(`CASE
		WHEN jsonb_typeof(color_palette->'palette') = 'array' THEN COALESCE((
			SELECT MIN(
				2 * POWER((('x'||substr(color_item->>'hex',2,2))::bit(8)::integer - %d),2) +
				4 * POWER((('x'||substr(color_item->>'hex',4,2))::bit(8)::integer - %d),2) +
				3 * POWER((('x'||substr(color_item->>'hex',6,2))::bit(8)::integer - %d),2)
			)
			FROM jsonb_array_elements(color_palette->'palette') WITH ORDINALITY palette(color_item,position)
			WHERE position <= 3 AND color_item->>'hex' ~ '^#[0-9a-fA-F]{6}$'
		),1000000000)
		ELSE 1000000000
	END`, red, green, blue), true
}

// parseHexByte 只解析两位十六进制字节，调用方已完成长度和字符检查。
func parseHexByte(value string) (int, bool) {
	if len(value) != 2 {
		return 0, false
	}
	number := 0
	for _, char := range value {
		number *= 16
		switch {
		case char >= '0' && char <= '9':
			number += int(char - '0')
		case char >= 'a' && char <= 'f':
			number += int(char-'a') + 10
		case char >= 'A' && char <= 'F':
			number += int(char-'A') + 10
		default:
			return 0, false
		}
	}
	return number, true
}

// GetInspirationStatistics 在 PostgreSQL 内按用户聚合拍摄属性，只返回分组名称和数量。
func (store *PostgresStore) GetInspirationStatistics(ctx context.Context, userID uuid.UUID) (model.InspirationStatistics, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return model.InspirationStatistics{}, err
	}
	var totals struct {
		TotalImages                int64 `gorm:"column:total_images"`
		ImagesWithShootingMetadata int64 `gorm:"column:images_with_shooting_metadata"`
	}
	if err := store.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)::bigint AS total_images,
		       COUNT(*) FILTER (WHERE
		           NULLIF(BTRIM(camera_make), '') IS NOT NULL OR
		           NULLIF(BTRIM(camera_model), '') IS NOT NULL OR
		           NULLIF(BTRIM(lens_make), '') IS NOT NULL OR
		           NULLIF(BTRIM(lens_model), '') IS NOT NULL OR
		           aperture_f_number IS NOT NULL OR maximum_aperture_f_number IS NOT NULL OR
		           focal_length_mm IS NOT NULL OR focal_length_35mm_mm IS NOT NULL OR
		           exposure_time_seconds IS NOT NULL OR iso_speed IS NOT NULL
		       )::bigint AS images_with_shooting_metadata
		FROM inspirations
		WHERE orgid = ? AND user_id = ?
	`, scope.OrgID, userID).Scan(&totals).Error; err != nil {
		return model.InspirationStatistics{}, persistenceError(err)
	}

	var rows []struct {
		Dimension string `gorm:"column:dimension"`
		Value     string `gorm:"column:value"`
		Count     int64  `gorm:"column:image_count"`
	}
	if err := store.db.WithContext(ctx).Raw(`
		WITH scoped AS (
		    SELECT camera_make, camera_model, lens_make, lens_model,
		           focal_length_mm, focal_length_35mm_mm, aperture_f_number,
		           iso_speed, exposure_time_seconds, flash_fired,
		           image_width, image_height, color_space, created_at
		    FROM inspirations
		    WHERE orgid = ? AND user_id = ?
		),
		camera_labels AS (
		    SELECT NULLIF(BTRIM(CONCAT_WS(' ', NULLIF(BTRIM(camera_make), ''), NULLIF(BTRIM(camera_model), ''))), '') AS label
		    FROM scoped
		),
		lens_labels AS (
		    SELECT NULLIF(BTRIM(CONCAT_WS(' ', NULLIF(BTRIM(lens_make), ''), NULLIF(BTRIM(lens_model), ''))), '') AS label
		    FROM scoped
		),
		color_labels AS (
			SELECT NULLIF(BTRIM(color_space), '') AS label FROM scoped
		),
		metrics AS (
		    (
		        SELECT 'camera_models'::text AS dimension, MIN(label) AS value, COUNT(*)::bigint AS image_count
		        FROM camera_labels WHERE label IS NOT NULL
		        GROUP BY LOWER(label) ORDER BY image_count DESC, value LIMIT 5
		    )
		    UNION ALL
		    (
		        SELECT 'lens_models'::text, MIN(label), COUNT(*)::bigint
		        FROM lens_labels WHERE label IS NOT NULL
		        GROUP BY LOWER(label) ORDER BY COUNT(*) DESC, MIN(label) LIMIT 5
		    )
		    UNION ALL
		    (
		        SELECT 'focal_lengths'::text,
		               CASE
		                   WHEN focal >= 200 THEN 'super_telephoto'
		                   WHEN focal >= 85 THEN 'telephoto'
		                   WHEN focal >= 35 THEN 'standard'
		                   WHEN focal >= 24 THEN 'wide'
		                   ELSE 'ultra_wide'
		               END,
		               COUNT(*)::bigint
		        FROM (
		            SELECT COALESCE(NULLIF(focal_length_35mm_mm, 0), focal_length_mm) AS focal FROM scoped
		        ) AS focal_values
		        WHERE focal > 0 GROUP BY 2
		    )
		    UNION ALL
		    (
		        SELECT 'apertures'::text,
		               CASE
		                   WHEN aperture_f_number <= 2.8 THEN 'wide_open'
		                   WHEN aperture_f_number <= 5.6 THEN 'large'
		                   WHEN aperture_f_number <= 8 THEN 'medium'
		                   WHEN aperture_f_number < 16 THEN 'small'
		                   ELSE 'very_small'
		               END,
		               COUNT(*)::bigint
		        FROM scoped WHERE aperture_f_number > 0 GROUP BY 2
		    )
		    UNION ALL
		    (
		        SELECT 'iso_speeds'::text,
		               CASE
		                   WHEN iso_speed < 100 THEN 'under_100'
		                   WHEN iso_speed < 400 THEN '100_400'
		                   WHEN iso_speed < 1600 THEN '400_1600'
		                   WHEN iso_speed < 6400 THEN '1600_6400'
		                   ELSE '6400_plus'
		               END,
		               COUNT(*)::bigint
		        FROM scoped WHERE iso_speed > 0 GROUP BY 2
		    )
		    UNION ALL
		    (
		        SELECT 'exposure_times'::text,
		               CASE
		                   WHEN exposure_time_seconds <= 1.0 / 500.0 THEN '1_500_or_faster'
		                   WHEN exposure_time_seconds <= 1.0 / 60.0 THEN '1_500_to_1_60'
		                   WHEN exposure_time_seconds <= 1 THEN '1_60_to_1s'
		                   ELSE 'over_1s'
		               END,
		               COUNT(*)::bigint
		        FROM scoped WHERE exposure_time_seconds > 0 GROUP BY 2
		    )
		    UNION ALL
		    (
		        SELECT 'flash_usage'::text,
		               CASE WHEN flash_fired THEN 'fired' ELSE 'not_fired' END,
		               COUNT(*)::bigint
		        FROM scoped WHERE flash_fired IS NOT NULL GROUP BY 2
		    )
		    UNION ALL
		    (
		        SELECT 'aspect_ratios'::text,
		               CASE
		                   WHEN image_width::numeric / NULLIF(image_height, 0) >= 1.05 THEN 'landscape'
		                   WHEN image_width::numeric / NULLIF(image_height, 0) <= 0.95 THEN 'portrait'
		                   ELSE 'square'
		               END,
		               COUNT(*)::bigint
		        FROM scoped WHERE image_width > 0 AND image_height > 0 GROUP BY 2
		    )
		    UNION ALL
		    (
		        SELECT 'megapixels'::text,
		               CASE
		                   WHEN image_width::numeric * image_height / 1000000 <= 2 THEN 'under_2'
		                   WHEN image_width::numeric * image_height / 1000000 <= 12 THEN '2_12'
		                   WHEN image_width::numeric * image_height / 1000000 <= 24 THEN '12_24'
		                   ELSE '24_plus'
		               END,
		               COUNT(*)::bigint
		        FROM scoped WHERE image_width > 0 AND image_height > 0 GROUP BY 2
		    )
		    UNION ALL
		    (
		        SELECT 'color_spaces'::text, MIN(label), COUNT(*)::bigint
		        FROM color_labels WHERE label IS NOT NULL
		        GROUP BY LOWER(label) ORDER BY COUNT(*) DESC, MIN(label) LIMIT 5
		    )
		    UNION ALL
		    (
		        SELECT 'collection_months'::text, TO_CHAR(months.month, 'YYYY-MM'), COUNT(scoped.created_at)::bigint
		        FROM GENERATE_SERIES(
		            DATE_TRUNC('month', CURRENT_TIMESTAMP AT TIME ZONE 'UTC') - INTERVAL '11 months',
		            DATE_TRUNC('month', CURRENT_TIMESTAMP AT TIME ZONE 'UTC'),
		            INTERVAL '1 month'
		        ) AS months(month)
		        LEFT JOIN scoped ON DATE_TRUNC('month', scoped.created_at AT TIME ZONE 'UTC') = months.month
		        GROUP BY months.month ORDER BY months.month
		    )
		)
		SELECT dimension, value, image_count
		FROM metrics
		ORDER BY dimension,
		         CASE WHEN dimension = 'collection_months' THEN value END ASC,
		         CASE WHEN dimension <> 'collection_months' THEN image_count END DESC,
		         value
	`, scope.OrgID, userID).Scan(&rows).Error; err != nil {
		return model.InspirationStatistics{}, persistenceError(err)
	}

	statistics := model.InspirationStatistics{
		TotalImages: totals.TotalImages, ImagesWithShootingMetadata: totals.ImagesWithShootingMetadata,
		CollectionMonths: make([]model.InspirationStatisticBucket, 0, 12),
		CameraModels:     make([]model.InspirationStatisticBucket, 0, 5), LensModels: make([]model.InspirationStatisticBucket, 0, 5),
		FocalLengths: make([]model.InspirationStatisticBucket, 0, 5), Apertures: make([]model.InspirationStatisticBucket, 0, 5),
		ISOSpeeds: make([]model.InspirationStatisticBucket, 0, 5), ExposureTimes: make([]model.InspirationStatisticBucket, 0, 4),
		FlashUsage: make([]model.InspirationStatisticBucket, 0, 2), AspectRatios: make([]model.InspirationStatisticBucket, 0, 3),
		Megapixels: make([]model.InspirationStatisticBucket, 0, 4), ColorSpaces: make([]model.InspirationStatisticBucket, 0, 5),
	}
	for _, row := range rows {
		bucket := model.InspirationStatisticBucket{Value: row.Value, Count: row.Count}
		switch row.Dimension {
		case "collection_months":
			statistics.CollectionMonths = append(statistics.CollectionMonths, bucket)
		case "camera_models":
			statistics.CameraModels = append(statistics.CameraModels, bucket)
		case "lens_models":
			statistics.LensModels = append(statistics.LensModels, bucket)
		case "focal_lengths":
			statistics.FocalLengths = append(statistics.FocalLengths, bucket)
		case "apertures":
			statistics.Apertures = append(statistics.Apertures, bucket)
		case "iso_speeds":
			statistics.ISOSpeeds = append(statistics.ISOSpeeds, bucket)
		case "exposure_times":
			statistics.ExposureTimes = append(statistics.ExposureTimes, bucket)
		case "flash_usage":
			statistics.FlashUsage = append(statistics.FlashUsage, bucket)
		case "aspect_ratios":
			statistics.AspectRatios = append(statistics.AspectRatios, bucket)
		case "megapixels":
			statistics.Megapixels = append(statistics.Megapixels, bucket)
		case "color_spaces":
			statistics.ColorSpaces = append(statistics.ColorSpaces, bucket)
		}
	}
	return statistics, nil
}

// FindInspirationByBusinessID 用业务 UUID 查询详情，并同时校验用户归属。
func (store *PostgresStore) FindInspirationByBusinessID(ctx context.Context, userID, inspirationID uuid.UUID) (model.Inspiration, error) {
	var inspiration model.Inspiration
	err := store.db.WithContext(ctx).
		Where("user_id = ? AND business_id = ?", userID, inspirationID).
		First(&inspiration).Error
	if err != nil {
		return model.Inspiration{}, persistenceError(err)
	}
	items := []model.Inspiration{inspiration}
	if err := populateInspirationTagNames(ctx, store.db, userID, items); err != nil {
		return model.Inspiration{}, err
	}
	return items[0], nil
}

// populateInspirationTagNames 批量读取当前用户标签名称，并按灵感图 tag_ids 的顺序填充展示字段。
func populateInspirationTagNames(ctx context.Context, db *gorm.DB, userID uuid.UUID, items []model.Inspiration) error {
	if _, err := tenant.Require(ctx); err != nil {
		return err
	}
	ids := make([]uuid.UUID, 0)
	seen := make(map[uuid.UUID]struct{})
	for index := range items {
		items[index].TagNames = make([]string, 0, len(items[index].TagIDs))
		for _, value := range items[index].TagIDs {
			tagID, err := uuid.Parse(value)
			if err != nil {
				return fault.Internal(err)
			}
			if _, exists := seen[tagID]; exists {
				continue
			}
			seen[tagID] = struct{}{}
			ids = append(ids, tagID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var customTags, systemTags []model.Tag
	if err := db.WithContext(ctx).Select("business_id", "content", "tag_type", "category").
		Where("business_id IN ? AND user_id = ? AND tag_type = 'custom'", ids, userID).Find(&customTags).Error; err != nil {
		return persistenceError(err)
	}
	if err := db.WithContext(tenant.System(ctx)).Select("business_id", "content", "tag_type", "category").
		Where("business_id IN ? AND user_id = ? AND tag_type = 'system'", ids, uuid.Nil).Find(&systemTags).Error; err != nil {
		return persistenceError(err)
	}
	tags := append(customTags, systemTags...)
	namesByID := make(map[uuid.UUID]string, len(tags))
	for _, tag := range tags {
		namesByID[tag.BusinessID] = tag.Content
	}
	for index := range items {
		for _, value := range items[index].TagIDs {
			tagID, _ := uuid.Parse(value) // UUID 数组由 PostgreSQL 类型约束保证格式有效。
			if name, exists := namesByID[tagID]; exists {
				items[index].TagNames = append(items[index].TagNames, name)
			}
		}
	}
	return nil
}

// UpdateInspiration 更新 service 已验证字段，并限定当前用户避免通过 UUID 越权写入。
func (store *PostgresStore) UpdateInspiration(ctx context.Context, userID, inspirationID uuid.UUID, updates map[string]any) (model.Inspiration, error) {
	if rawData, ok := updates["data"].(model.JSONB); ok {
		encoded := string(rawData)
		// 普通数据编辑不得自行清除待处理原图，也不得把压缩完成后已清理的旧 key 重新写回。
		updates["data"] = gorm.Expr(
			"CASE WHEN data->'source_image_key' IS NOT NULL THEN ((?::jsonb - 'source_image_key') || jsonb_build_object('source_image_key', data->'source_image_key')) ELSE (?::jsonb - 'source_image_key') END",
			encoded, encoded,
		)
	}
	updates["updated_at"] = time.Now().UTC()
	result := store.db.WithContext(ctx).Model(&model.Inspiration{}).
		Where("user_id = ? AND business_id = ?", userID, inspirationID).Updates(updates)
	if result.Error != nil {
		return model.Inspiration{}, persistenceError(result.Error)
	}
	return store.FindInspirationByBusinessID(ctx, userID, inspirationID)
}

// UpdateInspirationWithAsyncVersion 原子更新灵感图并递增异步代次，RETURNING 返回本次请求实际取得的版本。
func (store *PostgresStore) UpdateInspirationWithAsyncVersion(ctx context.Context, userID, inspirationID uuid.UUID, updates map[string]any) (model.Inspiration, error) {
	var updated model.Inspiration
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates["async_version"] = gorm.Expr("async_version + 1")
		updates["updated_at"] = time.Now().UTC()
		var inspiration model.Inspiration
		result := tx.Model(&inspiration).Clauses(clause.Returning{}).
			Where("user_id = ? AND business_id = ?", userID, inspirationID).Updates(updates)
		if result.Error != nil {
			return persistenceError(result.Error)
		}
		if result.RowsAffected == 0 {
			return fault.NotFound()
		}
		items := []model.Inspiration{inspiration}
		if err := populateInspirationTagNames(ctx, tx, userID, items); err != nil {
			return err
		}
		updated = items[0]
		return store.enqueueInspirationProcessing(tx, &updated)
	})
	return updated, err
}

// UpdateInspirationWithTagNames 在同一事务中复用或新建用户标签并更新灵感图，确保名称解析与绑定不会部分提交。
func (store *PostgresStore) UpdateInspirationWithTagNames(ctx context.Context, userID, inspirationID uuid.UUID, names []string, updates map[string]any, incrementAsyncVersion bool) (model.Inspiration, error) {
	var inspiration model.Inspiration
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		writeFields := make(map[string]any, len(updates)+3)
		for key, value := range updates {
			writeFields[key] = value
		}
		systemByName := make(map[string]uuid.UUID)
		if len(names) > 0 {
			var systemTags []model.Tag
			if err := tx.WithContext(tenant.System(ctx)).Where("user_id = ? AND tag_type = 'system' AND content IN ?", uuid.Nil, names).Find(&systemTags).Error; err != nil {
				return persistenceError(err)
			}
			for _, tag := range systemTags {
				systemByName[tag.Content] = tag.BusinessID
			}
		}
		newTags := make([]model.Tag, 0, len(names))
		for _, name := range names {
			if _, system := systemByName[name]; system {
				continue
			}
			newTags = append(newTags, model.Tag{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: userID, Content: name, TagType: "custom", Category: "custom"})
		}
		if len(newTags) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "orgid"}, {Name: "user_id"}, {Name: "content"}},
				DoNothing: true,
			}).Create(&newTags).Error; err != nil {
				return persistenceError(err)
			}
		}

		nameIDs := make(map[string]uuid.UUID, len(names))
		if len(names) > 0 {
			var customTags, systemTags []model.Tag
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("user_id = ? AND tag_type = 'custom' AND content IN ?", userID, names).Find(&customTags).Error; err != nil {
				return persistenceError(err)
			}
			if err := tx.WithContext(tenant.System(ctx)).Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("user_id = ? AND tag_type = 'system' AND content IN ?", uuid.Nil, names).Find(&systemTags).Error; err != nil {
				return persistenceError(err)
			}
			for _, tag := range customTags {
				nameIDs[tag.Content] = tag.BusinessID
			}
			for _, tag := range systemTags {
				nameIDs[tag.Content] = tag.BusinessID
			}
		}
		tagIDs := make(pq.StringArray, 0, len(names))
		for _, name := range names {
			tagID, exists := nameIDs[name]
			if !exists {
				return fault.Internal(errors.New("标签创建后未能按用户和名称读取"))
			}
			tagIDs = append(tagIDs, tagID.String())
		}
		writeFields["tag_ids"] = tagIDs
		writeFields["updated_at"] = time.Now().UTC()
		if incrementAsyncVersion {
			writeFields["async_version"] = gorm.Expr("async_version + 1")
		}
		if rawData, ok := writeFields["data"].(model.JSONB); ok {
			encoded := string(rawData)
			// 普通数据编辑不能清除后台待处理原图，也不能恢复已清理的原图 key。
			writeFields["data"] = gorm.Expr(
				"CASE WHEN data->'source_image_key' IS NOT NULL THEN ((?::jsonb - 'source_image_key') || jsonb_build_object('source_image_key', data->'source_image_key')) ELSE (?::jsonb - 'source_image_key') END",
				encoded, encoded,
			)
		}
		result := tx.Model(&model.Inspiration{}).
			Where("user_id = ? AND business_id = ?", userID, inspirationID).Updates(writeFields)
		if result.Error != nil {
			return persistenceError(result.Error)
		}
		if result.RowsAffected == 0 {
			return fault.NotFound()
		}
		if err := tx.Where("user_id = ? AND business_id = ?", userID, inspirationID).First(&inspiration).Error; err != nil {
			return persistenceError(err)
		}
		items := []model.Inspiration{inspiration}
		if err := populateInspirationTagNames(ctx, tx, userID, items); err != nil {
			return err
		}
		inspiration = items[0]
		if incrementAsyncVersion {
			return store.enqueueInspirationProcessing(tx, &inspiration)
		}
		return nil
	})
	if err != nil {
		return model.Inspiration{}, err
	}
	return inspiration, nil
}

// UpdateInspirationFileMetadataIfVersion 仅在图片 key 和异步代次仍匹配时写入后台处理结果。
func (store *PostgresStore) UpdateInspirationFileMetadataIfVersion(ctx context.Context, userID, inspirationID uuid.UUID, imageKey string, asyncVersion int64, fileHash *string, fileSize *int64) (bool, error) {
	result := store.db.WithContext(ctx).Model(&model.Inspiration{}).
		Where("user_id = ? AND business_id = ? AND image_key = ? AND async_version = ?", userID, inspirationID, imageKey, asyncVersion).
		Updates(map[string]any{"file_hash": fileHash, "file_size": fileSize, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return false, persistenceError(result.Error)
	}
	return result.RowsAffected > 0, nil
}

// RemoveInspirationSourceImageKeyIfVersion 仅在当前图片代次及原图 key 仍匹配时移除 JSON 中的原图引用。
func (store *PostgresStore) RemoveInspirationSourceImageKeyIfVersion(ctx context.Context, userID, inspirationID uuid.UUID, imageKey string, asyncVersion int64, sourceImageKey string) (bool, error) {
	result := store.db.WithContext(ctx).Model(&model.Inspiration{}).
		Where("user_id = ? AND business_id = ? AND image_key = ? AND async_version = ?", userID, inspirationID, imageKey, asyncVersion).
		Where("data ->> 'source_image_key' = ?", sourceImageKey).
		Updates(map[string]any{
			"data":       gorm.Expr("data - ?", "source_image_key"),
			"updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return false, persistenceError(result.Error)
	}
	return result.RowsAffected > 0, nil
}

// IsInspirationFileReferenced 检查文件 key 是否仍被任意灵感图作为工作图或原图引用。
func (store *PostgresStore) IsInspirationFileReferenced(ctx context.Context, imageKey string, exceptInspirationID uuid.UUID) (bool, error) {
	var inspiration model.Inspiration
	err := store.db.WithContext(ctx).Select("business_id").
		Where("business_id <> ? AND (image_key = ? OR data ->> 'source_image_key' = ?)", exceptInspirationID, imageKey, imageKey).
		Limit(1).Take(&inspiration).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 同一私有文件可能同时用作头像、计划封面或项目附件，清理原图前必须核对全部持久引用。
		checks := []struct {
			entity    any
			condition string
		}{
			{&model.User{}, "avatar_key = ?"},
			{&model.Plan{}, "cover_image_key = ?"},
			{&model.Project{}, "cover_image_key = ?"},
			{&model.ProjectAttachment{}, "storekey = ?"},
		}
		for _, check := range checks {
			var count int64
			if err := store.db.WithContext(ctx).Model(check.entity).Where(check.condition, imageKey).Limit(1).Count(&count).Error; err != nil {
				return false, persistenceError(err)
			}
			if count > 0 {
				return true, nil
			}
		}
		return false, nil
	}
	if err != nil {
		return false, persistenceError(err)
	}
	return true, nil
}

// UpdateInspirationImageMetadataIfVersion 在图片 key 和异步代次匹配时仅覆盖服务端已读到的非空值，并原子合并扩展标签。
func (store *PostgresStore) UpdateInspirationImageMetadataIfVersion(ctx context.Context, userID, inspirationID uuid.UUID, imageKey string, asyncVersion int64, metadata model.InspirationImageMetadata) (bool, error) {
	updates := make(map[string]any)
	for column, value := range metadata.DatabaseColumns() {
		if column == "image_metadata_extra" {
			extra := metadata.Extra
			if len(extra) > 2 {
				updates[column] = gorm.Expr("COALESCE(image_metadata_extra, '{}'::jsonb) || ?::jsonb", string(extra))
			}
			continue
		}
		reflected := reflect.ValueOf(value)
		if reflected.IsValid() && (reflected.Kind() == reflect.Pointer || reflected.Kind() == reflect.Slice || reflected.Kind() == reflect.Map) && reflected.IsNil() {
			continue
		}
		updates[column] = value
	}
	if len(updates) == 0 {
		return false, nil
	}
	updates["updated_at"] = time.Now().UTC()
	result := store.db.WithContext(ctx).Model(&model.Inspiration{}).
		Where("user_id = ? AND business_id = ? AND image_key = ? AND async_version = ?", userID, inspirationID, imageKey, asyncVersion).
		Updates(updates)
	if result.Error != nil {
		return false, persistenceError(result.Error)
	}
	return result.RowsAffected > 0, nil
}

// DeleteInspiration 仅物理删除当前用户自己的未被任何项目引用的灵感图。
func (store *PostgresStore) DeleteInspiration(ctx context.Context, userID, inspirationID uuid.UUID) error {
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var inspiration model.Inspiration
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ?", userID, inspirationID).First(&inspiration).Error; err != nil {
			return persistenceError(err)
		}
		var usages int64
		if err := tx.Model(&model.ProjectInspirationUsage{}).Where("inspiration_business_id = ?", inspirationID).Count(&usages).Error; err != nil {
			return persistenceError(err)
		}
		if usages > 0 {
			return fault.Conflict("该灵感图仍被项目引用，请先从所有项目正文中移除")
		}

		// 图片锁先于任务锁，与换图和回写保持同序；删除立即取消未结束的任务，历史计数保持不变。
		if err := tx.Model(&model.InspirationProcessingJob{}).Where("user_id=? AND inspiration_id=? AND stage NOT IN ('succeeded','canceled')", userID, inspirationID).Updates(map[string]any{"stage": "canceled", "lease_token": nil, "lease_until": nil, "updated_at": time.Now().UTC()}).Error; err != nil {
			return persistenceError(err)
		}
		if err := tx.Delete(&inspiration).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
}

// persistenceError 统一映射数据库错误，绝不把 SQL、字段值或底层驱动诊断回传客户端。
func persistenceError(err error) error {
	if err == nil {
		return nil
	}
	var applicationError *fault.Error
	if errors.As(err, &applicationError) {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fault.NotFound()
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return fault.Conflict("唯一字段已存在")
	}
	return fault.AttachSourceFromCaller(fault.Unavailable(err), 2)
}
