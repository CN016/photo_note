package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/service"
	"github.com/photo-nini/backend/internal/store"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type allowScheduleRequests struct{}

func (allowScheduleRequests) CheckScheduleBookingLimit(context.Context, string, int) (bool, error) {
	return true, nil
}

// TestScheduleStoreIsolationCapacityExpirationPrivacyAndRevocation 使用独立迁移数据库验证存储并发和公开数据边界。
func TestScheduleStoreIsolationCapacityExpirationPrivacyAndRevocation(t *testing.T) {
	dsn := os.Getenv("PHOTO_NINI_SCHEDULE_TEST_DSN")
	if dsn == "" {
		t.Skip("设置 PHOTO_NINI_SCHEDULE_TEST_DSN 并预先执行版本化迁移后运行档期 PostgreSQL 集成测试")
	}
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, Logger: logger.Discard})
	if err != nil {
		t.Fatalf("连接档期集成测试数据库失败: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("取得档期集成测试连接池失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.Exec("TRUNCATE schedule_bookings, schedule_exceptions, schedule_items, schedule_shares").Error; err != nil {
		t.Fatalf("清理专用档期测试表失败: %v", err)
	}

	repository := store.NewPostgresStore(database)
	ownerID, otherOwnerID := uuid.New(), uuid.New()
	ctx := tenant.Personal(context.Background(), ownerID)
	database = database.WithContext(ctx)
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	day := time.Now().In(location).AddDate(0, 0, 7)
	start := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, location)
	openItem := model.ScheduleItem{
		BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: ownerID, Kind: "availability", Title: "客户预约",
		Content: "只公开开放时段", StartAt: start, EndAt: start.Add(time.Hour), Recurrence: "none", RecurrenceWeekdays: pq.Int64Array{}, Capacity: 1,
	}
	otherItem := model.ScheduleItem{
		BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: otherOwnerID, Kind: "work", Title: "另一账户的私密工作",
		Content: "绝不能跨账户读取", StartAt: start, EndAt: start.Add(2 * time.Hour), Recurrence: "none", RecurrenceWeekdays: pq.Int64Array{}, Capacity: 1,
	}
	createItem := func(item *model.ScheduleItem) {
		t.Helper()
		if err := repository.CreateScheduleItem(ctx, item, func([]model.ScheduleItem, []model.ScheduleException) error { return nil }); err != nil {
			t.Fatalf("创建档期测试数据失败: %v", err)
		}
	}
	createItem(&openItem)
	createItem(&otherItem)
	privateWork := model.ScheduleItem{
		BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: ownerID, Kind: "work", Title: "拥有者私密工作",
		Content: "私密事项内容", StartAt: start, EndAt: start.Add(30 * time.Minute), Recurrence: "none", RecurrenceWeekdays: pq.Int64Array{}, Capacity: 1,
	}
	if err := database.Create(&privateWork).Error; err != nil {
		t.Fatalf("创建私密工作事项测试数据失败: %v", err)
	}

	ownerItems, _, err := repository.ListScheduleData(ctx, ownerID)
	if err != nil || len(ownerItems) != 2 {
		t.Fatalf("档期查询没有隔离到当前用户: items=%d err=%v", len(ownerItems), err)
	}
	otherItems, _, err := repository.ListScheduleData(ctx, otherOwnerID)
	if err != nil || len(otherItems) != 1 || otherItems[0].BusinessID != otherItem.BusinessID {
		t.Fatalf("第二个用户读取到了错误档期: items=%d err=%v", len(otherItems), err)
	}

	calendar := service.NewScheduleService(repository, allowScheduleRequests{}, 10)
	share, err := calendar.CreateShare(ctx, ownerID, "https://booking.example")
	if err != nil {
		t.Fatalf("创建预约分享链接失败: %v", err)
	}
	token := strings.TrimPrefix(share.URL, "https://booking.example/book/")
	var persistedShare model.ScheduleShare
	if err := database.Where("user_id = ?", ownerID).First(&persistedShare).Error; err != nil || persistedShare.TokenHash == token || !persistedShare.Enabled {
		t.Fatalf("分享配置未安全保存令牌摘要: enabled=%v err=%v", persistedShare.Enabled, err)
	}
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, location)
	slots, err := calendar.PublicSlots(context.Background(), token, from, from.AddDate(0, 0, 1))
	if err != nil || len(slots) != 1 || slots[0].BusinessID != openItem.BusinessID || slots[0].Remaining != 1 {
		t.Fatalf("公开查询没有只返回开放时段: slots=%d err=%v", len(slots), err)
	}
	publicJSON, err := json.Marshal(slots)
	if err != nil {
		t.Fatalf("序列化公开时段失败: %v", err)
	}
	if strings.Contains(string(publicJSON), "拥有者私密工作") || strings.Contains(string(publicJSON), "私密事项内容") || strings.Contains(string(publicJSON), "user_id") || strings.Contains(string(publicJSON), `"id"`) {
		t.Fatalf("公开时段泄露了私密工作或内部标识: %s", publicJSON)
	}

	// 多个访客同时请求一个容量为一的时段，数据库行锁应只允许其中一个占位成功。
	const applicants = 12
	startRequests := make(chan struct{})
	results := make(chan error, applicants)
	var workers sync.WaitGroup
	for index := 0; index < applicants; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-startRequests
			_, requestErr := calendar.Request(context.Background(), "192.0.2.10", token, openItem.BusinessID, start,
				fmt.Sprintf("客户%d", index), "13800138000", "")
			results <- requestErr
		}(index)
	}
	close(startRequests)
	workers.Wait()
	close(results)
	successes := 0
	for requestErr := range results {
		if requestErr == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("并发申请超出时段容量: 成功数=%d", successes)
	}

	bookings, err := repository.ListScheduleBookings(ctx, ownerID, "all")
	if err != nil || len(bookings) != 1 || bookings[0].Status != "pending" || bookings[0].ExpiresAt == nil {
		t.Fatalf("预约申请没有按拥有者隔离并占位: bookings=%d err=%v", len(bookings), err)
	}
	past := time.Now().UTC().Add(-time.Minute)
	if err := database.Model(&model.ScheduleBooking{}).Where("business_id = ?", bookings[0].BusinessID).Update("expires_at", past).Error; err != nil {
		t.Fatalf("设置过期预约测试数据失败: %v", err)
	}
	bookings, err = repository.ListScheduleBookings(ctx, ownerID, "all")
	if err != nil || len(bookings) != 1 || bookings[0].Status != "expired" || bookings[0].ExpiresAt != nil {
		t.Fatalf("超过 24 小时的待确认申请未释放名额: bookings=%#v err=%v", bookings, err)
	}
	if _, err := calendar.Request(context.Background(), "192.0.2.10", token, openItem.BusinessID, start, "第二位客户", "13800138001", ""); err != nil {
		t.Fatalf("过期申请释放后仍无法预约: %v", err)
	}

	if err := repository.RevokeScheduleShare(ctx, ownerID); err != nil {
		t.Fatalf("撤销预约分享链接失败: %v", err)
	}
	if _, err := calendar.PublicSlots(context.Background(), token, from, from.AddDate(0, 0, 1)); err == nil {
		t.Fatal("撤销后的预约链接仍可读取开放时段")
	}
}
