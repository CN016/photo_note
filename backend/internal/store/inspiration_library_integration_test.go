package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestInspirationLibraryPostgres 在独立完整迁移库验证目标快照、历史计数、通知幂等和任务租约。
func TestInspirationLibraryPostgres(t *testing.T) {
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if dsn == "" {
		t.Skip("需要独立 PostgreSQL 测试库")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	conn, _ := db.DB()
	defer conn.Close()
	// 使用独立schema避免运行中的API worker领取测试用例的任务。
	conn.SetMaxOpenConns(1)
	schema := "library_service_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if e := db.Exec(`CREATE SCHEMA "` + schema + `"`).Error; e != nil {
		t.Fatal(e)
	}
	defer db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
	if e := db.Exec(`SET search_path TO "` + schema + `",public`).Error; e != nil {
		t.Fatal(e)
	}
	files, e := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if e != nil {
		t.Fatal(e)
	}
	for _, file := range files {
		body, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		if e = db.Exec(string(body)).Error; e != nil {
			t.Fatal(e)
		}
	}
	r := NewPostgresStore(db)
	r.SetInspirationProcessingAlgorithm("library-test-v1")
	uid := uuid.New()
	ctx := tenant.Personal(context.Background(), uid)
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	must(r.SaveInspirationGoal(ctx, uid, true, 3, "00:00"))
	item := model.Inspiration{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: uid, ImageKey: "unsupported:test", Data: model.JSONB(`{}`)}
	must(r.CreateInspiration(ctx, &item))
	day := time.Now().In(libraryChinaTime).Format("2006-01-02")
	rows, err := r.ListInspirationActivities(ctx, uid, day, day)
	must(err)
	if len(rows) != 1 || rows[0].SavedCount != 1 || rows[0].Target == nil || *rows[0].Target != 3 {
		t.Fatalf("计数或目标错误: %+v", rows)
	}
	must(r.SaveInspirationGoal(ctx, uid, true, 4, "00:00"))
	goal, err := r.GetInspirationGoal(ctx, uid)
	must(err)
	must(r.PublishInspirationGoalNotification(ctx, goal))
	must(r.PublishInspirationGoalNotification(ctx, goal))
	var count int64
	must(db.WithContext(ctx).Model(&model.Notification{}).Where("user_id=? AND event_type=?", uid, "inspiration.goal.unmet").Count(&count).Error)
	if count != 1 {
		t.Fatalf("通知重复: %d", count)
	}
	claimed, err := r.AcknowledgeInspirationReminder(ctx, uid, day)
	must(err)
	if !claimed {
		t.Fatal("首次提醒未确认")
	}
	claimed, err = r.AcknowledgeInspirationReminder(ctx, uid, day)
	must(err)
	if claimed {
		t.Fatal("重复确认")
	}
	other := tenant.Personal(context.Background(), uuid.New())
	foreign, err := r.ListInspirationActivities(other, uid, day, day)
	if err == nil && len(foreign) != 0 {
		t.Fatal("活动跨用户泄露")
	}
	if _, err = r.ListInspirationActivities(context.Background(), uid, day, day); err == nil {
		t.Fatal("缺失上下文未拒绝")
	}
	summary, err := r.InspirationBrowseSummary(ctx, uid, day, day)
	must(err)
	if len(summary["days"]) == 0 {
		t.Logf("分组键: %+v", summary)
	}
	// 没有存图或停机的日期保留此前目标，今天调整不能覆盖昨天。
	pastTarget := 7
	twoDaysAgo := time.Now().In(libraryChinaTime).AddDate(0, 0, -2).Format("2006-01-02")
	historical := model.InspirationActivity{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: uid, Day: twoDaysAgo, Target: &pastTarget}
	must(db.WithContext(ctx).Create(&historical).Error)
	history, e := r.ListInspirationActivities(ctx, uid, twoDaysAgo, day)
	must(e)
	if len(history) != 3 || history[1].Target == nil || *history[1].Target != 7 || history[1].SavedCount != 0 || *history[2].Target != 4 {
		t.Fatal("零存图日期丢失历史目标")
	}
	// 两个组织及同组织不同用户的目标和每日活动各自隔离。
	owner := model.User{BaseModel: model.BaseModel{BusinessID: uuid.New()}, Nickname: "图库隔离测试", Phone: fmt.Sprintf("19%09d", time.Now().UnixNano()%1000000000), PasswordHash: "test-only"}
	must(r.CreateUser(tenant.System(context.Background()), &owner))
	for _, name := range []string{"图库甲", "图库乙"} {
		org := model.Organization{Name: name, Status: "active"}
		must(r.CreateOrganization(tenant.System(context.Background()), &org, owner.BusinessID))
		scoped := tenant.WithScope(context.Background(), tenant.Scope{OrgID: org.OrgID, UserID: owner.BusinessID})
		must(r.SaveInspirationGoal(scoped, owner.BusinessID, true, 7, "20:00"))
		records, e := r.ListInspirationActivities(scoped, owner.BusinessID, day, day)
		must(e)
		if len(records) != 1 || records[0].SavedCount != 0 || *records[0].Target != 7 {
			t.Fatal("组织活动串空间")
		}
		another := tenant.WithScope(context.Background(), tenant.Scope{OrgID: org.OrgID, UserID: uuid.New()})
		if _, e = r.GetInspirationGoal(another, owner.BusinessID); e == nil {
			t.Fatal("同组织跨用户未拒绝")
		}
	}
	// 使用精确任务领取而非系统全库领取，隔离其他集成测试的待处理任务。
	var job model.InspirationProcessingJob
	must(db.WithContext(ctx).Where("inspiration_id=?", item.BusinessID).First(&job).Error)
	lease := uuid.New()
	until := time.Now().Add(time.Minute)
	job.Stage = "analyzing"
	job.LeaseToken = &lease
	job.LeaseUntil = &until
	must(db.WithContext(ctx).Model(&job).Updates(map[string]any{"stage": job.Stage, "lease_token": lease, "lease_until": until}).Error)
	stale := job
	wrong := uuid.New()
	stale.LeaseToken = &wrong
	must(r.FinishInspirationProcessing(ctx, stale, "failed", "invalid", time.Now(), nil))
	var retained model.InspirationProcessingJob
	must(db.WithContext(ctx).Where("business_id=?", job.BusinessID).First(&retained).Error)
	if retained.Stage != "analyzing" {
		t.Fatal("旧租约覆盖当前任务")
	}
	must(r.FinishInspirationProcessing(ctx, job, "failed", "invalid", time.Now(), nil))
	image, err := r.FindInspirationByBusinessID(ctx, uid, item.BusinessID)
	must(err)
	if image.PeopleCount != nil || image.IsEmptyShot != nil {
		t.Fatal("失败被错误解释为空镜")
	}
	must(r.RetryInspirationProcessing(ctx, uid, item.BusinessID))
	// 替换图片取消旧代次；旧 worker 的成功结果不能覆盖新图。
	previous := job
	_, err = r.UpdateInspirationWithAsyncVersion(ctx, uid, item.BusinessID, map[string]any{"image_key": "unsupported:replacement", "analysis_status": "pending_compression"})
	must(err)
	must(r.FinishInspirationProcessing(ctx, previous, "succeeded", "", time.Now(), nil))
	var replacement model.InspirationProcessingJob
	must(db.WithContext(ctx).Where("inspiration_id=? AND async_version=?", item.BusinessID, item.AsyncVersion+1).First(&replacement).Error)
	expired := time.Now().Add(-time.Minute)
	oldToken := uuid.New()
	must(db.WithContext(ctx).Model(&replacement).Updates(map[string]any{"stage": "analyzing", "lease_until": expired, "lease_token": oldToken, "next_attempt_at": time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}).Error)
	recovered, e := r.ClaimInspirationProcessing(tenant.System(context.Background()), "analysis", time.Minute)
	must(e)
	if recovered == nil || recovered.BusinessID != replacement.BusinessID || recovered.LeaseToken == nil || *recovered.LeaseToken == oldToken {
		t.Fatal("超时租约未恢复")
	}
	zero := 0
	empty := true
	color := "#ffffff"
	hue := 0.0
	bucket := "white"
	palette := model.JSONB(`{"version":1,"palette":[{"hex":"#ffffff","percentage":100}],"other_percentage":0}`)
	must(r.FinishInspirationProcessing(ctx, *recovered, "succeeded", "", time.Now(), &model.InspirationAnalysis{PeopleCount: &zero, IsEmptyShot: &empty, DominantColor: &color, DominantHue: &hue, ColorBucket: &bucket, ColorPalette: &palette}))
	filtered := model.WithInspirationFilters(ctx, model.InspirationFilters{People: "0", EmptyShot: "true", Color: "white", DateFrom: day, DateTo: day, Project: "unassigned"})
	matches, total, e := r.ListInspirations(filtered, uid, nil, "any", "", "created_desc", 0, 1)
	must(e)
	if total != 1 || len(matches) != 1 || matches[0].PeopleCount == nil {
		t.Fatal("画像分页筛选失败")
	}
	must(r.DeleteInspiration(ctx, uid, item.BusinessID))
	rows, err = r.ListInspirationActivities(ctx, uid, day, day)
	must(err)
	if rows[0].SavedCount != 1 || *rows[0].Target != 4 {
		t.Fatal("删除回减历史计数或修改目标丢失快照")
	}
}

// TestLibraryIncrementalMigration 验证旧数据按北京时间回填，历史目标为空，完整字段注释及无外键。
func TestLibraryIncrementalMigration(t *testing.T) {
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if dsn == "" {
		t.Skip("需要独立迁移测试库")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	conn, _ := db.DB()
	conn.SetMaxOpenConns(1)
	schema := "library_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	must(db.Exec(`CREATE SCHEMA "` + schema + `"`).Error)
	defer func() { db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`); conn.Close() }()
	must(db.Exec(`SET search_path TO "` + schema + `",public`).Error)
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	must(err)
	for _, file := range files {
		if filepath.Base(file) >= "000023_" {
			continue
		}
		body, e := os.ReadFile(file)
		must(e)
		must(db.Exec(string(body)).Error)
	}
	uid := uuid.New()
	for _, stamp := range []string{"2026-10-25T15:59:59Z", "2026-10-25T16:00:00Z"} {
		must(db.Exec(`INSERT INTO inspirations(business_id,orgid,user_id,image_key,data,created_at) VALUES(?,-1,?,'unsupported:history','{}',?::timestamptz)`, uuid.New(), uid, stamp).Error)
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000023_inspiration_library.sql"))
	must(err)
	must(db.Exec(string(body)).Error)
	var days []struct {
		Day        string
		SavedCount int
		Target     *int
	}
	must(db.Raw(`SELECT day::text,saved_count,target FROM inspiration_activities WHERE user_id=? ORDER BY day`, uid).Scan(&days).Error)
	if len(days) != 2 || days[0].Day != "2026-10-25" || days[1].Day != "2026-10-26" || days[0].SavedCount != 1 || days[0].Target != nil {
		t.Fatalf("回填跨日或历史目标错误: %+v", days)
	}
	// 独立 schema 没有投递 worker；验证两条启用渠道各有一个 outbox，绝不发送外网请求。
	repository := NewPostgresStore(db)
	private := tenant.Personal(context.Background(), uid)
	must(repository.SaveInspirationGoal(private, uid, true, 10, "00:00"))
	for _, name := range []string{"测试渠道甲", "测试渠道乙"} {
		channel := model.NoticeConfig{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: uid, Provider: "wecom", Name: name, WebhookURL: "https://example.invalid/never-sent", Enabled: true}
		must(db.WithContext(private).Create(&channel).Error)
	}
	goal, e := repository.GetInspirationGoal(private, uid)
	must(e)
	must(repository.PublishInspirationGoalNotification(private, goal))
	must(repository.PublishInspirationGoalNotification(private, goal))
	var deliveries int64
	must(db.WithContext(private).Model(&model.NoticeDelivery{}).Where("user_id=?", uid).Count(&deliveries).Error)
	if deliveries != 2 {
		t.Fatalf("outbox 幂等或渠道数量错误: %d", deliveries)
	}
	var count int64
	must(db.Raw(`SELECT count(*) FROM information_schema.table_constraints WHERE table_schema=? AND constraint_type='FOREIGN KEY'`, schema).Scan(&count).Error)
	if count != 0 {
		t.Fatal("出现外键")
	}
	must(db.Raw(`SELECT count(*) FROM information_schema.columns c WHERE c.table_schema=? AND c.table_name IN ('inspiration_goals','inspiration_activities','inspiration_processing_jobs') AND NOT EXISTS(SELECT 1 FROM pg_class cl JOIN pg_namespace n ON n.oid=cl.relnamespace JOIN pg_attribute a ON a.attrelid=cl.oid JOIN pg_description d ON d.objoid=cl.oid AND d.objsubid=a.attnum WHERE n.nspname=c.table_schema AND cl.relname=c.table_name AND a.attname=c.column_name AND d.description<>'')`, schema).Scan(&count).Error)
	if count != 0 {
		t.Fatal("缺少中文字段注释")
	}
}
