package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/service"
	"github.com/photo-nini/backend/internal/store"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestPortfolioHTTPPostgres 验证真实数据库与本地文件中的创建、上传、分享、撤销和删除流程。
func TestPortfolioHTTPPostgres(t *testing.T) {
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if dsn == "" {
		t.Skip("未配置独立 PostgreSQL 测试库")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	repo := store.NewPostgresStore(db)
	manager, err := filestore.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := service.NewFileService(manager, repo)
	user := uuid.New()
	photos := service.NewPortfolioPhotoService(repo, files, 1024*1024, func(context.Context) (string, error) { return "local", nil })
	tokens := &testTokenStore{userID: user}
	router := testRouter(Dependencies{Tokens: tokens, Files: files, Portfolios: service.NewPortfolioService(repo), PortfolioShares: service.NewPortfolioShareService(repo), PortfolioPhotos: photos, MaxUploadBytes: 1024 * 1024, MaxBodyBytes: 1024 * 1024})
	request := func(method, path string, body *bytes.Buffer, authorized bool, contentType string) *httptest.ResponseRecorder {
		var req *http.Request
		if body == nil {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, body)
		}
		if authorized {
			req.Header.Set("Authorization", "Bearer valid")
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	created := request("POST", "/api/v1/portfolios", bytes.NewBufferString(`{"title":"test","description":"","tag_ids":[]}`), true, "application/json")
	if created.Code != 201 {
		t.Fatalf("创建作品集 %d %s", created.Code, created.Body.String())
	}
	var album struct {
		Data struct {
			ID string `json:"business_id"`
		} `json:"data"`
	}
	if err = json.Unmarshal(created.Body.Bytes(), &album); err != nil || album.Data.ID == "" {
		t.Fatal("缺少作品集 ID", err)
	}
	var file bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(&file, img); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	multi := multipart.NewWriter(&body)
	part, err := multi.CreateFormFile("file", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(file.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err = multi.Close(); err != nil {
		t.Fatal(err)
	}
	upload := request("POST", "/api/v1/portfolios/"+album.Data.ID+"/photos", &body, true, multi.FormDataContentType())
	if upload.Code != 201 {
		t.Fatalf("上传图片 %d %s", upload.Code, upload.Body.String())
	}
	var result struct {
		Data struct {
			Key string `json:"image_key"`
		} `json:"data"`
	}
	if err = json.Unmarshal(upload.Body.Bytes(), &result); err != nil || !strings.HasPrefix(result.Data.Key, "public_") {
		t.Fatal("未产生公开文件", err)
	}

	// 从拥有的私有灵感图复制文件与元数据；其他用户不能借用该记录。
	personal := tenant.Personal(context.Background(), user)
	privateKey, err := files.Store(personal, "local", "source.png", bytes.NewReader(file.Bytes()), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	focal := 50.0
	latitude := 31.2
	serial := "camera-serial-test"
	inspiration := model.Inspiration{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, ImageKey: privateKey, FileName: "source.png", TagIDs: pq.StringArray{}, Data: model.JSONB(`{}`), ImageMetadata: model.InspirationImageMetadata{FocalLengthMM: &focal, GPSLatitude: &latitude, CameraSerialNumber: &serial, Extra: model.JSONB(`{}`)}}
	if err = db.WithContext(personal).Create(&inspiration).Error; err != nil {
		t.Fatal(err)
	}
	copyBody := func() *bytes.Buffer {
		return bytes.NewBufferString(`{"inspiration_id":"` + inspiration.BusinessID.String() + `"}`)
	}
	tokens.userID = uuid.New()
	denied := request("POST", "/api/v1/portfolios/"+album.Data.ID+"/photos/copy", copyBody(), true, "application/json")
	if denied.Code == 201 {
		t.Fatal("其他用户不能复制作品集或灵感图")
	}
	tokens.userID = user
	copied := request("POST", "/api/v1/portfolios/"+album.Data.ID+"/photos/copy", copyBody(), true, "application/json")
	if copied.Code != 201 || !strings.Contains(copied.Body.String(), `"focal_length_mm":50`) {
		t.Fatalf("复制灵感图失败 %d %s", copied.Code, copied.Body.String())
	}
	var copiedResult struct {
		Data struct {
			Key string `json:"image_key"`
		} `json:"data"`
	}
	if err = json.Unmarshal(copied.Body.Bytes(), &copiedResult); err != nil || copiedResult.Data.Key == privateKey || copiedResult.Data.Key == result.Data.Key {
		t.Fatal("复制未生成独立副本", err)
	}
	share := request("POST", "/api/v1/portfolios/"+album.Data.ID+"/share", nil, true, "")
	if share.Code != 200 {
		t.Fatalf("分享 %d %s", share.Code, share.Body.String())
	}
	var shareBody struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err = json.Unmarshal(share.Body.Bytes(), &shareBody); err != nil || shareBody.Data.Token == "" {
		t.Fatal("缺少令牌", err)
	}
	public := request("GET", "/api/v1/public-portfolios/"+shareBody.Data.Token, nil, false, "")
	if public.Code != 200 || !strings.Contains(public.Body.String(), result.Data.Key) || !strings.Contains(public.Body.String(), `"gps_latitude":31.2`) || !strings.Contains(public.Body.String(), "camera-serial-test") || strings.Contains(public.Body.String(), "project_ids") {
		t.Fatalf("公开详情 %d %s", public.Code, public.Body.String())
	}
	oldToken := shareBody.Data.Token
	reset := request("POST", "/api/v1/portfolios/"+album.Data.ID+"/share", nil, true, "")
	if reset.Code != 200 {
		t.Fatal("重置分享失败", reset.Code)
	}
	if err = json.Unmarshal(reset.Body.Bytes(), &shareBody); err != nil || shareBody.Data.Token == oldToken {
		t.Fatal("重置必须生成新的令牌", err)
	}
	if got := request("GET", "/api/v1/public-portfolios/"+oldToken, nil, false, ""); got.Code != 404 {
		t.Fatalf("重置后旧链接仍有效 %d", got.Code)
	}
	revoke := request("DELETE", "/api/v1/portfolios/"+album.Data.ID+"/share", nil, true, "")
	if revoke.Code != 200 {
		t.Fatal("撤销失败", revoke.Code)
	}
	if got := request("GET", "/api/v1/public-portfolios/"+shareBody.Data.Token, nil, false, ""); got.Code != 404 {
		t.Fatalf("旧链接未失效 %d", got.Code)
	}
	if got := request("GET", "/api/v1/public-portfolio-images/"+result.Data.Key, nil, false, ""); got.Code != 200 {
		t.Fatalf("公开图片直链不应随撤销失效 %d", got.Code)
	}
	deleted := request("DELETE", "/api/v1/portfolios/"+album.Data.ID, nil, true, "")
	if deleted.Code != 200 {
		t.Fatalf("删除失败 %d %s", deleted.Code, deleted.Body.String())
	}
	if got := request("GET", "/api/v1/public-portfolio-images/"+result.Data.Key, nil, false, ""); got.Code != 404 {
		t.Fatalf("删除后公开文件仍存在 %d", got.Code)
	}
}
