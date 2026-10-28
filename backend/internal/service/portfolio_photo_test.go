package service

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"
)

type portfolioPhotoTestRepo struct {
	user, portfolio uuid.UUID
	created         *model.PortfolioPhoto
	inspiration     model.Inspiration
	saveErr         error
}

func (r *portfolioPhotoTestRepo) FindPortfolio(_ context.Context, user, id uuid.UUID) (model.Portfolio, error) {
	if user != r.user || id != r.portfolio {
		return model.Portfolio{}, errors.New("not found")
	}
	return model.Portfolio{UserID: user}, nil
}
func (r *portfolioPhotoTestRepo) ListPortfolioPhotos(context.Context, uuid.UUID, uuid.UUID) ([]model.PortfolioPhoto, error) {
	return nil, nil
}
func (r *portfolioPhotoTestRepo) CreatePortfolioPhoto(_ context.Context, item *model.PortfolioPhoto) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.created = item
	return nil
}
func (r *portfolioPhotoTestRepo) FindInspirationByBusinessID(_ context.Context, user, id uuid.UUID) (model.Inspiration, error) {
	if user != r.user || id != r.inspiration.BusinessID {
		return model.Inspiration{}, errors.New("not found")
	}
	return r.inspiration, nil
}

type portfolioPhotoTestFiles struct {
	stored, source []byte
	removed        bool
}

func (f *portfolioPhotoTestFiles) StorePublic(_ context.Context, _, _ string, r io.Reader, _ int64) (string, error) {
	f.stored, _ = io.ReadAll(r)
	return "public-test-key", nil
}
func (f *portfolioPhotoTestFiles) Delete(context.Context, string) error { f.removed = true; return nil }
func (f *portfolioPhotoTestFiles) Open(string) (io.ReadCloser, int64, string, error) {
	return nil, 0, "", errors.New("context required")
}
func (f *portfolioPhotoTestFiles) OpenContext(ctx context.Context, key string) (io.ReadCloser, int64, string, error) {
	if _, err := tenant.Require(ctx); err != nil {
		return nil, 0, "", err
	}
	if key != "owned-key" {
		return nil, 0, "", errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(f.source)), int64(len(f.source)), "image/png", nil
}

// TestPortfolioCopyAndCleanup 验证独立复制、元数据保留以及写入失败时回收文件。
func TestPortfolioCopyAndCleanup(t *testing.T) {
	var data bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	user, album, source := uuid.New(), uuid.New(), uuid.New()
	focal := 35.0
	repo := &portfolioPhotoTestRepo{user: user, portfolio: album, inspiration: model.Inspiration{BaseModel: model.BaseModel{BusinessID: source}, UserID: user, ImageKey: "owned-key", FileName: "photo.png", ImageMetadata: model.InspirationImageMetadata{FocalLengthMM: &focal}}}
	files := &portfolioPhotoTestFiles{source: data.Bytes()}
	service := NewPortfolioPhotoService(repo, files, 1024, func(context.Context) (string, error) { return "local", nil })
	ctx := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 17, UserID: user})
	photo, err := service.CopyInspiration(ctx, user, album, source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(files.stored, files.source) || !bytes.Contains(photo.ImageMetadata, []byte(`"focal_length_mm":35`)) {
		t.Fatalf("副本或属性丢失: %s", photo.ImageMetadata)
	}
	if _, err := service.CopyInspiration(context.Background(), user, album, source); err == nil {
		t.Fatal("缺失空间应拒绝")
	}
	repo.saveErr = errors.New("save failed")
	files.removed = false
	if _, err := service.CopyInspiration(ctx, user, album, source); err == nil || !files.removed {
		t.Fatal("存储失败必须回收公开副本")
	}
}
