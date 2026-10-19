package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/service"
)

func TestInspirationMetadataCreatePatchDetailAndList(t *testing.T) {
	userID := uuid.New()
	repository := &imageMetadataAPITestRepository{userID: userID}
	inspirations := service.NewInspirationService(repository, nil, 0)
	router := testRouter(Dependencies{Tokens: &testTokenStore{userID: userID}, Inspirations: inspirations})

	created := metadataAPICall(t, router, http.MethodPost, "/api/v1/inspirations", `{"image_key":"legacy-image","file_name":"photo.jpg","data":{"schema_version":1},"image_metadata":{"source_file_size":1234,"source_last_modified_at":"2026-04-15T01:06:00Z","title":"browser title","extra":{"exifr:xmp:Rating":4}}}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var createEnvelope struct {
		Data inspirationResponse `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createEnvelope); err != nil {
		t.Fatal(err)
	}
	if createEnvelope.Data.ImageMetadata == nil || createEnvelope.Data.ImageMetadata.SourceFileSize == nil || *createEnvelope.Data.ImageMetadata.SourceFileSize != 1234 {
		t.Fatalf("create response did not return client source metadata: %+v", createEnvelope.Data.ImageMetadata)
	}
	if createEnvelope.Data.ImageMetadata.Title == nil || *createEnvelope.Data.ImageMetadata.Title != "browser title" {
		t.Fatalf("create response title missing: %+v", createEnvelope.Data.ImageMetadata)
	}
	id := createEnvelope.Data.BusinessID.String()

	patched := metadataAPICall(t, router, http.MethodPatch, "/api/v1/inspirations/"+id,
		`{"image_key":"replacement-image","image_metadata":{"title":"updated title","extra":{}}}`)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", patched.Code, patched.Body.String())
	}
	var patchEnvelope struct {
		Data inspirationResponse `json:"data"`
	}
	if err := json.Unmarshal(patched.Body.Bytes(), &patchEnvelope); err != nil {
		t.Fatal(err)
	}
	if patchEnvelope.Data.ImageMetadata == nil || patchEnvelope.Data.ImageMetadata.Title == nil || *patchEnvelope.Data.ImageMetadata.Title != "updated title" {
		t.Fatalf("patch response did not return updated metadata: %+v", patchEnvelope.Data.ImageMetadata)
	}
	if patchEnvelope.Data.ImageMetadata.SourceFileSize != nil {
		t.Fatalf("changing images should not retain the previous original size: %v", patchEnvelope.Data.ImageMetadata.SourceFileSize)
	}

	detail := metadataAPICall(t, router, http.MethodGet, "/api/v1/inspirations/"+id, "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"image_metadata"`) {
		t.Fatalf("detail did not return image metadata: status=%d body=%s", detail.Code, detail.Body.String())
	}
	list := metadataAPICall(t, router, http.MethodGet, "/api/v1/inspirations", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	var listEnvelope struct {
		Data struct {
			Items []map[string]json.RawMessage `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(listEnvelope.Data.Items) != 1 {
		t.Fatalf("list item count = %d, want 1", len(listEnvelope.Data.Items))
	}
	if _, exists := listEnvelope.Data.Items[0]["image_metadata"]; exists {
		t.Fatal("list response should omit the image_metadata object")
	}
}

func metadataAPICall(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

type imageMetadataAPITestRepository struct {
	userID uuid.UUID
	items  []model.Inspiration
}

func (*imageMetadataAPITestRepository) ValidateTagOwnership(context.Context, uuid.UUID, []uuid.UUID) error {
	return nil
}
func (repository *imageMetadataAPITestRepository) CreateInspiration(_ context.Context, item *model.Inspiration) error {
	item.CreatedAt, item.UpdatedAt = time.Now().UTC(), time.Now().UTC()
	repository.items = append(repository.items, *item)
	return nil
}
func (repository *imageMetadataAPITestRepository) ListInspirations(_ context.Context, userID uuid.UUID, _ []uuid.UUID, _, _, _ string, _, _ int) ([]model.Inspiration, int64, error) {
	if userID != repository.userID {
		return nil, 0, nil
	}
	return append([]model.Inspiration(nil), repository.items...), int64(len(repository.items)), nil
}
func (repository *imageMetadataAPITestRepository) FindInspirationByBusinessID(_ context.Context, userID, itemID uuid.UUID) (model.Inspiration, error) {
	for _, item := range repository.items {
		if userID == repository.userID && item.BusinessID == itemID {
			return item, nil
		}
	}
	return model.Inspiration{}, fault.NotFound()
}
func (repository *imageMetadataAPITestRepository) UpdateInspiration(ctx context.Context, userID, itemID uuid.UUID, updates map[string]any) (model.Inspiration, error) {
	return repository.applyUpdate(userID, itemID, updates, false)
}
func (repository *imageMetadataAPITestRepository) UpdateInspirationWithAsyncVersion(ctx context.Context, userID, itemID uuid.UUID, updates map[string]any) (model.Inspiration, error) {
	return repository.applyUpdate(userID, itemID, updates, true)
}
func (repository *imageMetadataAPITestRepository) applyUpdate(userID, itemID uuid.UUID, updates map[string]any, increment bool) (model.Inspiration, error) {
	for index := range repository.items {
		item := &repository.items[index]
		if userID != repository.userID || item.BusinessID != itemID {
			continue
		}
		if value, ok := updates["image_key"].(string); ok {
			item.ImageKey = value
		}
		if value, ok := updates["file_name"].(string); ok {
			item.FileName = value
		}
		if value, ok := updates["data"].(model.JSONB); ok {
			item.Data = value
		}
		if title, ok := updates["title"].(*string); ok {
			item.ImageMetadata.Title = title
		}
		if size, exists := updates["source_file_size"]; exists {
			item.ImageMetadata.SourceFileSize, _ = size.(*int64)
		}
		if increment {
			item.AsyncVersion++
		}
		item.UpdatedAt = time.Now().UTC()
		return *item, nil
	}
	return model.Inspiration{}, fault.NotFound()
}
func (*imageMetadataAPITestRepository) UpdateInspirationFileMetadataIfVersion(context.Context, uuid.UUID, uuid.UUID, string, int64, *string, *int64) (bool, error) {
	return false, nil
}
func (*imageMetadataAPITestRepository) UpdateInspirationImageMetadataIfVersion(context.Context, uuid.UUID, uuid.UUID, string, int64, model.InspirationImageMetadata) (bool, error) {
	return false, nil
}
func (*imageMetadataAPITestRepository) RemoveInspirationSourceImageKeyIfVersion(context.Context, uuid.UUID, uuid.UUID, string, int64, string) (bool, error) {
	return false, nil
}
func (*imageMetadataAPITestRepository) IsInspirationFileReferenced(context.Context, string, uuid.UUID) (bool, error) {
	return false, nil
}
func (*imageMetadataAPITestRepository) DeleteInspiration(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
