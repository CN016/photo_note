package store_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/store"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestPlanReminderPersistenceIsolationAndConcurrentDelivery 使用预先应用版本化迁移的专用 PostgreSQL 验证提醒持久化、用户隔离及幂等事务。
func TestPlanReminderPersistenceIsolationAndConcurrentDelivery(t *testing.T) {
	dsn := os.Getenv("PHOTO_NINI_PLAN_TEST_DSN")
	if dsn == "" {
		t.Skip("设置 PHOTO_NINI_PLAN_TEST_DSN 并预先执行版本化迁移后运行计划 PostgreSQL 集成测试")
	}
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, Logger: logger.Discard})
	if err != nil {
		t.Fatalf("连接计划集成测试数据库失败: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("取得计划集成测试连接池失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(32)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ownerID, otherOwnerID := uuid.New(), uuid.New()
	ctx := tenant.Personal(context.Background(), ownerID)
	database = database.WithContext(ctx)
	repository := store.NewPostgresStore(database)
	var databaseNow time.Time
	if err := database.Raw("SELECT CURRENT_TIMESTAMP").Scan(&databaseNow).Error; err != nil {
		t.Fatalf("读取数据库时间失败: %v", err)
	}
	t.Cleanup(func() {
		userIDs := []uuid.UUID{ownerID, otherOwnerID}
		_ = database.Where("user_id IN ?", userIDs).Delete(&model.NoticeDelivery{}).Error
		_ = database.Where("user_id IN ?", userIDs).Delete(&model.Notification{}).Error
		_ = database.Where("user_id IN ?", userIDs).Delete(&model.NoticeConfig{}).Error
		_ = database.Where("user_id IN ?", userIDs).Delete(&model.PlanReminder{}).Error
		_ = database.Where("user_id IN ?", userIDs).Delete(&model.Plan{}).Error
	})
	createPlan := func(userID uuid.UUID, startsAt time.Time, offsets []int, title string) model.Plan {
		t.Helper()
		item := model.Plan{
			BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: userID,
			Title: title, Description: "仅用于专用数据库集成测试", StartsAt: startsAt,
			CoverEmoji: "📌", Status: "active",
		}
		if err := repository.CreatePlan(ctx, &item, offsets); err != nil {
			t.Fatalf("创建计划测试数据失败: %v", err)
		}
		return item
	}
	ownerPlan := createPlan(ownerID, databaseNow.Add(30*time.Minute), []int{60}, "并发提醒计划")
	otherPlan := createPlan(otherOwnerID, databaseNow.Add(2*time.Hour), []int{60}, "另一用户计划")
	reschedulePlan := createPlan(ownerID, databaseNow.Add(3*time.Hour), []int{60}, "重排计划")

	for index := 0; index < 2; index++ {
		config := model.NoticeConfig{
			BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: ownerID,
			Provider: []string{"wecom", "dingtalk"}[index], Name: fmt.Sprintf("测试渠道%d", index+1),
			Enabled: true,
		}
		if err := database.Create(&config).Error; err != nil {
			t.Fatalf("创建测试 webhook 配置失败: %v", err)
		}
	}

	ownerPlans, err := repository.ListPlans(ctx, ownerID)
	if err != nil || len(ownerPlans) != 2 {
		t.Fatalf("计划列表未按当前用户隔离: count=%d err=%v", len(ownerPlans), err)
	}
	if _, err := repository.FindPlan(ctx, otherOwnerID, ownerPlan.BusinessID); !fault.Is(err, 404, "not_found") {
		t.Fatalf("其他用户读取计划应返回 not_found，实际错误为 %v", err)
	}

	var dueReminder model.PlanReminder
	if err := database.Where("user_id = ? AND plan_business_id = ?", ownerID, ownerPlan.BusinessID).First(&dueReminder).Error; err != nil {
		t.Fatalf("查询持久化提醒任务失败: %v", err)
	}
	dueIDs, err := repository.DuePlanReminderIDs(ctx, 100)
	if err != nil {
		t.Fatalf("查询到期提醒失败: %v", err)
	}
	if !containsPlanReminder(dueIDs, dueReminder.BusinessID) {
		t.Fatalf("未从数据库时间范围读取到期提醒 %s: %#v", dueReminder.BusinessID, dueIDs)
	}
	expiredPlan := createPlan(ownerID, databaseNow.Add(-time.Minute), []int{60}, "已逾期提醒计划")
	var expiredReminder model.PlanReminder
	if err := database.Where("user_id = ? AND plan_business_id = ?", ownerID, expiredPlan.BusinessID).First(&expiredReminder).Error; err != nil {
		t.Fatalf("查询已逾期计划提醒失败: %v", err)
	}
	if _, err := repository.DuePlanReminderIDs(ctx, 100); err != nil {
		t.Fatalf("清理已逾期计划提醒失败: %v", err)
	}
	if err := database.First(&expiredReminder, "business_id = ?", expiredReminder.BusinessID).Error; err != nil || expiredReminder.Status != "cancelled" {
		t.Fatalf("计划已开始后应取消提醒: status=%s err=%v", expiredReminder.Status, err)
	}

	const workers = 16
	start := make(chan struct{})
	results := make(chan bool, workers)
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			created, publishErr := repository.CreatePlanReminderNotification(ctx, dueReminder.BusinessID)
			results <- created
			errors <- publishErr
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errors)
	createdCount := 0
	for created := range results {
		if created {
			createdCount++
		}
	}
	for publishErr := range errors {
		if publishErr != nil {
			t.Fatalf("并行发送提醒失败: %v", publishErr)
		}
	}
	if createdCount != 1 {
		t.Fatalf("并行 worker 应只创建一条通知，实际创建数=%d", createdCount)
	}

	eventKey := fmt.Sprintf("plan.reminder.due:%s", dueReminder.BusinessID)
	var notification model.Notification
	if err := database.Where("user_id = ? AND event_key = ?", ownerID, eventKey).First(&notification).Error; err != nil {
		t.Fatalf("站内通知没有持久化: %v", err)
	}
	var deliveryCount int64
	if err := database.Model(&model.NoticeDelivery{}).Where("user_id = ? AND notification_business_id = ?", ownerID, notification.BusinessID).Count(&deliveryCount).Error; err != nil {
		t.Fatalf("查询 webhook outbox 失败: %v", err)
	}
	if deliveryCount != 2 {
		t.Fatalf("站内通知应对应两个 webhook outbox，实际为 %d", deliveryCount)
	}

	updated, err := repository.UpdatePlan(ctx, ownerID, reschedulePlan.BusinessID, map[string]any{}, []int{30, 120}, true)
	if err != nil {
		t.Fatalf("只修改提醒配置失败: %v", err)
	}
	if !updated.UpdatedAt.After(reschedulePlan.UpdatedAt) {
		t.Fatalf("只修改提醒时计划更新时间应同步更新: before=%s after=%s", reschedulePlan.UpdatedAt, updated.UpdatedAt)
	}
	newStart := databaseNow.Add(4 * time.Hour)
	updated, err = repository.UpdatePlan(ctx, ownerID, reschedulePlan.BusinessID, map[string]any{"starts_at": newStart}, []int{15, 30, 120}, true)
	if err != nil {
		t.Fatalf("修改计划时间和提醒失败: %v", err)
	}
	if len(updated.ReminderOffsets) != 3 {
		t.Fatalf("计划修改后提醒配置未保存完整: %#v", updated.ReminderOffsets)
	}
	var pendingReminders []model.PlanReminder
	if err := database.Where("user_id = ? AND plan_business_id = ? AND reminder_status = 'pending'", ownerID, reschedulePlan.BusinessID).Find(&pendingReminders).Error; err != nil {
		t.Fatalf("读取重排后的提醒失败: %v", err)
	}
	if len(pendingReminders) != 3 {
		t.Fatalf("计划修改后应有三条 pending 提醒，实际为 %d", len(pendingReminders))
	}
	var oldReminder model.PlanReminder
	if err := database.Where("user_id = ? AND plan_business_id = ? AND lead_minutes = ? AND reminder_status = 'cancelled'", ownerID, reschedulePlan.BusinessID, 60).First(&oldReminder).Error; err != nil {
		t.Fatalf("修改计划后旧提醒未取消: %v", err)
	}

	if _, err := repository.CompletePlan(ctx, otherOwnerID, otherPlan.BusinessID); err != nil {
		t.Fatalf("完成计划失败: %v", err)
	}
	var cancelledCount int64
	if err := database.Model(&model.PlanReminder{}).Where("user_id = ? AND plan_business_id = ? AND reminder_status = 'cancelled'", otherOwnerID, otherPlan.BusinessID).Count(&cancelledCount).Error; err != nil {
		t.Fatalf("查询已取消提醒失败: %v", err)
	}
	if cancelledCount != 1 {
		t.Fatalf("完成计划后应取消未发送提醒，实际取消数=%d", cancelledCount)
	}
}

func containsPlanReminder(ids []uuid.UUID, wanted uuid.UUID) bool {
	for _, id := range ids {
		if id == wanted {
			return true
		}
	}
	return false
}
