package store

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ListProjects 按用户范围分页检索项目；GIN gram 负责短词候选，strpos 再核对真实连续子串。
func (store *PostgresStore) ListProjects(ctx context.Context, userID uuid.UUID, query string, offset, pageSize int) ([]model.Project, int64, error) {
	base := store.db.WithContext(ctx).Model(&model.Project{}).Where("user_id = ? AND deleted_at IS NULL", userID)
	if query != "" {
		grams := projectSearchGrams(query)
		base = base.Where("search_grams @> ? AND strpos(search_text, ?) > 0", pq.StringArray(grams), query)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	var items []model.Project
	if err := base.Select("business_id", "title", "cover_emoji", "cover_image_key", "cover_file_name", "revision", "created_at", "updated_at").Order("updated_at DESC, created_at DESC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	return items, total, nil
}

// FindProject 同时限定项目业务 UUID、所属用户和未删除状态。
func (store *PostgresStore) FindProject(ctx context.Context, userID, projectID uuid.UUID) (model.Project, error) {
	var item model.Project
	if err := store.db.WithContext(ctx).Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, projectID).First(&item).Error; err != nil {
		return model.Project{}, persistenceError(err)
	}
	return item, nil
}

// CreateProject 在一个事务中保存文档并同步灵感图、封面和正文附件关系。
func (store *PostgresStore) CreateProject(ctx context.Context, item *model.Project, inspirations []uuid.UUID, attachments []model.ProjectAttachment) error {
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		item.CreatedAt, item.UpdatedAt = now, now
		if err := lockProjectInspirations(tx, item.UserID, inspirations); err != nil {
			return err
		}
		if err := tx.Create(item).Error; err != nil {
			return persistenceError(err)
		}
		if err := syncProjectInspirations(tx, item.UserID, item.BusinessID, inspirations, now); err != nil {
			return err
		}
		return syncProjectAttachments(tx, item.UserID, item.BusinessID, attachments, now)
	})
}

// UpdateProject 锁定当前修订并同时提交文档、索引文本及资源关联，避免自动保存交叉覆盖。
func (store *PostgresStore) UpdateProject(ctx context.Context, userID, projectID uuid.UUID, revision int64, updates map[string]any, inspirations []uuid.UUID, attachments []model.ProjectAttachment) (model.Project, error) {
	var updated model.Project
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.Project
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, projectID).First(&current).Error; err != nil {
			return persistenceError(err)
		}
		if current.Revision != revision {
			return fault.Conflict("项目已在其他位置更新，请重新加载后继续编辑")
		}
		if err := lockProjectInspirations(tx, userID, inspirations); err != nil {
			return err
		}
		now := time.Now().UTC()
		updates["updated_at"], updates["revision"] = now, current.Revision+1
		if err := tx.Model(&model.Project{}).Where("user_id = ? AND business_id = ?", userID, projectID).Updates(updates).Error; err != nil {
			return persistenceError(err)
		}
		if err := syncProjectInspirations(tx, userID, projectID, inspirations, now); err != nil {
			return err
		}
		if err := syncProjectAttachments(tx, userID, projectID, attachments, now); err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, projectID).First(&updated).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
	return updated, err
}

// DeleteProject 软删除项目并保留正文及所有资源关系，引用中的灵感图仍不能被删除。
func (store *PostgresStore) DeleteProject(ctx context.Context, userID, projectID uuid.UUID) error {
	result := store.db.WithContext(ctx).Model(&model.Project{}).Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, projectID).
		Updates(map[string]any{"deleted_at": time.Now().UTC(), "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return persistenceError(result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

// FindProjectAttachment 将附件授权限定到项目拥有者、项目和附件业务 UUID 三个维度。
func (store *PostgresStore) FindProjectAttachment(ctx context.Context, userID, projectID uuid.UUID, storeKey string) (model.ProjectAttachment, error) {
	var project model.Project
	if err := store.db.WithContext(ctx).Select("business_id").Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, projectID).First(&project).Error; err != nil {
		return model.ProjectAttachment{}, persistenceError(err)
	}
	var item model.ProjectAttachment
	if err := store.db.WithContext(ctx).Where("user_id = ? AND project_business_id = ? AND storekey = ?", userID, projectID, storeKey).First(&item).Error; err != nil {
		return model.ProjectAttachment{}, persistenceError(err)
	}
	return item, nil
}

// ValidateProjectInspirations 确认所有引用均属于当前用户；保存事务还会在修改关联前再次锁定校验。
func (store *PostgresStore) ValidateProjectInspirations(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	var count int64
	if err := store.db.WithContext(ctx).Model(&model.Inspiration{}).Where("user_id = ? AND business_id IN ?", userID, ids).Count(&count).Error; err != nil {
		return persistenceError(err)
	}
	if count != int64(len(ids)) {
		return fault.InvalidField("document", "项目引用的灵感图不存在或不属于当前账号")
	}
	return nil
}

// lockProjectInspirations 以共享键锁与灵感图删除事务串行化，防止并发保存留下失效引用。
func lockProjectInspirations(tx *gorm.DB, userID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	// 多个保存事务以相同顺序锁灵感图，避免相交引用集合按随机 map 次序造成死锁。
	orderedIDs := append([]uuid.UUID(nil), ids...)
	sort.Slice(orderedIDs, func(i, j int) bool { return orderedIDs[i].String() < orderedIDs[j].String() })
	var found []model.Inspiration
	if err := tx.Clauses(clause.Locking{Strength: "KEY SHARE"}).Select("business_id").Where("user_id = ? AND business_id IN ?", userID, orderedIDs).Order("business_id").Find(&found).Error; err != nil {
		return persistenceError(err)
	}
	if len(found) != len(orderedIDs) {
		return fault.InvalidField("document", "项目引用的灵感图不存在或不属于当前账号")
	}
	return nil
}

func syncProjectInspirations(tx *gorm.DB, userID, projectID uuid.UUID, ids []uuid.UUID, now time.Time) error {
	query := tx.Where("user_id = ? AND project_business_id = ?", userID, projectID)
	if len(ids) == 0 {
		if err := query.Delete(&model.ProjectInspirationUsage{}).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	}
	if err := query.Where("inspiration_business_id NOT IN ?", ids).Delete(&model.ProjectInspirationUsage{}).Error; err != nil {
		return persistenceError(err)
	}
	for _, id := range ids {
		var count int64
		if err := tx.Model(&model.ProjectInspirationUsage{}).Where("user_id = ? AND project_business_id = ? AND inspiration_business_id = ?", userID, projectID, id).Count(&count).Error; err != nil {
			return persistenceError(err)
		}
		if count > 0 {
			continue
		}
		row := model.ProjectInspirationUsage{BaseModel: model.BaseModel{BusinessID: uuid.New(), CreatedAt: now, UpdatedAt: now}, UserID: userID, ProjectBusinessID: projectID, InspirationBusinessID: id}
		if err := tx.Create(&row).Error; err != nil {
			return persistenceError(err)
		}
	}
	return nil
}

func syncProjectAttachments(tx *gorm.DB, userID, projectID uuid.UUID, items []model.ProjectAttachment, now time.Time) error {
	keys := make([]string, 0, len(items))
	byKey := make(map[string]model.ProjectAttachment, len(items))
	for _, item := range items {
		if previous, exists := byKey[item.StoreKey]; exists {
			if previous.AssetRole != item.AssetRole {
				previous.AssetRole = "both"
			}
			byKey[item.StoreKey] = previous
		} else {
			byKey[item.StoreKey] = item
			keys = append(keys, item.StoreKey)
		}
	}
	query := tx.Where("user_id = ? AND project_business_id = ?", userID, projectID)
	if len(keys) == 0 {
		if err := query.Delete(&model.ProjectAttachment{}).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	}
	if err := query.Where("storekey NOT IN ?", keys).Delete(&model.ProjectAttachment{}).Error; err != nil {
		return persistenceError(err)
	}
	for _, key := range keys {
		item := byKey[key]
		item.ID, item.BusinessID, item.CreatedAt, item.UpdatedAt = 0, uuid.New(), now, now
		item.UserID, item.ProjectBusinessID = userID, projectID
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "project_business_id"}, {Name: "storekey"}}, DoUpdates: clause.Assignments(map[string]any{"file_name": item.FileName, "file_size": item.FileSize, "content_type": item.ContentType, "asset_role": item.AssetRole, "updated_at": now})}).Create(&item).Error; err != nil {
			return persistenceError(err)
		}
	}
	return nil
}

func projectSearchGrams(query string) []string {
	runes := []rune(strings.ToLower(query))
	set := map[string]struct{}{}
	for i, r := range runes {
		set[string(r)] = struct{}{}
		if i+1 < len(runes) {
			set[string(runes[i:i+2])] = struct{}{}
		}
	}
	grams := make([]string, 0, len(set))
	for gram := range set {
		grams = append(grams, gram)
	}
	return grams
}
