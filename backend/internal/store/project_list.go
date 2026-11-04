package store

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/model"
	"gorm.io/gorm"
)

// ListProjectsFiltered 在一致只读快照内计算完整分类计数并分页，仅选择卡片所需小字段。
// 正文和人工总结不会在列表查询中被加载；组织及创建者边界由可信上下文共同限定。
func (store *PostgresStore) ListProjectsFiltered(ctx context.Context, user uuid.UUID, query, status string, offset, limit int) ([]model.Project, int64, model.ProjectStatusCounts, error) {
	if err := projectUserBoundary(ctx, user); err != nil {
		return nil, 0, model.ProjectStatusCounts{}, err
	}
	items := make([]model.Project, 0)
	counts := model.ProjectStatusCounts{}
	var total int64
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		base := func() *gorm.DB {
			q := tx.Model(&model.Project{}).Where("user_id = ? AND deleted_at IS NULL", user)
			if query != "" {
				q = q.Where("search_grams @> ? AND strpos(search_text, ?) > 0", pq.StringArray(projectSearchGrams(query)), query)
			}
			return q
		}
		var grouped []struct {
			Status string
			Count  int64
		}
		if err := base().Select("status, count(*) AS count").Group("status").Scan(&grouped).Error; err != nil {
			return persistenceError(err)
		}
		for _, row := range grouped {
			switch row.Status {
			case "unfinished":
				counts.Unfinished = row.Count
			case "executed":
				counts.Executed = row.Count
			case "completed":
				counts.Completed = row.Count
			}
			if status == "" || status == row.Status {
				total += row.Count
			}
		}
		q := base()
		if status != "" {
			q = q.Where("status = ?", status)
		}
		return persistenceError(q.Select("business_id", "title", "cover_emoji", "cover_image_key", "cover_file_name", "revision", "created_at", "updated_at", "default_read_only", "status", "shoot_start_at", "shoot_end_at", "shoot_location", "shoot_person", "shoot_kind").Order("updated_at DESC, business_id ASC").Offset(offset).Limit(limit).Find(&items).Error)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return items, total, counts, err
}
