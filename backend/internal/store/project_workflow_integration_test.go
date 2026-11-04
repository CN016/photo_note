package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/projectstatus"
	"github.com/photo-nini/backend/internal/service"
	"github.com/photo-nini/backend/internal/store"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// workflowTestDB 只在显式命名为 *_test 的独立测试库中新建随机 schema；不清理任何既有业务表。
// 每条连接均绑定同一 schema，允许真实并发事务测试，而非仅靠单连接排队模拟。
func workflowTestDB(t *testing.T, beforeWorkflow ...func(*gorm.DB)) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if dsn == "" {
		t.Skip("需要独立 PostgreSQL 测试库 PHOTO_NINI_TENANT_TEST_DSN")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(config.Database, "_test") {
		t.Fatal("策划集成测试要求独立且名称以 _test 结尾的数据库")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	schema := "project_workflow_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["search_path"] = schema + ",public"
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	registered := stdlib.RegisterConnConfig(config)
	connection, err := sql.Open("pgx", registered)
	if err != nil {
		t.Fatal(err)
	}
	connection.SetMaxOpenConns(8)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: connection}), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, TranslateError: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		connection.Close()
		stdlib.UnregisterConnConfig(registered)
		admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
		adminSQL, _ := admin.DB()
		adminSQL.Close()
	})
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if filepath.Base(file) == "000028_project_workflow.sql" {
			for _, prepare := range beforeWorkflow {
				prepare(db)
			}
		}
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.Exec(string(body)).Error; err != nil {
			t.Fatalf("迁移 %s 失败: %v", filepath.Base(file), err)
		}
	}
	return db
}

// TestProjectStatusMigrationPostgres 核验历史状态回填与 Go 判定一致，包括 Unicode 空白和非任务删除线。
func TestProjectStatusMigrationPostgres(t *testing.T) {
	user := uuid.New()
	samples := []struct {
		id                      uuid.UUID
		document, summary, want string
	}{
		{uuid.New(), `{"type":"doc","content":[{"type":"paragraph"}]}`, "已写总结", "unfinished"},
		{uuid.New(), `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"删除文字","marks":[{"type":"strike"}]}]}]}`, "", "unfinished"},
		{uuid.New(), `{"type":"doc","content":[{"type":"taskItem","attrs":{"checked":true}}]}`, "\u00a0\u3000", "executed"},
		{uuid.New(), `{"type":"doc","content":[{"type":"taskItem","attrs":{"checked":"true"}}]}`, "", "unfinished"},
		{uuid.New(), `{"type":"doc","content":[{"type":"taskItem","content":[{"type":"text","text":"\u00a0","marks":[{"type":"strike"}]}]}]}`, "", "unfinished"},
		{uuid.New(), `{"type":"doc","content":[{"type":"taskItem","content":[{"type":"text","text":"动作","marks":[{"type":"strike"}]}]}]}`, "复盘", "completed"},
	}
	db := workflowTestDB(t, func(db *gorm.DB) {
		config := model.AppConfig{BaseModel: model.BaseModel{BusinessID: uuid.New(), OrgID: -1}, ConfigKey: "projects.ai.api_key", ConfigValue: "test-placeholder", IsPublic: true}
		if err := db.Create(&config).Error; err != nil {
			t.Fatal(err)
		}
		for _, sample := range samples {
			if err := db.Exec("INSERT INTO projects (business_id,orgid,user_id,title,cover_emoji,document,manual_summary) VALUES (?,-1,?,'历史策划','cover',?::jsonb,?)", sample.id, user, sample.document, sample.summary).Error; err != nil {
				t.Fatal(err)
			}
		}
	})
	var config model.AppConfig
	if err := db.Where("config_key = ?", "projects.ai.api_key").First(&config).Error; err != nil || config.IsPublic || !config.IsSecret || config.ConfigValue != "test-placeholder" {
		t.Fatal("迁移未保护已有 AI 密钥配置", err)
	}
	for _, sample := range samples {
		var item model.Project
		if err := db.Where("business_id = ? AND user_id = ?", sample.id, user).First(&item).Error; err != nil {
			t.Fatal(err)
		}
		if item.Status != sample.want || item.Status != projectstatus.Calculate([]byte(sample.document), sample.summary) {
			t.Fatalf("迁移状态不符: got=%s want=%s", item.Status, sample.want)
		}
	}
}

// TestProjectWorkflowPostgres 在真实 PostgreSQL 覆盖状态、兼容保存、幂等副本、并发档期及跨空间关联。
func TestProjectWorkflowPostgres(t *testing.T) {
	db := workflowTestDB(t)
	repo := store.NewPostgresStore(db)
	projects := service.NewProjectService(repo, nil, 1024)
	workflow := service.NewProjectWorkflowService(repo, nil)
	user, other := uuid.New(), uuid.New()
	personal := tenant.Personal(context.Background(), user)
	orgA := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 701, UserID: user})
	orgB := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 702, UserID: user})
	member := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 701, UserID: other})
	page := service.PageRequest{Page: 1, PageSize: 20, Offset: 0}
	empty := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`)
	marked := json.RawMessage(`{"type":"doc","content":[{"type":"taskList","content":[{"type":"taskItem","attrs":{"checked":true},"content":[{"type":"paragraph","content":[{"type":"text","text":"检查光线"}]}]}]}]}`)
	summary := "保留侧光，下次提前踩点"
	analysis := "人工编辑的分析"
	start := time.Date(2026, 11, 5, 2, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	shoot := model.ProjectShooting{StartAt: &start, EndAt: &end, Location: "城南公园", Person: "九九", Kind: "photo"}
	create := func(ctx context.Context, owner uuid.UUID, title string, doc json.RawMessage, review *string, shooting *model.ProjectShooting) service.ProjectView {
		t.Helper()
		item, err := projects.Create(ctx, owner, service.ProjectSave{Title: title, CoverEmoji: "cover", DocumentSchemaVersion: 1, Document: doc, ManualSummary: review, Shooting: shooting, AIAnalysis: &analysis})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	for _, scope := range []struct {
		ctx  context.Context
		user uuid.UUID
	}{{personal, user}, {orgA, user}, {orgB, user}, {member, other}} {
		item := create(scope.ctx, scope.user, "隔离策划", marked, &summary, &shoot)
		if item.Status != "completed" {
			t.Fatalf("创建状态错误: %s", item.Status)
		}
		listed, err := projects.ListFiltered(scope.ctx, scope.user, "", "completed", page)
		if err != nil || listed.Total != 1 || len(listed.Items) != 1 || listed.StatusCounts.Completed != 1 {
			t.Fatalf("状态分页/隔离错误: %#v %v", listed, err)
		}
		if _, err = projects.Get(context.Background(), scope.user, item.BusinessID); err == nil {
			t.Fatal("缺失范围未拒绝")
		}
		if _, err = projects.Get(tenant.System(scope.ctx), scope.user, item.BusinessID); err == nil {
			t.Fatal("系统权限进入普通策划")
		}
	}
	source := create(orgA, user, "拍摄复盘", marked, &summary, &shoot)
	// 旧客户端省略新字段仍应保留拍摄信息、人工总结与 AI 分析。
	updated, err := projects.Update(orgA, user, source.BusinessID, service.ProjectSave{Title: source.Title, CoverEmoji: "cover", DocumentSchemaVersion: 1, Document: empty, Revision: source.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ManualSummary != summary || updated.Shooting.Location != shoot.Location || updated.AIAnalysis != analysis || updated.Status != "unfinished" {
		t.Fatalf("旧客户端覆盖新字段: %#v", updated)
	}
	if _, err = projects.Update(orgA, user, source.BusinessID, service.ProjectSave{Title: source.Title, CoverEmoji: "cover", DocumentSchemaVersion: 1, Document: marked, Revision: source.Revision}); !fault.Is(err, 409, "conflict") {
		t.Fatal("迟到修订未拒绝")
	}
	blank := " \n\t"
	updated, err = projects.Update(orgA, user, source.BusinessID, service.ProjectSave{Title: source.Title, CoverEmoji: "cover", DocumentSchemaVersion: 1, Document: marked, ManualSummary: &blank, Revision: updated.Revision})
	if err != nil || updated.Status != "executed" {
		t.Fatalf("空白总结状态错误: %v", err)
	}
	updated, err = projects.Update(orgA, user, source.BusinessID, service.ProjectSave{Title: source.Title, CoverEmoji: "cover", DocumentSchemaVersion: 1, Document: marked, ManualSummary: &summary, Revision: updated.Revision})
	if err != nil || updated.Status != "completed" {
		t.Fatal("完成状态错误", err)
	}
	// 多页分类的总数必须是完整结果，不得读取正文到列表。
	for i := 0; i < 22; i++ {
		create(orgA, user, "未完成分页", empty, nil, nil)
	}
	listed, err := projects.ListFiltered(orgA, user, "未完成", "unfinished", page)
	if err != nil || listed.Total != 22 || listed.StatusCounts.Unfinished != 22 || len(listed.Items) != 20 {
		t.Fatalf("完整计数失败: %#v %v", listed, err)
	}
	second, err := projects.ListFiltered(orgA, user, "未完成", "unfinished", service.PageRequest{Page: 2, PageSize: 20, Offset: 20})
	if err != nil || len(second.Items) != 2 {
		t.Fatal("第二页错误", err)
	}
	// 并发完成同一总结返回相同文档，后续独立编辑不被源总结覆盖。
	var wg sync.WaitGroup
	ids := make(chan uuid.UUID, 8)
	errors := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			view, e := workflow.CompleteReview(orgA, user, source.BusinessID, updated.Revision)
			if e != nil {
				errors <- e
			} else {
				ids <- view.BusinessID
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(errors)
	for e := range errors {
		t.Fatal(e)
	}
	var reviewID uuid.UUID
	for id := range ids {
		if reviewID != uuid.Nil && reviewID != id {
			t.Fatal("生成了多个总结副本")
		}
		reviewID = id
	}
	review, err := workflow.GetReview(orgA, user, reviewID)
	if err != nil || !strings.Contains(review.Title, "城南公园") || review.Content != summary {
		t.Fatal("总结复制错误", err)
	}
	review, err = workflow.SaveReview(orgA, user, reviewID, service.ProjectReviewInput{Title: review.Title, Content: "独立编辑", Revision: review.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = workflow.SaveReview(orgA, user, reviewID, service.ProjectReviewInput{Title: review.Title, Content: "旧版本", Revision: 1}); !fault.Is(err, 409, "conflict") {
		t.Fatal("总结旧版本未拒绝")
	}
	if _, err = workflow.GetReview(orgB, user, reviewID); !fault.Is(err, 404, "not_found") {
		t.Fatal("总结跨组织可见")
	}
	if _, err = workflow.GetReview(member, other, reviewID); !fault.Is(err, 404, "not_found") {
		t.Fatal("总结跨用户可见")
	}
	// 自动新建的档期并发重试仅创建一条；原有同时间档期必须由用户确认。
	linkIDs := make(chan uuid.UUID, 4)
	scheduleErrors := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			view, e := workflow.SyncSchedule(orgA, user, source.BusinessID, service.ProjectScheduleInput{Revision: updated.Revision})
			if e != nil {
				scheduleErrors <- e
			} else if view.ScheduleID != nil {
				linkIDs <- *view.ScheduleID
			}
		}()
	}
	wg.Wait()
	close(linkIDs)
	close(scheduleErrors)
	for e := range scheduleErrors {
		t.Fatal(e)
	}
	var scheduleID uuid.UUID
	for id := range linkIDs {
		if scheduleID != uuid.Nil && scheduleID != id {
			t.Fatal("重复创建档期")
		}
		scheduleID = id
	}
	if scheduleID == uuid.Nil {
		t.Fatal("未创建档期")
	}
	otherPlan := create(orgA, user, "关联现有档期", empty, nil, &shoot)
	candidates, err := workflow.ScheduleCandidates(orgA, user, otherPlan.BusinessID)
	if err != nil || len(candidates) != 1 {
		t.Fatal("候选错误", err)
	}
	if _, err = workflow.SyncSchedule(orgA, user, otherPlan.BusinessID, service.ProjectScheduleInput{Revision: 1}); !fault.Is(err, 409, "project_schedule_candidates") {
		t.Fatal("候选未确认却创建档期")
	}
	existingLink, err := workflow.SyncSchedule(orgA, user, otherPlan.BusinessID, service.ProjectScheduleInput{Revision: 1, ScheduleID: &scheduleID})
	if err != nil || existingLink.Owned {
		t.Fatal("关联现有档期错误", err)
	}
	newStart := start.Add(2 * time.Hour)
	newEnd := newStart.Add(time.Hour)
	changedShoot := shoot
	changedShoot.StartAt, changedShoot.EndAt = &newStart, &newEnd
	otherPlan, err = projects.Update(orgA, user, otherPlan.BusinessID, service.ProjectSave{Title: otherPlan.Title, CoverEmoji: "cover", DocumentSchemaVersion: 1, Document: empty, Revision: 1, Shooting: &changedShoot})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = workflow.SyncSchedule(orgA, user, otherPlan.BusinessID, service.ProjectScheduleInput{Revision: otherPlan.Revision}); err != nil {
		t.Fatal(err)
	}
	var event model.ScheduleItem
	if err = db.WithContext(orgA).Where("business_id = ?", scheduleID).First(&event).Error; err != nil || !event.StartAt.Equal(start) {
		t.Fatal("用户关联的已有档期被自动改写", err)
	}
	// 本策划拥有的档期可同步，日历删除后清理关联；主动重试仍可重新创建。
	ownedStart, ownedEnd := start.Add(4*time.Hour), start.Add(5*time.Hour)
	ownedShoot := shoot
	ownedShoot.StartAt, ownedShoot.EndAt = &ownedStart, &ownedEnd
	oldRevision := updated.Revision
	updated, err = projects.Update(orgA, user, source.BusinessID, service.ProjectSave{Title: source.Title, CoverEmoji: "cover", DocumentSchemaVersion: 1, Document: marked, Revision: updated.Revision, Shooting: &ownedShoot})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = workflow.SyncSchedule(orgA, user, source.BusinessID, service.ProjectScheduleInput{Revision: oldRevision}); !fault.Is(err, 409, "conflict") {
		t.Fatal("旧修订可同步档期")
	}
	if _, err = workflow.SyncSchedule(orgA, user, source.BusinessID, service.ProjectScheduleInput{Revision: updated.Revision}); err != nil {
		t.Fatal(err)
	}
	if err = db.WithContext(orgA).Where("business_id = ?", scheduleID).First(&event).Error; err != nil || !event.StartAt.Equal(ownedStart) {
		t.Fatal("拥有的档期未同步", err)
	}
	if err = repo.DeleteScheduleItem(orgA, user, scheduleID); err != nil {
		t.Fatal(err)
	}
	cleared, err := projects.Get(orgA, user, source.BusinessID)
	if err != nil || cleared.ScheduleID != nil || cleared.ScheduleSyncNeeded {
		t.Fatal("删除日历后关联未清理或会立即重建", err)
	}
	if _, err = workflow.SyncSchedule(orgA, user, source.BusinessID, service.ProjectScheduleInput{Revision: updated.Revision}); err != nil {
		t.Fatal("手动重试失败", err)
	}
	// 开放预约冲突只影响档期，不能破坏已保存策划；移除冲突后可成功重试。
	blockedStart, blockedEnd := start.Add(10*time.Hour), start.Add(11*time.Hour)
	blockedShoot := shoot
	blockedShoot.StartAt, blockedShoot.EndAt = &blockedStart, &blockedEnd
	schedules := service.NewScheduleService(repo, nil, 1)
	availability, err := schedules.Create(orgA, user, service.ScheduleFields{Kind: "availability", Title: "开放时段", StartAt: blockedStart, EndAt: blockedEnd, Recurrence: "none", Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	blockedPlan := create(orgA, user, "冲突重试", empty, nil, &blockedShoot)
	if _, err = workflow.SyncSchedule(orgA, user, blockedPlan.BusinessID, service.ProjectScheduleInput{Revision: 1}); !fault.Is(err, 409, "conflict") {
		t.Fatal("开放时段冲突未拒绝", err)
	}
	preserved, err := projects.Get(orgA, user, blockedPlan.BusinessID)
	if err != nil || preserved.Shooting.Location != shoot.Location || preserved.ScheduleID != nil {
		t.Fatal("档期失败破坏策划", err)
	}
	if err = repo.DeleteScheduleItem(orgA, user, availability.BusinessID); err != nil {
		t.Fatal(err)
	}
	if _, err = workflow.SyncSchedule(orgA, user, blockedPlan.BusinessID, service.ProjectScheduleInput{Revision: 1}); err != nil {
		t.Fatal("解除冲突后不能重试", err)
	}
	// 作品集关联双端校验，同组织其他用户及跨组织均不得关联。
	portfolio := model.Portfolio{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, Title: "选片作品", TagIDs: []string{}}
	if err = repo.CreatePortfolio(orgA, &portfolio); err != nil {
		t.Fatal(err)
	}
	if err = workflow.SetPortfolios(orgA, user, source.BusinessID, []uuid.UUID{portfolio.BusinessID}); err != nil {
		t.Fatal(err)
	}
	if links, e := workflow.Portfolios(orgA, user, source.BusinessID); e != nil || len(links) != 1 {
		t.Fatal("正向关联失败", e)
	}
	if links, e := repo.ListPortfolioProjects(orgA, user, portfolio.BusinessID); e != nil || len(links) != 1 || links[0].ProjectID != source.BusinessID {
		t.Fatal("反向关联失败", e)
	}
	foreign := create(orgB, user, "跨组织关联", empty, nil, nil)
	if err = workflow.SetPortfolios(orgB, user, foreign.BusinessID, []uuid.UUID{portfolio.BusinessID}); err == nil {
		t.Fatal("允许跨组织作品集关联")
	}
	memberPlan := create(member, other, "成员自己的策划", empty, nil, nil)
	if err = workflow.SetPortfolios(member, other, memberPlan.BusinessID, []uuid.UUID{portfolio.BusinessID}); err == nil {
		t.Fatal("允许同组织不同用户关联作品集")
	}
	if err = workflow.SetPortfolios(context.Background(), user, source.BusinessID, []uuid.UUID{portfolio.BusinessID}); err == nil {
		t.Fatal("缺失范围可修改作品集关联")
	}
	if _, err = workflow.CompleteReview(member, other, source.BusinessID, updated.Revision); !fault.Is(err, 404, "not_found") {
		t.Fatal("同组织其他用户可复制总结")
	}
	if err = projects.Delete(orgA, user, source.BusinessID); err != nil {
		t.Fatal(err)
	}
	if copy, e := workflow.GetReview(orgA, user, reviewID); e != nil || copy.Content != "独立编辑" {
		t.Fatal("删除源策划破坏独立总结", e)
	}
}
