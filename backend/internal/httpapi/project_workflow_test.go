package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/service"
	"github.com/photo-nini/backend/internal/store"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestProjectWorkflowHTTPPostgres 覆盖真实接口及数据库；自动档期必须在保存响应完成前处理，不能依赖编辑页停留。
func TestProjectWorkflowHTTPPostgres(t *testing.T) {
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if dsn == "" {
		t.Skip("需要已迁移的独立 PostgreSQL 测试库")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(config.Database, "_test") {
		t.Fatal("必须使用名称以 _test 结尾的独立测试库")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	defer connection.Close()
	repo := store.NewPostgresStore(db)
	projects := service.NewProjectService(repo, nil, 1024)
	workflow := service.NewProjectWorkflowService(repo, service.NewRuntimeConfigService(repo))
	user := uuid.New()
	ctx := tenant.Personal(context.Background(), user)
	t.Cleanup(func() {
		for _, table := range []any{&model.ProjectReview{}, &model.Project{}, &model.ScheduleItem{}} {
			db.WithContext(ctx).Where("user_id = ?", user).Delete(table)
		}
	})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	routes := router.Group("/api/v1")
	routes.Use(func(c *gin.Context) {
		requestCtx := context.WithValue(c.Request.Context(), claimsKey{}, store.TokenClaims{UserBusinessID: user, SessionID: "project-workflow-http-test"})
		c.Request = c.Request.WithContext(tenant.Personal(requestCtx, user))
	})
	routes.GET("/projects", listProjectsHandler(projects))
	routes.POST("/projects", createProjectHandler(projects, workflow))
	routes.GET("/projects/:business_id", getProjectHandler(projects))
	routes.PATCH("/projects/:business_id", patchProjectHandler(projects, workflow))
	registerProjectWorkflow(routes, workflow)
	call := func(method, path string, input any, status int) json.RawMessage {
		t.Helper()
		body, _ := json.Marshal(input)
		request := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, response.Code, response.Body.String())
		}
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if status < 400 {
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
		}
		return envelope.Data
	}
	start := time.Date(2026, 12, 5, 2, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	shoot := model.ProjectShooting{StartAt: &start, EndAt: &end, Location: "海边", Person: "九九", Kind: "video"}
	input := service.ProjectSave{Title: "接口验收拍摄", CoverEmoji: "cover", DocumentSchemaVersion: 1, Document: json.RawMessage(`{"type":"doc","content":[{"type":"taskList","content":[{"type":"taskItem","attrs":{"checked":true},"content":[{"type":"paragraph"}]}]}]}`), Shooting: &shoot}
	var source service.ProjectView
	if err = json.Unmarshal(call("POST", "/projects", input, 201), &source); err != nil {
		t.Fatal(err)
	}
	if source.ScheduleID == nil || source.ScheduleSyncState != "linked" || source.Status != "executed" {
		t.Fatalf("保存响应未完成自动档期: %#v", source)
	}
	var event model.ScheduleItem
	if err = db.WithContext(ctx).Where("business_id = ?", source.ScheduleID).First(&event).Error; err != nil {
		t.Fatal(err)
	}
	summary := "收音需要再检查"
	input.Revision = source.Revision
	input.ManualSummary = &summary
	if err = json.Unmarshal(call("PATCH", "/projects/"+source.BusinessID.String(), input, 200), &source); err != nil {
		t.Fatal(err)
	}
	if source.Status != "completed" {
		t.Fatal("复盘保存未更新状态")
	}
	var review service.ProjectReviewView
	json.Unmarshal(call("POST", "/projects/"+source.BusinessID.String()+"/review", map[string]any{"revision": source.Revision}, 200), &review)
	var retry service.ProjectReviewView
	json.Unmarshal(call("POST", "/projects/"+source.BusinessID.String()+"/review", map[string]any{"revision": 1}, 200), &retry)
	if review.BusinessID == uuid.Nil || review.BusinessID != retry.BusinessID {
		t.Fatal("接口重试生成多个副本")
	}
	call("PATCH", "/project-reviews/"+review.BusinessID.String(), service.ProjectReviewInput{Title: review.Title, Content: "独立总结", Revision: 1}, 200)
	call("PATCH", "/project-reviews/"+review.BusinessID.String(), service.ProjectReviewInput{Title: review.Title, Content: "过期版本", Revision: 1}, 409)
	call("POST", "/projects/"+source.BusinessID.String()+"/ai-analysis", service.ProjectAIInput{Revision: source.Revision, Locale: "zh-CN"}, 503)
	list := call("GET", "/projects?status=completed", nil, 200)
	if bytes.Contains(list, []byte(`"document"`)) || bytes.Contains(list, []byte(`"manual_summary"`)) {
		t.Fatal("列表泄漏完整正文")
	}
	call("GET", "/projects?status=invalid", nil, 400)
	call("GET", "/projects?status=completed&status=executed", nil, 400)
	input.Title = "已有时间候选"
	input.Revision = 1
	var candidate service.ProjectView
	json.Unmarshal(call("POST", "/projects", input, 201), &candidate)
	if candidate.ScheduleID != nil || candidate.ScheduleSyncState != "candidates" {
		t.Fatal("已有候选未等用户确认")
	}
	call("POST", "/projects/"+candidate.BusinessID.String()+"/schedule", service.ProjectScheduleInput{Revision: 1, ScheduleID: source.ScheduleID}, 200)
	var count int64
	db.WithContext(ctx).Model(&model.ScheduleItem{}).Where("user_id = ?", user).Count(&count)
	if count != 1 {
		t.Fatal("确认关联时重复创建档期")
	}
	unauthenticated := gin.New()
	group := unauthenticated.Group("/api/v1")
	registerProjectWorkflow(group, workflow)
	response := httptest.NewRecorder()
	unauthenticated.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/project-reviews", nil))
	if response.Code != 401 {
		t.Fatal("总结接口未要求认证")
	}
}
