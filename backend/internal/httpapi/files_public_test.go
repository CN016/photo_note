package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/photo-nini/backend/internal/filestore"
)

// TestPublicFeedbackImageRequiresActualImageBytes 确认 public 路由按文件头识别内容，不信任图片扩展名。
func TestPublicFeedbackImageRequiresActualImageBytes(t *testing.T) {
	manager, err := filestore.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	validKey, err := manager.StorePublic(context.Background(), "local", "feedback.png", bytes.NewReader(pngBytes(t)), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	spoofedKey, err := manager.StorePublic(context.Background(), "local", "plain-text.png", strings.NewReader("not an image"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	// 普通下载为历史兼容会按扩展名回退；公共接口仍必须检查真实文件头。
	spoofedReader, _, fallbackType, err := manager.Open(spoofedKey)
	if err != nil {
		t.Fatal(err)
	}
	_ = spoofedReader.Close()
	if fallbackType != "image/png" {
		t.Fatalf("test did not exercise extension fallback: got %q", fallbackType)
	}

	router := testRouter(Dependencies{Files: manager})
	valid := httptest.NewRecorder()
	router.ServeHTTP(valid, httptest.NewRequest(http.MethodGet, "/api/v1/public-feedback-images/"+validKey, nil))
	if valid.Code != http.StatusOK || valid.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("valid public image response = %d/%s, want 200/image/png", valid.Code, valid.Header().Get("Content-Type"))
	}
	spoofed := httptest.NewRecorder()
	router.ServeHTTP(spoofed, httptest.NewRequest(http.MethodGet, "/api/v1/public-feedback-images/"+spoofedKey, nil))
	if spoofed.Code != http.StatusNotFound {
		t.Fatalf("non-image bytes with .png extension status = %d, want 404", spoofed.Code)
	}
	privateKey, err := manager.Store(context.Background(), "local", "private.png", bytes.NewReader(pngBytes(t)), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	private := httptest.NewRecorder()
	router.ServeHTTP(private, httptest.NewRequest(http.MethodGet, "/api/v1/public-feedback-images/"+privateKey, nil))
	if private.Code != http.StatusNotFound {
		t.Fatalf("private file public read status = %d, want 404", private.Code)
	}
}
