package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// libraryChinaTime 只用于入库日界线；数据库时间仍保留时区。
var libraryChinaTime = time.FixedZone("Asia/Shanghai", 8*60*60)

// SetInspirationProcessingAlgorithm 只在启动装配时设置，从统一配置服务取得版本，之后只读。
func (store *PostgresStore) SetInspirationProcessingAlgorithm(value string) {
	store.inspirationAlgorithm = value
}

// DurableInspirationProcessing 标记此 repository 提供持久化队列，禁止再向旧内存队列重复入队。
func (store *PostgresStore) DurableInspirationProcessing() bool { return true }

// recordInspirationActivity 在创建事务内原子累加，不在编辑或删除时回减。
func recordInspirationActivity(tx *gorm.DB, item *model.Inspiration) error {
	var goal model.InspirationGoal
	err := tx.Where("user_id = ?", item.UserID).First(&goal).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var target *int
	if goal.Enabled {
		n := goal.Target
		target = &n
	}
	day := item.CreatedAt.In(libraryChinaTime).Format("2006-01-02")
	row := model.InspirationActivity{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: item.UserID, Day: day, SavedCount: 1, Target: target}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "orgid"}, {Name: "user_id"}, {Name: "day"}}, DoUpdates: clause.Assignments(map[string]any{"saved_count": gorm.Expr("inspiration_activities.saved_count + 1"), "updated_at": time.Now().UTC()})}).Create(&row).Error
}

// enqueueInspirationProcessing 与图片写入共用事务；历史代次任务取消，唯一约束拒绝重复排队。
func (store *PostgresStore) enqueueInspirationProcessing(tx *gorm.DB, item *model.Inspiration) error {
	// 扫描快照可能已被替换；先锁定并重新读取当前图片，避免旧快照取消新代次任务。
	var current model.Inspiration
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=? AND business_id=?", item.UserID, item.BusinessID).First(&current).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	item = &current
	now := time.Now().UTC()
	if err := tx.Model(&model.InspirationProcessingJob{}).Where("user_id = ? AND inspiration_id = ? AND (async_version <> ? OR image_key <> ? OR algorithm <> ?) AND stage NOT IN ('succeeded','canceled')", item.UserID, item.BusinessID, item.AsyncVersion, item.ImageKey, store.inspirationAlgorithm).Updates(map[string]any{"stage": "canceled", "updated_at": now}).Error; err != nil {
		return err
	}
	job := model.InspirationProcessingJob{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: item.UserID, InspirationID: item.BusinessID, ImageKey: item.ImageKey, AsyncVersion: item.AsyncVersion, Algorithm: store.inspirationAlgorithm, Stage: "pending_compression", NextAttemptAt: now}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "orgid"}, {Name: "user_id"}, {Name: "inspiration_id"}, {Name: "async_version"}, {Name: "algorithm"}}, DoNothing: true}).Create(&job).Error
}

// applyInspirationFilters 所有关联均同时约束空间和创建者；不接收客户端的组织或用户条件。
func applyInspirationFilters(q *gorm.DB, ctx context.Context, userID uuid.UUID) *gorm.DB {
	f := model.InspirationFiltersFromContext(ctx)
	if f.DateFrom != "" {
		q = q.Where("created_at >= (?::date::timestamp AT TIME ZONE 'Asia/Shanghai')", f.DateFrom)
	}
	if f.DateTo != "" {
		q = q.Where("created_at < ((?::date + 1)::timestamp AT TIME ZONE 'Asia/Shanghai')", f.DateTo)
	}
	if f.People != "" {
		if f.People == "3+" {
			q = q.Where("analysis_status = 'succeeded' AND people_count >= 3")
		} else {
			q = q.Where("analysis_status = 'succeeded' AND people_count::text = ?", f.People)
		}
	}
	if f.EmptyShot != "" {
		q = q.Where("analysis_status = 'succeeded' AND is_empty_shot = ?", f.EmptyShot == "true")
	}
	if f.Color != "" {
		q = q.Where("color_bucket = ? AND analysis_status = 'succeeded'", f.Color)
	}
	if f.Status != "" {
		q = q.Where("analysis_status = ?", f.Status)
	}
	if f.Project != "" {
		scope, err := tenant.Require(ctx)
		if err != nil {
			q.AddError(err)
			return q
		}
		expression := `EXISTS (SELECT 1 FROM project_inspiration_usages u JOIN projects p ON p.business_id=u.project_business_id AND p.orgid=u.orgid AND p.user_id=u.user_id WHERE u.orgid=? AND u.user_id=? AND u.inspiration_business_id=inspirations.business_id AND p.deleted_at IS NULL`
		if f.Project == "unassigned" {
			q = q.Where("NOT "+expression+")", scope.OrgID, userID)
		} else {
			q = q.Where(expression+" AND p.business_id=?)", scope.OrgID, userID, f.Project)
		}
	}
	return q
}

// libraryUserBoundary 拒绝普通调用把当前会话范围与另一创建者拼接。
func libraryUserBoundary(ctx context.Context, userID uuid.UUID) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if scope.UserID != userID {
		return fault.Unauthorized()
	}
	return nil
}

// GetInspirationGoal 不存在时返回关闭状态，不隐式创建跨空间记录。
func (store *PostgresStore) GetInspirationGoal(ctx context.Context, userID uuid.UUID) (model.InspirationGoal, error) {
	if err := libraryUserBoundary(ctx, userID); err != nil {
		return model.InspirationGoal{}, err
	}
	row := model.InspirationGoal{ReminderTime: "20:00"}
	err := store.db.WithContext(ctx).Where("user_id = ?", userID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.InspirationGoal{ReminderTime: "20:00"}, nil
	}
	return row, persistenceError(err)
}

// SaveInspirationGoal 在同一事务内更新设置和当天快照；历史日期不被重新解释。
func (store *PostgresStore) SaveInspirationGoal(ctx context.Context, userID uuid.UUID, enabled bool, target int, reminder string) error {
	if err := libraryUserBoundary(ctx, userID); err != nil {
		return err
	}
	return persistenceError(store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		goal := model.InspirationGoal{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: userID, Enabled: enabled, Target: target, ReminderTime: reminder}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "orgid"}, {Name: "user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"enabled", "target", "reminder_time", "updated_at"})}).Create(&goal).Error; err != nil {
			return err
		}
		var snapshot *int
		if enabled {
			snapshot = &target
		}
		row := model.InspirationActivity{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: userID, Day: time.Now().In(libraryChinaTime).Format("2006-01-02"), Target: snapshot}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "orgid"}, {Name: "user_id"}, {Name: "day"}}, DoUpdates: clause.Assignments(map[string]any{"target": snapshot, "updated_at": time.Now().UTC()})}).Create(&row).Error
	}))
}

// ListInspirationActivities 按日期投影为 YYYY-MM-DD，避免 date 被驱动格式化成时间戳。
func (store *PostgresStore) ListInspirationActivities(ctx context.Context, userID uuid.UUID, from, to string) ([]model.InspirationActivity, error) {
	if err := libraryUserBoundary(ctx, userID); err != nil {
		return nil, err
	}
	rows := make([]model.InspirationActivity, 0)
	err := store.db.WithContext(ctx).Select("id,business_id,orgid,created_at,updated_at,user_id,day::text AS day,saved_count,target,reminder_notification_id,reminder_acknowledged_at").Where("user_id = ? AND day BETWEEN ?::date AND ?::date", userID, from, to).Order("day").Find(&rows).Error

	if err != nil {
		return nil, persistenceError(err)
	}
	// 目标变更总会写当天快照；零存图或停机日期沿用最近快照，禁用记录的NULL也会正确中断延续。
	var previous model.InspirationActivity
	previousErr := store.db.WithContext(ctx).Select("target").Where("user_id=? AND day<?::date", userID, from).Order("day DESC").First(&previous).Error
	if previousErr != nil && !errors.Is(previousErr, gorm.ErrRecordNotFound) {
		return nil, persistenceError(previousErr)
	}
	start, parseErr := time.Parse("2006-01-02", from)
	if parseErr != nil {
		return nil, fault.InvalidRequest("活动日期格式无效")
	}
	end, parseErr := time.Parse("2006-01-02", to)
	if parseErr != nil || end.Before(start) || end.Sub(start) > 366*24*time.Hour {
		return nil, fault.InvalidRequest("活动日期范围无效")
	}
	today := time.Now().In(libraryChinaTime).Format("2006-01-02")
	byDay := make(map[string]model.InspirationActivity, len(rows))
	for _, row := range rows {
		byDay[row.Day] = row
	}
	result := make([]model.InspirationActivity, 0, len(rows))
	target := previous.Target
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		if key > today {
			break
		}
		if row, ok := byDay[key]; ok {
			target = row.Target
			result = append(result, row)
		} else {
			result = append(result, model.InspirationActivity{Day: key, Target: target})
		}
	}
	return result, nil
}

// AcknowledgeInspirationReminder 原子领取弹窗，同一天跨页面或设备也只成功确认一次。
func (store *PostgresStore) AcknowledgeInspirationReminder(ctx context.Context, userID uuid.UUID, day string) (bool, error) {
	if err := libraryUserBoundary(ctx, userID); err != nil {
		return false, err
	}
	var claimed bool
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.InspirationActivity
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=? AND day=?::date AND reminder_notification_id IS NOT NULL AND reminder_acknowledged_at IS NULL AND target IS NOT NULL AND saved_count < target", userID, day).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		now := time.Now().UTC()
		if err := tx.Model(&row).Update("reminder_acknowledged_at", now).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Notification{}).Where("user_id=? AND business_id=?", userID, *row.ReminderNotificationID).Updates(map[string]any{"read_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return claimed, persistenceError(err)
}

// InspirationFacet 只输出分组计数和业务键，不包含自增 ID 或私有图片内容。
type InspirationFacet = model.InspirationFacet

// InspirationBrowseSummary 汇总当前用户分类及指定月份的现存图片日期，用于轻量日历展示。
func (store *PostgresStore) InspirationBrowseSummary(ctx context.Context, userID uuid.UUID, from, to string) (map[string][]InspirationFacet, error) {
	if err := libraryUserBoundary(ctx, userID); err != nil {
		return nil, err
	}
	scope, err := tenant.Require(ctx)
	if err != nil {
		return nil, err
	}
	result := map[string][]InspirationFacet{}
	for name, expr := range map[string]string{"people": "CASE WHEN analysis_status='succeeded' THEN CASE WHEN people_count>=3 THEN '3+' ELSE people_count::text END ELSE analysis_status END", "colors": "COALESCE(color_bucket,'unknown')", "statuses": "analysis_status"} {
		rows := make([]InspirationFacet, 0)
		if err := store.db.WithContext(ctx).Model(&model.Inspiration{}).Select(expr+" AS key, count(*) AS count").Where("user_id=?", userID).Group(expr).Scan(&rows).Error; err != nil {
			return nil, persistenceError(err)
		}
		result[name] = rows
	}
	days := make([]InspirationFacet, 0)
	if err := store.db.WithContext(ctx).Model(&model.Inspiration{}).Select("to_char(created_at AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD') AS key, count(*) AS count").Where("user_id=? AND created_at >= (?::date::timestamp AT TIME ZONE 'Asia/Shanghai') AND created_at < ((?::date+1)::timestamp AT TIME ZONE 'Asia/Shanghai')", userID, from, to).Group("key").Order("key").Scan(&days).Error; err != nil {
		return nil, persistenceError(err)
	}
	// 每天最多三张预览，窗口函数在数据库裁剪；不为日历加载当月全部文档。
	var previews []model.InspirationPreview
	if err := store.db.WithContext(ctx).Raw(`SELECT business_id,image_key,file_name,day FROM (SELECT business_id,image_key,file_name,to_char(created_at AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD') AS day,row_number() OVER(PARTITION BY (created_at AT TIME ZONE 'Asia/Shanghai')::date ORDER BY created_at DESC,business_id DESC) AS rank FROM inspirations WHERE orgid=? AND user_id=? AND created_at >= (?::date::timestamp AT TIME ZONE 'Asia/Shanghai') AND created_at < ((?::date+1)::timestamp AT TIME ZONE 'Asia/Shanghai')) daily WHERE rank<=3`, scope.OrgID, userID, from, to).Scan(&previews).Error; err != nil {
		return nil, persistenceError(err)
	}
	for i := range days {
		for _, preview := range previews {
			if preview.Day == days[i].Key {
				days[i].Previews = append(days[i].Previews, preview)
			}
		}
	}
	result["days"] = days
	tags := make([]InspirationFacet, 0)
	if err := store.db.WithContext(ctx).Raw(`SELECT t.business_id::text AS key,t.content AS name,count(i.id) AS count FROM tags t LEFT JOIN inspirations i ON i.orgid=t.orgid AND i.user_id=t.user_id AND t.business_id=ANY(i.tag_ids) WHERE t.orgid=? AND t.user_id=? GROUP BY t.business_id,t.content ORDER BY t.content`, scope.OrgID, userID).Scan(&tags).Error; err != nil {
		return nil, persistenceError(err)
	}
	result["tags"] = tags
	projects := make([]InspirationFacet, 0)
	err = store.db.WithContext(ctx).Raw(`SELECT p.business_id::text AS key,p.title AS name,count(u.id) AS count FROM projects p LEFT JOIN project_inspiration_usages u ON u.project_business_id=p.business_id AND u.orgid=p.orgid AND u.user_id=p.user_id AND EXISTS(SELECT 1 FROM inspirations i WHERE i.business_id=u.inspiration_business_id AND i.orgid=u.orgid AND i.user_id=u.user_id) WHERE p.orgid=? AND p.user_id=? AND p.deleted_at IS NULL GROUP BY p.business_id,p.title ORDER BY p.title`, scope.OrgID, userID).Scan(&projects).Error
	result["projects"] = projects
	return result, persistenceError(err)
}

// ScanInspirationProcessingPage 仅用于受控 worker；按内部 ID 游标扫描，逐条恢复私有上下文后写任务。
func (store *PostgresStore) ScanInspirationProcessingPage(ctx context.Context, after int, algorithm string) (int, int, error) {
	if !tenant.IsSystem(ctx) {
		return after, 0, fault.Unauthorized()
	}
	var rows []model.Inspiration
	if err := store.db.WithContext(ctx).Where("id > ? AND (analysis_algorithm <> ? OR analysis_version <> async_version OR analysis_status <> 'succeeded')", after, algorithm).Order("id").Limit(500).Find(&rows).Error; err != nil {
		return after, 0, persistenceError(err)
	}
	for _, item := range rows {
		after = item.ID
		scoped := scopedResourceContext(ctx, item.OrgID, item.UserID)
		if err := store.db.WithContext(scoped).Transaction(func(tx *gorm.DB) error { return store.enqueueInspirationProcessing(tx, &item) }); err != nil {
			return after, 0, persistenceError(err)
		}
	}
	return after, len(rows), nil
}

// ClaimInspirationProcessing 用 SKIP LOCKED 领取单条任务；只对到期租约恢复，领取后生成新的随机令牌。
func (store *PostgresStore) ClaimInspirationProcessing(ctx context.Context, stage string, lease time.Duration) (*model.InspirationProcessingJob, error) {
	if !tenant.IsSystem(ctx) {
		return nil, fault.Unauthorized()
	}
	pending, active := "pending_analysis", "analyzing"
	if stage == "compression" {
		pending, active = "pending_compression", "compressing"
	}
	var job *model.InspirationProcessingJob
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.InspirationProcessingJob
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("((stage=? AND next_attempt_at<=CURRENT_TIMESTAMP) OR (stage=? AND lease_until<CURRENT_TIMESTAMP))", pending, active).
			Where(`orgid=-1 OR EXISTS(SELECT 1 FROM organizations o JOIN organization_members m ON m.orgid=o.orgid WHERE o.orgid=inspiration_processing_jobs.orgid AND o.organization_status='active' AND m.user_id=inspiration_processing_jobs.user_id AND m.member_status='active')`).Order("next_attempt_at,id").First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		tx = tx.WithContext(scopedResourceContext(ctx, row.OrgID, row.UserID))
		until := time.Now().UTC().Add(lease)
		token := uuid.New()
		row.Stage = active
		row.LeaseUntil = &until
		row.LeaseToken = &token
		row.Attempts++
		if err := tx.Model(&row).Updates(map[string]any{"stage": active, "attempts": row.Attempts, "lease_until": until, "lease_token": token, "updated_at": time.Now().UTC()}).Error; err != nil {
			return err
		}
		job = &row
		return nil
	})
	// 领取事务先释放任务行，再更新图片，避免与换图的“图片→任务”锁序形成死锁。
	if err == nil && job != nil {
		scoped := scopedResourceContext(ctx, job.OrgID, job.UserID)
		err = store.db.WithContext(scoped).Model(&model.Inspiration{}).Where("user_id=? AND business_id=? AND async_version=? AND image_key=? AND analysis_status <> 'succeeded'", job.UserID, job.InspirationID, job.AsyncVersion, job.ImageKey).Where("EXISTS(SELECT 1 FROM inspiration_processing_jobs j WHERE j.business_id=? AND j.orgid=? AND j.user_id=? AND j.lease_token=? AND j.stage=?)", job.BusinessID, job.OrgID, job.UserID, job.LeaseToken, active).Update("analysis_status", active).Error
	}
	return job, persistenceError(err)
}

// FinishInspirationProcessing 锁定任务与当前图片后按租约、图片 key 和代次同时比较，过期结果不会覆盖新图。
func (store *PostgresStore) FinishInspirationProcessing(ctx context.Context, job model.InspirationProcessingJob, stage, errorCode string, next time.Time, analysis *model.InspirationAnalysis) error {
	return persistenceError(store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 成员停用或组织停用后不提交迟到画像；租约保留，恢复权限后可重新领取。
		available, availabilityErr := resourceOrganizationAvailable(tx, job.OrgID, job.UserID)
		if availabilityErr != nil || !available {
			return availabilityErr
		}
		var item model.Inspiration
		imageErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=? AND business_id=? AND async_version=? AND image_key=?", job.UserID, job.InspirationID, job.AsyncVersion, job.ImageKey).First(&item).Error
		if errors.Is(imageErr, gorm.ErrRecordNotFound) {
			stage = "canceled"
		} else if imageErr != nil {
			return imageErr
		}
		var current model.InspirationProcessingJob
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=? AND business_id=? AND lease_token=? AND stage=? AND lease_until>CURRENT_TIMESTAMP", job.UserID, job.BusinessID, job.LeaseToken, job.Stage).First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		fields := map[string]any{"stage": stage, "lease_token": nil, "lease_until": nil, "next_attempt_at": next, "error_code": errorCode, "updated_at": time.Now().UTC()}
		if stage == "pending_analysis" && errorCode == "" {
			fields["attempts"] = 0
		}
		if err := tx.Model(&current).Updates(fields).Error; err != nil {
			return err
		}
		if stage == "canceled" {
			return nil
		}
		updates := map[string]any{"analysis_status": stage, "updated_at": time.Now().UTC()}
		if analysis != nil {
			updates["people_count"] = analysis.PeopleCount
			updates["is_empty_shot"] = analysis.IsEmptyShot
			updates["dominant_color"] = analysis.DominantColor
			updates["dominant_hue"] = analysis.DominantHue
			updates["color_bucket"] = analysis.ColorBucket
			updates["color_palette"] = analysis.ColorPalette
			updates["analysis_version"] = job.AsyncVersion
			updates["analysis_algorithm"] = job.Algorithm
			updates["analyzed_at"] = time.Now().UTC()
		}
		return tx.Model(&item).Updates(updates).Error
	}))
}

// RetryInspirationProcessing 只恢复当前用户当前图片的终止失败任务，不更改每日次数或成功结果。
func (store *PostgresStore) RetryInspirationProcessing(ctx context.Context, userID, id uuid.UUID) error {
	item, err := store.FindInspirationByBusinessID(ctx, userID, id)
	if err != nil {
		return err
	}
	return persistenceError(store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := store.enqueueInspirationProcessing(tx, &item); err != nil {
			return err
		}
		result := tx.Model(&model.InspirationProcessingJob{}).Where("user_id=? AND inspiration_id=? AND async_version=? AND algorithm=? AND stage IN ('failed','unsupported')", userID, id, item.AsyncVersion, store.inspirationAlgorithm).Updates(map[string]any{"stage": "pending_compression", "attempts": 0, "error_code": "", "next_attempt_at": time.Now().UTC(), "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fault.Conflict("当前图片没有可重试的失败任务")
		}
		return tx.Model(&item).Update("analysis_status", "pending_compression").Error
	}))
}

// DueInspirationGoalsPage 仅后台按游标发现到点的目标；通知事务会再次校验数量及组织有效性。
func (store *PostgresStore) DueInspirationGoalsPage(ctx context.Context, after int) ([]model.InspirationGoal, error) {
	if !tenant.IsSystem(ctx) {
		return nil, fault.Unauthorized()
	}
	var rows []model.InspirationGoal
	err := store.db.WithContext(ctx).Where("id>? AND enabled=true AND reminder_time<=to_char(CURRENT_TIMESTAMP AT TIME ZONE 'Asia/Shanghai','HH24:MI')", after).Order("id").Limit(500).Find(&rows).Error
	return rows, persistenceError(err)
}

// PublishInspirationGoalNotification 在事务内复核目标并原子写入站内消息与 outbox，事件键保证每天只创建一次。
func (store *PostgresStore) PublishInspirationGoalNotification(ctx context.Context, goal model.InspirationGoal) error {
	return persistenceError(store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		available, err := resourceOrganizationAvailable(tx, goal.OrgID, goal.UserID)
		if err != nil || !available {
			return err
		}
		var current model.InspirationGoal
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("business_id=? AND user_id=? AND enabled=true", goal.BusinessID, goal.UserID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		now := time.Now().UTC()
		local := now.In(libraryChinaTime)
		day := local.Format("2006-01-02")
		if current.ReminderTime > local.Format("15:04") {
			return nil
		}
		row := model.InspirationActivity{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: goal.UserID, Day: day, Target: &current.Target}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "orgid"}, {Name: "user_id"}, {Name: "day"}}, DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=? AND day=?::date", goal.UserID, day).First(&row).Error; err != nil {
			return err
		}
		if row.SavedCount >= current.Target || row.ReminderNotificationID != nil {
			return nil
		}
		payload, _ := json.Marshal(map[string]any{"day": day, "target": current.Target, "saved_count": row.SavedCount})
		note := model.Notification{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: goal.UserID, EventType: "inspiration.goal.unmet", EventKey: "inspiration.goal.unmet:" + day, ResourceType: "inspiration_goal", Payload: model.JSONB(payload)}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "orgid"}, {Name: "user_id"}, {Name: "event_key"}}, DoNothing: true}).Create(&note)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		text := fmt.Sprintf("%s 已存图 %d 张，每日目标 %d 张。", day, row.SavedCount, current.Target)
		webhook, _ := json.Marshal(map[string]string{"title": "灵感库存图提醒", "text": text})
		var configs []model.NoticeConfig
		if err := tx.Where("user_id=? AND enabled=true AND deleted_at IS NULL", goal.UserID).Find(&configs).Error; err != nil {
			return err
		}
		for _, channel := range configs {
			delivery := model.NoticeDelivery{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: goal.UserID, DeliveryKind: "notification", NotificationBusinessID: &note.BusinessID, NoticeConfigBusinessID: channel.BusinessID, Provider: channel.Provider, ConfigName: channel.Name, EventType: note.EventType, Payload: model.JSONB(webhook), PartIndex: 1, PartCount: 1, Status: "pending", NextAttemptAt: now}
			if err := tx.Create(&delivery).Error; err != nil {
				return err
			}
		}
		return tx.Model(&row).Updates(map[string]any{"reminder_notification_id": note.BusinessID, "target": current.Target, "updated_at": now}).Error
	}))
}
