package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const planReminderEventType = "plan.reminder.due"

// ListPlans 只返回用户自己的有效计划，并加载仍属于当前计划提醒配置的提前分钟数。
func (store *PostgresStore) ListPlans(ctx context.Context, userID uuid.UUID) ([]model.Plan, error) {
	var items []model.Plan
	if err := store.db.WithContext(ctx).Where("user_id = ? AND deleted_at IS NULL", userID).
		Order("starts_at ASC, created_at ASC").Find(&items).Error; err != nil {
		return nil, persistenceError(err)
	}
	if err := store.loadPlanReminderOffsets(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

// FindPlan 双重限定用户和业务 UUID，隐藏已软删除资源并加载当前提醒设置。
func (store *PostgresStore) FindPlan(ctx context.Context, userID, planID uuid.UUID) (model.Plan, error) {
	var item model.Plan
	if err := store.db.WithContext(ctx).Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, planID).First(&item).Error; err != nil {
		return model.Plan{}, persistenceError(err)
	}
	items := []model.Plan{item}
	if err := store.loadPlanReminderOffsets(ctx, items); err != nil {
		return model.Plan{}, err
	}
	return items[0], nil
}

// CreatePlan 在同一事务中保存计划和最多三条待发送提醒；所有关联均使用业务 UUID 且不创建外键。
func (store *PostgresStore) CreatePlan(ctx context.Context, item *model.Plan, offsets []int) error {
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		item.CreatedAt, item.UpdatedAt = now, now
		if err := tx.Create(item).Error; err != nil {
			return persistenceError(err)
		}
		reminders := makePlanReminderRows(item, offsets, now)
		if len(reminders) > 0 {
			if err := tx.Create(&reminders).Error; err != nil {
				return persistenceError(err)
			}
		}
		item.ReminderOffsets = append([]int(nil), offsets...)
		return nil
	})
}

// UpdatePlan 锁定计划后应用部分字段；时间或提醒配置变化时只取消未发送任务并保留已发送历史。
func (store *PostgresStore) UpdatePlan(ctx context.Context, userID, planID uuid.UUID, updates map[string]any, offsets []int, reschedule bool) (model.Plan, error) {
	var updated model.Plan
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.Plan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, planID).First(&current).Error; err != nil {
			return persistenceError(err)
		}
		if current.Status != "active" {
			return fault.Conflict("已完成计划不能再修改")
		}
		now := time.Now().UTC()
		updates["updated_at"] = now
		if len(updates) > 1 || reschedule {
			if err := tx.Model(&model.Plan{}).Where("user_id = ? AND business_id = ?", userID, planID).Updates(updates).Error; err != nil {
				return persistenceError(err)
			}
		}
		if reschedule {
			if err := reschedulePlanReminders(tx, current, updates, offsets, now); err != nil {
				return err
			}
		}
		if err := tx.Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, planID).First(&updated).Error; err != nil {
			return persistenceError(err)
		}
		if err := loadPlanReminderOffsetsTx(tx, &updated); err != nil {
			return err
		}
		return nil
	})
	return updated, err
}

// CompletePlan 原子地保留计划并取消全部 pending 提醒，重复完成请求返回同一已完成计划。
func (store *PostgresStore) CompletePlan(ctx context.Context, userID, planID uuid.UUID) (model.Plan, error) {
	var item model.Plan
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, planID).First(&item).Error; err != nil {
			return persistenceError(err)
		}
		if item.Status == "active" {
			now := time.Now().UTC()
			if err := tx.Model(&model.Plan{}).Where("user_id = ? AND business_id = ?", userID, planID).
				Updates(map[string]any{"plan_status": "completed", "completed_at": now, "updated_at": now}).Error; err != nil {
				return persistenceError(err)
			}
			if err := tx.Model(&model.PlanReminder{}).Where("user_id = ? AND plan_business_id = ? AND reminder_status = 'pending'", userID, planID).
				Updates(map[string]any{"reminder_status": "cancelled", "updated_at": now}).Error; err != nil {
				return persistenceError(err)
			}
			item.Status, item.CompletedAt, item.UpdatedAt = "completed", &now, now
		}
		return loadPlanReminderOffsetsTx(tx, &item)
	})
	return item, err
}

// DeletePlan 软删除用户计划并原子取消未发送任务，已创建的站内消息与投递历史继续保留。
func (store *PostgresStore) DeletePlan(ctx context.Context, userID, planID uuid.UUID) error {
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item model.Plan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, planID).First(&item).Error; err != nil {
			return persistenceError(err)
		}
		now := time.Now().UTC()
		if err := tx.Model(&model.Plan{}).Where("user_id = ? AND business_id = ?", userID, planID).
			Updates(map[string]any{"plan_status": "deleted", "deleted_at": now, "updated_at": now}).Error; err != nil {
			return persistenceError(err)
		}
		if err := tx.Model(&model.PlanReminder{}).Where("user_id = ? AND plan_business_id = ? AND reminder_status = 'pending'", userID, planID).
			Updates(map[string]any{"reminder_status": "cancelled", "updated_at": now}).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
}

// DuePlanReminderIDs 按数据库当前时间取消已逾期计划提醒，并读取尚未开始计划的已到期任务。
func (store *PostgresStore) DuePlanReminderIDs(ctx context.Context, limit int) ([]uuid.UUID, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	var ids []uuid.UUID
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			UPDATE plan_reminders AS reminder
			SET reminder_status = 'cancelled', updated_at = CURRENT_TIMESTAMP
			FROM plans AS plan
			WHERE reminder.plan_business_id = plan.business_id
			  AND reminder.reminder_status = 'pending'
			  AND (plan.plan_status <> 'active' OR plan.deleted_at IS NOT NULL OR plan.starts_at <= CURRENT_TIMESTAMP)
		`).Error; err != nil {
			return persistenceError(err)
		}
		if err := tx.Table("plan_reminders AS reminder").
			Joins("JOIN plans AS plan ON plan.business_id = reminder.plan_business_id").
			Where("reminder.reminder_status = 'pending' AND plan.plan_status = 'active' AND plan.deleted_at IS NULL AND plan.starts_at > CURRENT_TIMESTAMP AND reminder.remind_at <= CURRENT_TIMESTAMP").
			Order("reminder.remind_at ASC, reminder.created_at ASC").Limit(limit).
			Pluck("reminder.business_id", &ids).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
	return ids, err
}

// CreatePlanReminderNotification 锁定计划与提醒并重验数据库时间，在一笔事务中写消息、webhook outbox 和 sent 状态。
func (store *PostgresStore) CreatePlanReminderNotification(ctx context.Context, reminderID uuid.UUID) (bool, error) {
	created := false
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var identity struct {
			PlanBusinessID uuid.UUID `gorm:"column:plan_business_id"`
		}
		if err := tx.Model(&model.PlanReminder{}).Select("plan_business_id").Where("business_id = ?", reminderID).First(&identity).Error; err != nil {
			return persistenceError(err)
		}
		var plan model.Plan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("business_id = ?", identity.PlanBusinessID).First(&plan).Error; err != nil {
			return persistenceError(err)
		}
		var reminder model.PlanReminder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("business_id = ?", reminderID).First(&reminder).Error; err != nil {
			return persistenceError(err)
		}
		if reminder.Status != "pending" {
			return nil
		}
		var databaseNow time.Time
		if err := tx.Raw("SELECT CURRENT_TIMESTAMP").Scan(&databaseNow).Error; err != nil {
			return persistenceError(err)
		}
		if plan.Status != "active" || plan.DeletedAt != nil || !plan.StartsAt.After(databaseNow) {
			return persistenceError(tx.Model(&reminder).Updates(map[string]any{"reminder_status": "cancelled", "updated_at": databaseNow}).Error)
		}
		if reminder.RemindAt.After(databaseNow) {
			return nil
		}
		payloadBytes, err := json.Marshal(map[string]any{"title": plan.Title, "starts_at": plan.StartsAt})
		if err != nil {
			return fmt.Errorf("生成计划提醒载荷失败: %w", err)
		}
		payload := model.JSONB(payloadBytes)
		eventKey := fmt.Sprintf("plan.reminder.due:%s", reminder.BusinessID)
		notification := model.Notification{
			BaseModel: model.BaseModel{BusinessID: uuid.New(), CreatedAt: databaseNow, UpdatedAt: databaseNow},
			UserID:    plan.UserID, EventType: planReminderEventType, EventKey: eventKey,
			ResourceType: "plan", ResourceBusinessID: &plan.BusinessID, Payload: payload,
		}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "event_key"}}, DoNothing: true}).Create(&notification)
		if result.Error != nil {
			return persistenceError(result.Error)
		}
		if result.RowsAffected > 0 {
			created = true
			var configs []model.NoticeConfig
			if err := tx.Where("user_id = ? AND enabled = TRUE AND deleted_at IS NULL", plan.UserID).Order("created_at ASC").Find(&configs).Error; err != nil {
				return persistenceError(err)
			}
			webhookPayload, err := json.Marshal(map[string]string{
				"title": "计划提醒：" + plan.Title,
				"text":  planReminderWebhookText(plan.Title, plan.StartsAt),
			})
			if err != nil {
				return fmt.Errorf("生成计划提醒 webhook 载荷失败: %w", err)
			}
			for _, config := range configs {
				delivery := model.NoticeDelivery{
					BaseModel: model.BaseModel{BusinessID: uuid.New(), CreatedAt: databaseNow, UpdatedAt: databaseNow},
					UserID:    plan.UserID, DeliveryKind: "notification", NotificationBusinessID: &notification.BusinessID,
					NoticeConfigBusinessID: config.BusinessID, Provider: config.Provider, ConfigName: config.Name,
					EventType: planReminderEventType, Payload: model.JSONB(webhookPayload), PartIndex: 1, PartCount: 1,
					Status: "pending", NextAttemptAt: databaseNow,
				}
				if err := tx.Create(&delivery).Error; err != nil {
					return persistenceError(err)
				}
			}
		}
		if err := tx.Model(&reminder).Updates(map[string]any{"reminder_status": "sent", "sent_at": databaseNow, "updated_at": databaseNow}).Error; err != nil {
			return persistenceError(err)
		}
		return nil
	})
	return created, err
}

// loadPlanReminderOffsets 在主查询结束后批量装载可编辑配置，避免列表逐条查询提醒任务。
func (store *PostgresStore) loadPlanReminderOffsets(ctx context.Context, items []model.Plan) error {
	if len(items) == 0 {
		return nil
	}
	planIDs := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		planIDs = append(planIDs, item.BusinessID)
	}
	var reminders []model.PlanReminder
	if err := store.db.WithContext(ctx).Select("plan_business_id", "lead_minutes").
		Where("plan_business_id IN ? AND is_configured = TRUE", planIDs).Order("lead_minutes ASC").Find(&reminders).Error; err != nil {
		return persistenceError(err)
	}
	byPlan := make(map[uuid.UUID][]int, len(items))
	for _, reminder := range reminders {
		byPlan[reminder.PlanBusinessID] = append(byPlan[reminder.PlanBusinessID], reminder.LeadMinutes)
	}
	for index := range items {
		items[index].ReminderOffsets = byPlan[items[index].BusinessID]
	}
	return nil
}

// loadPlanReminderOffsetsTx 在已有事务内读取一个计划的可编辑提醒设置。
func loadPlanReminderOffsetsTx(tx *gorm.DB, item *model.Plan) error {
	var reminders []model.PlanReminder
	if err := tx.Select("lead_minutes").Where("plan_business_id = ? AND is_configured = TRUE", item.BusinessID).
		Order("lead_minutes ASC").Find(&reminders).Error; err != nil {
		return persistenceError(err)
	}
	item.ReminderOffsets = make([]int, 0, len(reminders))
	for _, reminder := range reminders {
		item.ReminderOffsets = append(item.ReminderOffsets, reminder.LeadMinutes)
	}
	return nil
}

// planReminderWebhookText 转义计划标题中的 Markdown 控制符，外部机器人只接收标题与计划时间。
func planReminderWebhookText(title string, startsAt time.Time) string {
	escaped := strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)", "#", "\\#", ">", "\\>", "@", "\\@").Replace(title)
	chinaTime := startsAt.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	return fmt.Sprintf("计划提醒：%s（%s）", escaped, chinaTime.Format("2006-01-02 15:04"))
}

// makePlanReminderRows 为创建计划生成持久任务；remind_at 早于当前时间时仍保留，worker 会按数据库时钟补发。
func makePlanReminderRows(item *model.Plan, offsets []int, now time.Time) []model.PlanReminder {
	rows := make([]model.PlanReminder, 0, len(offsets))
	for _, offset := range offsets {
		rows = append(rows, model.PlanReminder{
			BaseModel: model.BaseModel{BusinessID: uuid.New(), CreatedAt: now, UpdatedAt: now},
			UserID:    item.UserID, PlanBusinessID: item.BusinessID, LeadMinutes: offset,
			RemindAt: item.StartsAt.Add(-time.Duration(offset) * time.Minute), Status: "pending", Configured: true,
		})
	}
	return rows
}

// reschedulePlanReminders 保留已发送偏移；旧 pending 任务作废后为新配置生成不会重复发送的任务。
func reschedulePlanReminders(tx *gorm.DB, current model.Plan, updates map[string]any, offsets []int, now time.Time) error {
	startAt := current.StartsAt
	if value, ok := updates["starts_at"].(time.Time); ok {
		startAt = value
	}
	if err := tx.Model(&model.PlanReminder{}).Where("user_id = ? AND plan_business_id = ? AND reminder_status = 'pending'", current.UserID, current.BusinessID).
		Updates(map[string]any{"reminder_status": "cancelled", "is_configured": false, "updated_at": now}).Error; err != nil {
		return persistenceError(err)
	}
	if err := tx.Model(&model.PlanReminder{}).Where("user_id = ? AND plan_business_id = ? AND reminder_status = 'sent'", current.UserID, current.BusinessID).
		Update("is_configured", false).Error; err != nil {
		return persistenceError(err)
	}
	for _, offset := range offsets {
		var sent int64
		if err := tx.Model(&model.PlanReminder{}).Where("user_id = ? AND plan_business_id = ? AND lead_minutes = ? AND reminder_status = 'sent'", current.UserID, current.BusinessID, offset).Count(&sent).Error; err != nil {
			return persistenceError(err)
		}
		if sent > 0 {
			if err := tx.Model(&model.PlanReminder{}).Where("user_id = ? AND plan_business_id = ? AND lead_minutes = ? AND reminder_status = 'sent'", current.UserID, current.BusinessID, offset).
				Update("is_configured", true).Error; err != nil {
				return persistenceError(err)
			}
			continue
		}
		row := model.PlanReminder{
			BaseModel: model.BaseModel{BusinessID: uuid.New(), CreatedAt: now, UpdatedAt: now},
			UserID:    current.UserID, PlanBusinessID: current.BusinessID, LeadMinutes: offset,
			RemindAt: startAt.Add(-time.Duration(offset) * time.Minute), Status: "pending", Configured: true,
		}
		if err := tx.Create(&row).Error; err != nil {
			return persistenceError(err)
		}
	}
	return nil
}
