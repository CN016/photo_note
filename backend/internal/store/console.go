package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IsAdministrator 每次查询当前数据库角色，不使用 token 中可长期缓存的权限副本。
func (store *PostgresStore) IsAdministrator(ctx context.Context, userID uuid.UUID) (bool, error) {
	var user model.User
	err := store.db.WithContext(ctx).Select("business_id", "is_admin").Where("business_id = ?", userID).First(&user).Error
	if err != nil {
		return false, persistenceError(err)
	}
	return user.IsAdmin, nil
}

// ListConsoleUsers 仅选择用户管理需要的资料，不读取密码哈希或头像存储键。
func (store *PostgresStore) ListConsoleUsers(ctx context.Context, search string, offset, limit int) ([]model.ConsoleUser, int64, error) {
	query := store.db.WithContext(ctx).Model(&model.User{})
	if search != "" {
		pattern := escapedContainsPattern(search)
		query = query.Where("nickname ILIKE ? ESCAPE E'\\\\' OR phone ILIKE ? ESCAPE E'\\\\'", pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	items := make([]model.ConsoleUser, 0)
	err := query.Select("business_id", "nickname", "phone", "is_admin", "created_at").
		Order("created_at DESC, business_id DESC").Offset(offset).Limit(limit).Scan(&items).Error
	if err != nil {
		return nil, 0, persistenceError(err)
	}
	return items, total, nil
}

// SetUserAdministrator 以事务级 advisory lock 串行化角色变更，避免并发撤销移除最后一位管理员。
func (store *PostgresStore) SetUserAdministrator(ctx context.Context, userID uuid.UUID, isAdmin bool) error {
	return persistenceError(store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(741920231, 17)").Error; err != nil {
			return err
		}
		var user model.User
		if err := tx.Select("business_id", "is_admin").Where("business_id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		if user.IsAdmin == isAdmin {
			return nil
		}
		if !isAdmin {
			var count int64
			if err := tx.Model(&model.User{}).Where("is_admin = ?", true).Count(&count).Error; err != nil {
				return err
			}
			if count <= 1 {
				return fault.Conflict("至少需要保留一位管理员")
			}
		}
		result := tx.Model(&model.User{}).Where("business_id = ?", userID).
			Updates(map[string]any{"is_admin": isAdmin, "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	}))
}

// ListConsoleConfigs 返回配置表实体供 service 统一执行秘密脱敏后再序列化。
func (store *PostgresStore) ListConsoleConfigs(ctx context.Context) ([]model.AppConfig, error) {
	items := make([]model.AppConfig, 0)
	if err := store.db.WithContext(ctx).Order("config_key ASC").Find(&items).Error; err != nil {
		return nil, persistenceError(err)
	}
	return items, nil
}

// FindConsoleConfig 按唯一配置 key 读取当前值；秘密字段只会在 service 内存中使用。
func (store *PostgresStore) FindConsoleConfig(ctx context.Context, key string) (model.AppConfig, error) {
	var item model.AppConfig
	if err := store.db.WithContext(ctx).Where("config_key = ?", key).First(&item).Error; err != nil {
		return model.AppConfig{}, persistenceError(err)
	}
	return item, nil
}

// CreateConsoleConfig 保存管理者新增的配置项并由统一业务 UUID 标识。
func (store *PostgresStore) CreateConsoleConfig(ctx context.Context, item *model.AppConfig) error {
	if err := store.db.WithContext(ctx).Create(item).Error; err != nil {
		return persistenceError(err)
	}
	return nil
}

// UpdateConsoleConfig 只写入 service 白名单字段，再从数据库读取最终值供脱敏 DTO 映射。
func (store *PostgresStore) UpdateConsoleConfig(ctx context.Context, key string, updates map[string]any) (model.AppConfig, error) {
	updates["updated_at"] = time.Now().UTC()
	result := store.db.WithContext(ctx).Model(&model.AppConfig{}).Where("config_key = ?", key).Updates(updates)
	if result.Error != nil {
		return model.AppConfig{}, persistenceError(result.Error)
	}
	if result.RowsAffected == 0 {
		return model.AppConfig{}, fault.NotFound()
	}
	return store.FindConsoleConfig(ctx, key)
}

// ListConsoleInspirations 提供全站只读图片摘要，明确不选择私人编辑文档 data 字段。
func (store *PostgresStore) ListConsoleInspirations(ctx context.Context, search, sort string, offset, limit int) ([]model.ConsoleInspiration, int64, error) {
	query := store.db.WithContext(ctx).Table("inspirations AS i").Joins("LEFT JOIN users AS u ON u.business_id = i.user_id")
	if search != "" {
		pattern := escapedContainsPattern(search)
		query = query.Where("i.file_name ILIKE ? ESCAPE E'\\\\' OR u.nickname ILIKE ? ESCAPE E'\\\\'", pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	order := map[string]string{"created_desc": "i.created_at DESC, i.business_id DESC", "created_asc": "i.created_at ASC, i.business_id ASC", "updated_desc": "i.updated_at DESC, i.business_id DESC"}[sort]
	if order == "" {
		order = "i.created_at DESC, i.business_id DESC"
	}
	items := make([]model.ConsoleInspiration, 0)
	err := query.Select("i.business_id, i.user_id, COALESCE(u.nickname, '') AS owner_nickname, i.tag_ids, i.image_key, i.file_name, i.created_at, i.updated_at").
		Order(order).Offset(offset).Limit(limit).Scan(&items).Error
	if err != nil {
		return nil, 0, persistenceError(err)
	}
	return items, total, nil
}

// ListConsoleFeedback 按状态读取反馈及提交者昵称；游客或已删除用户使用中性称呼。
func (store *PostgresStore) ListConsoleFeedback(ctx context.Context, status string, offset, limit int) ([]model.ConsoleFeedback, int64, error) {
	query := store.db.WithContext(ctx).Table("feedbacks AS f").Joins("LEFT JOIN users AS u ON u.business_id = f.user_id")
	if status != "" {
		query = query.Where("f.feedback_status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	items := make([]model.ConsoleFeedback, 0)
	err := query.Select("f.business_id, f.user_id, COALESCE(u.nickname, '游客') AS nickname, f.content, f.image_keys, f.reply_content, f.reply_image_keys, f.feedback_status, f.created_at, f.updated_at").
		Order("f.created_at DESC, f.business_id DESC").Offset(offset).Limit(limit).Scan(&items).Error
	if err != nil {
		return nil, 0, persistenceError(err)
	}
	return items, total, nil
}

// GetConsoleFeedback 读取单条反馈的用户关联和现有回复图片，用于验证管理员提交的保留列表。
func (store *PostgresStore) GetConsoleFeedback(ctx context.Context, feedbackID uuid.UUID) (model.Feedback, error) {
	var feedback model.Feedback
	err := store.db.WithContext(ctx).Select("business_id", "user_id", "reply_content", "reply_image_keys").
		Where("business_id = ?", feedbackID).First(&feedback).Error
	return feedback, persistenceError(err)
}

// UpdateConsoleFeedbackReply 将回复与站内通知、所有启用渠道的 outbox 放在同一事务内提交。
func (store *PostgresStore) UpdateConsoleFeedbackReply(ctx context.Context, feedbackID uuid.UUID, content string, retainedKeys, newKeys []string, eventKey string, payload model.JSONB, parts []string) error {
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var feedback model.Feedback
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("business_id", "user_id", "reply_image_keys").
			Where("business_id = ?", feedbackID).First(&feedback).Error; err != nil {
			return err
		}
		existing := make(map[string]struct{}, len(feedback.ReplyImageKeys))
		for _, key := range feedback.ReplyImageKeys {
			existing[key] = struct{}{}
		}
		seen := make(map[string]struct{}, len(retainedKeys)+len(newKeys))
		for _, key := range retainedKeys {
			if _, ok := existing[key]; !ok {
				return fault.InvalidField("kept_image_keys", "只能保留当前反馈已有的回复图片")
			}
			if _, duplicate := seen[key]; duplicate {
				return fault.InvalidField("kept_image_keys", "回复图片列表不能包含重复项目")
			}
			seen[key] = struct{}{}
		}
		for _, key := range newKeys {
			if _, duplicate := seen[key]; duplicate {
				return fault.InvalidField("images", "回复图片列表不能包含重复项目")
			}
			seen[key] = struct{}{}
		}
		imageKeys := make([]string, 0, len(seen))
		imageKeys = append(imageKeys, retainedKeys...)
		imageKeys = append(imageKeys, newKeys...)
		if len(imageKeys) > 5 {
			return fault.InvalidField("images", "回复图片最多五张")
		}
		now := time.Now().UTC()
		result := tx.Model(&model.Feedback{}).Where("business_id = ?", feedbackID).Updates(map[string]any{
			"reply_content": content, "reply_image_keys": pq.StringArray(imageKeys), "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if feedback.UserID == nil || (strings.TrimSpace(content) == "" && len(imageKeys) == 0) {
			return nil
		}
		if eventKey == "" || len(parts) == 0 || !json.Valid(payload) {
			return errors.New("反馈回复通知载荷无效")
		}
		userID := *feedback.UserID
		notification := model.Notification{
			BaseModel:          model.BaseModel{BusinessID: uuid.New(), CreatedAt: now, UpdatedAt: now},
			UserID:             userID,
			EventType:          "feedback.replied",
			EventKey:           eventKey,
			ResourceType:       "feedback",
			ResourceBusinessID: &feedbackID,
			Payload:            payload,
		}
		inserted := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "event_key"}},
			DoNothing: true,
		}).Create(&notification)
		if inserted.Error != nil {
			return inserted.Error
		}
		if inserted.RowsAffected == 0 {
			return nil
		}
		var configs []model.NoticeConfig
		if err := tx.Where("user_id = ? AND enabled = TRUE AND deleted_at IS NULL", userID).Order("created_at ASC").Find(&configs).Error; err != nil {
			return err
		}
		for _, config := range configs {
			for index, part := range parts {
				partIndex := index + 1
				partTitle := fmt.Sprintf("反馈中心已回复（%d/%d）", partIndex, len(parts))
				partPayload, err := json.Marshal(map[string]any{
					"title": partTitle, "markdown": part, "part_index": partIndex, "part_count": len(parts),
				})
				if err != nil {
					return err
				}
				delivery := model.NoticeDelivery{
					BaseModel: model.BaseModel{BusinessID: uuid.New(), CreatedAt: now, UpdatedAt: now},
					UserID:    userID, DeliveryKind: "notification", NotificationBusinessID: &notification.BusinessID,
					NoticeConfigBusinessID: config.BusinessID, Provider: config.Provider, ConfigName: config.Name,
					EventType: "feedback.replied", Payload: model.JSONB(partPayload), PartIndex: partIndex, PartCount: len(parts),
					Status: "pending", NextAttemptAt: now,
				}
				if err := tx.Create(&delivery).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err == nil {
		return nil
	}
	var applicationError *fault.Error
	if errors.As(err, &applicationError) && applicationError.Status < 500 {
		return err
	}
	return persistenceError(err)
}

// UpdateConsoleFeedbackStatus 只更新预先校验的反馈状态，不允许管理页修改或删除正文与附件。
func (store *PostgresStore) UpdateConsoleFeedbackStatus(ctx context.Context, feedbackID uuid.UUID, status string) error {
	result := store.db.WithContext(ctx).Model(&model.Feedback{}).Where("business_id = ?", feedbackID).
		Updates(map[string]any{"feedback_status": status, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return persistenceError(result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

// escapedContainsPattern 将输入作为字面检索内容，不允许用户提供的 % 或 _ 扩大匹配范围。
func escapedContainsPattern(value string) string {
	escaped := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
	return "%" + escaped + "%"
}
