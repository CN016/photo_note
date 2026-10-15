package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PostgresStore 把所有 GORM 查询集中在 repository 层，service 不直接拼接或执行数据库语句。
type PostgresStore struct {
	db *gorm.DB
}

// NewPostgresStore 创建只封装查询能力的 repository，不会触发 AutoMigrate 或其他 DDL。
func NewPostgresStore(db *gorm.DB) *PostgresStore {
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
		Where("is_public = ?", true)
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
		Select("business_id", "nickname", "phone", "avatar_key", "created_at", "updated_at").
		Where("business_id = ?", businessID).First(&user).Error
	if err != nil {
		return model.User{}, persistenceError(err)
	}
	return user, nil
}

// FindUserPasswordHash 仅供密码修改验证当前凭据，返回字段不会进入用户资料 DTO。
func (store *PostgresStore) FindUserPasswordHash(ctx context.Context, businessID uuid.UUID) (model.User, error) {
	var user model.User
	err := store.db.WithContext(ctx).Select("business_id", "password_hash").
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

// CreateFeedback 持久化游客或已认证用户的反馈；图片只保存受服务端验证的 storekey 数组。
func (store *PostgresStore) CreateFeedback(ctx context.Context, feedback *model.Feedback) error {
	if err := store.db.WithContext(ctx).Create(feedback).Error; err != nil {
		return persistenceError(err)
	}
	return nil
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

// ListTags 按当前用户隔离地分页查询标签，并单独统计匹配总数用于稳定分页响应。
func (store *PostgresStore) ListTags(ctx context.Context, userID uuid.UUID, offset, limit int) ([]model.Tag, int64, error) {
	query := store.db.WithContext(ctx).Model(&model.Tag{}).Where("user_id = ?", userID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	items := make([]model.Tag, 0)
	if err := query.Order("created_at DESC, business_id DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	return items, total, nil
}

// CreateTag 保存归属于当前用户业务 UUID 的标签。
func (store *PostgresStore) CreateTag(ctx context.Context, tag *model.Tag) error {
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
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tag model.Tag
		if err := tx.Where("user_id = ? AND business_id = ?", userID, tagID).First(&tag).Error; err != nil {
			return persistenceError(err)
		}
		if err := tx.Exec(`
			UPDATE inspirations
			SET tag_ids = array_remove(tag_ids, ?::uuid), updated_at = CURRENT_TIMESTAMP
			WHERE user_id = ? AND ?::uuid = ANY(tag_ids)
		`, tagID.String(), userID, tagID.String()).Error; err != nil {
			return persistenceError(err)
		}
		if err := tx.Delete(&tag).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
}

// ValidateTagOwnership 确认所有标签业务 UUID 均存在且属于同一当前用户，不创建数据库外键。
func (store *PostgresStore) ValidateTagOwnership(ctx context.Context, userID uuid.UUID, tagIDs []uuid.UUID) error {
	if len(tagIDs) == 0 {
		return nil
	}
	var count int64
	if err := store.db.WithContext(ctx).Model(&model.Tag{}).
		Where("user_id = ? AND business_id IN ?", userID, tagIDs).Count(&count).Error; err != nil {
		return persistenceError(err)
	}
	if count != int64(len(tagIDs)) {
		return fault.InvalidField("tag_ids", "存在不存在或不属于当前用户的标签")
	}
	return nil
}

// CreateInspiration 保存灵感图及其版本化 JSONB 数据。
func (store *PostgresStore) CreateInspiration(ctx context.Context, inspiration *model.Inspiration) error {
	if err := store.db.WithContext(ctx).Create(inspiration).Error; err != nil {
		return persistenceError(err)
	}
	return nil
}

// ListInspirations 按用户、标签任一/全部匹配规则和稳定排序执行分页检索。
func (store *PostgresStore) ListInspirations(ctx context.Context, userID uuid.UUID, tagIDs []uuid.UUID, match string, offset, limit int) ([]model.Inspiration, int64, error) {
	query := store.db.WithContext(ctx).Model(&model.Inspiration{}).Where("user_id = ?", userID)
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
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	items := make([]model.Inspiration, 0)
	if err := query.Order("created_at DESC, business_id DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	return items, total, nil
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
	return inspiration, nil
}

// UpdateInspiration 更新 service 已验证字段，并限定当前用户避免通过 UUID 越权写入。
func (store *PostgresStore) UpdateInspiration(ctx context.Context, userID, inspirationID uuid.UUID, updates map[string]any) (model.Inspiration, error) {
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
	updates["async_version"] = gorm.Expr("async_version + 1")
	updates["updated_at"] = time.Now().UTC()
	var inspiration model.Inspiration
	result := store.db.WithContext(ctx).Model(&inspiration).Clauses(clause.Returning{}).
		Where("user_id = ? AND business_id = ?", userID, inspirationID).Updates(updates)
	if result.Error != nil {
		return model.Inspiration{}, persistenceError(result.Error)
	}
	if result.RowsAffected == 0 {
		return model.Inspiration{}, fault.NotFound()
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

// DeleteInspiration 仅物理删除当前用户自己的灵感图。
func (store *PostgresStore) DeleteInspiration(ctx context.Context, userID, inspirationID uuid.UUID) error {
	result := store.db.WithContext(ctx).Where("user_id = ? AND business_id = ?", userID, inspirationID).
		Delete(&model.Inspiration{})
	if result.Error != nil {
		return persistenceError(result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

// persistenceError 统一映射数据库错误，绝不把 SQL、字段值或底层驱动诊断回传客户端。
func persistenceError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fault.NotFound()
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return fault.Conflict("唯一字段已存在")
	}
	return fault.Unavailable(err)
}
