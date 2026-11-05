// 本工具仅在本机独立测试库上运行交付 UI 验收：业务接口、文件服务和 worker 为真实实现，账号为测试替身。
// 不得部署此工具。必须显式设置 PHOTO_NINI_DELIVERY_QA=1，且 DSN 必须指向名为 delivery_test 的独立库。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/httpapi"
	"github.com/photo-nini/backend/internal/security"
	"github.com/photo-nini/backend/internal/service"
	"github.com/photo-nini/backend/internal/store"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// qaTokens 只接受明确的测试令牌，不读生产 token、Redis 或用户配置。
type qaTokens struct{ user uuid.UUID }

func (q qaTokens) ValidateAccess(_ context.Context, token string) (store.TokenClaims, error) {
	if token != "delivery-qa-access" {
		return store.TokenClaims{}, fault.Unauthorized()
	}
	return store.TokenClaims{UserBusinessID: q.user, OrgID: -1, SessionID: "delivery-qa", SessionVersion: 1}, nil
}
func (q qaTokens) ResolveScope(_ context.Context, user uuid.UUID, org int64, session string, version int64) (tenant.Scope, error) {
	if user != q.user || org != -1 || session != "delivery-qa" || version != 1 {
		return tenant.Scope{}, fault.Unauthorized()
	}
	return tenant.Scope{OrgID: -1, UserID: user, SessionID: session, Version: version}, nil
}

// main 对随机 schema 执行所有迁移，输出仅包含测试资源业务 UUID，令牌与凭据不进入日志。
func main() {
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if os.Getenv("PHOTO_NINI_DELIVERY_QA") != "1" || !strings.Contains(dsn, "dbname=delivery_test") {
		log.Fatal("仅允许显式启用的独立 delivery_test 库")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, TranslateError: true, Logger: logger.Discard})
	must(err)
	sqlDB, err := db.DB()
	must(err)
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	schema := "delivery_ui_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	must(db.Exec(`CREATE SCHEMA "` + schema + `"`).Error)
	defer db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
	must(db.Exec(`SET search_path TO "` + schema + `", public`).Error)
	migrations, err := filepath.Glob("migrations/*.sql")
	must(err)
	if len(migrations) == 0 {
		log.Fatal("请在 backend 目录运行")
	}
	for _, p := range migrations {
		raw, e := os.ReadFile(p)
		must(e)
		must(db.Exec(string(raw)).Error)
	}
	root, err := os.MkdirTemp("", "photo-nini-delivery-qa-")
	must(err)
	defer os.RemoveAll(root)
	repository := store.NewPostgresStore(db)
	config := service.NewRuntimeConfigService(repository)
	manager, err := filestore.NewManager(root)
	must(err)
	files := service.NewFileService(manager, repository)
	must(files.ConfigureThumbnails(context.Background(), config))
	passwords, err := security.NewPasswordManager()
	must(err)
	deliveries := service.NewDeliveryService(repository, files, config, passwords, 20000000)
	notices := service.NewNoticeService(repository, config)
	user := uuid.New()
	ctx := tenant.Personal(context.Background(), user)
	task, err := deliveries.Create(ctx, service.DeliveryInput{Title: "秋日人像 · 交付验收", Customer: "测试客户", Description: "请选择喜欢的照片，并填写修图备注。", LimitMode: "exact", SelectionLimit: 2, AllowDownload: true})
	must(err)
	palette := []color.RGBA{{194, 135, 78, 255}, {95, 134, 128, 255}, {141, 115, 145, 255}, {121, 139, 85, 255}, {83, 113, 146, 255}, {172, 108, 98, 255}}
	for i, c := range palette {
		img := image.NewRGBA(image.Rect(0, 0, 1000, 700))
		for y := 0; y < 700; y++ {
			for x := 0; x < 1000; x++ {
				value := c
				if x > 300 && x < 700 && y > 130 && y < 650 {
					value.R = uint8(int(c.R) * 7 / 10)
					value.G = uint8(int(c.G) * 7 / 10)
				}
				img.SetRGBA(x, y, value)
			}
		}
		var raw bytes.Buffer
		must(jpeg.Encode(&raw, img, &jpeg.Options{Quality: 95}))
		detail, e := deliveries.Get(ctx, task.BusinessID, false)
		must(e)
		_, e = deliveries.Upload(ctx, task.BusinessID, detail.Task.Version, "proof", fmt.Sprintf("DSC_%04d.JPG", i+1), "人像", bytes.NewReader(raw.Bytes()))
		must(e)
	}
	detail, err := deliveries.Get(ctx, task.BusinessID, false)
	must(err)
	_, err = deliveries.Command(ctx, task.BusinessID, service.DeliveryCommand{Version: detail.Task.Version, Action: "publish"})
	must(err)
	detail, err = deliveries.Get(ctx, task.BusinessID, false)
	must(err)
	share, err := deliveries.Command(ctx, task.BusinessID, service.DeliveryCommand{Version: detail.Task.Version, Action: "share"})
	must(err)
	// 测试链接只写入受控临时文件，终端不输出分享令牌。
	fixture := os.Getenv("PHOTO_NINI_DELIVERY_QA_FIXTURE")
	if fixture != "" {
		raw, _ := json.Marshal(map[string]string{"task_id": task.BusinessID.String(), "public_path": "/d/" + share})
		must(os.WriteFile(fixture, raw, 0600))
	}
	go deliveries.RunExportWorker(context.Background())
	go deliveries.RunNoticeWorker(context.Background(), notices)
	router := httpapi.Router(httpapi.Dependencies{Deliveries: deliveries, Tokens: qaTokens{user}, Files: files, MaxUploadBytes: 20000000, MaxBodyBytes: 2000000, PostgresPing: sqlDB.PingContext, RedisPing: func(context.Context) error { return nil }})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/auth/refresh" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"tokens": store.TokenPair{AccessToken: "delivery-qa-access", RefreshToken: "delivery-qa-refresh", OrgID: -1, SessionID: "delivery-qa", SessionVersion: 1, TokenType: "Bearer", ExpiresIn: 3600}}})
			return
		}
		if r.URL.Path == "/api/v1/users/me" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"business_id": user, "nickname": "交付验收", "phone": "", "avatar_key": nil, "extra": map[string]any{}, "is_admin": false}})
			return
		}
		if r.URL.Path == "/api/v1/notifications/unread-count" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]int{"unread_count": 0}})
			return
		}
		router.ServeHTTP(w, r)
	})
	log.Printf("交付 QA 服务仅监听 127.0.0.1:18859，使用独立临时 schema")
	log.Fatal(http.ListenAndServe("127.0.0.1:18859", handler))
}

// must 仅用于本机测试工具初始化；业务响应由实际服务处理。
func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
