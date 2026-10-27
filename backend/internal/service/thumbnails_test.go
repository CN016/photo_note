package service

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/tenant"
)

// TestThumbnailAlwaysAuthorizes 覆盖个人、两个组织、同组织他人及缺失上下文，缓存命中也必须重新鉴权。
func TestThumbnailAlwaysAuthorizes(t *testing.T) {
	raw, e := filestore.NewManager(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	files := NewFileService(raw, &fileOwnershipMemory{})
	files.thumbnailConfig = func(context.Context) (ThumbnailConfig, error) {
		return ThumbnailConfig{Dimension: 384, Quality: 78, MaxPixels: 40000000, Workers: 2, MaxBytes: 1000000}, nil
	}
	files.thumbnailSlots = make(chan struct{}, 2)
	user := uuid.New()
	ctx := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 17, UserID: user})
	var imageBytes bytes.Buffer
	jpeg.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 40, 30)), nil)
	key, e := files.Store(ctx, "local", "image.jpg", bytes.NewReader(imageBytes.Bytes()), 1000000)
	if e != nil {
		t.Fatal(e)
	}
	first, _, e := files.OpenThumbnail(ctx, key)
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, first)
	first.Close()
	for _, foreign := range []context.Context{context.Background(), tenant.Personal(context.Background(), user), tenant.WithScope(context.Background(), tenant.Scope{OrgID: 18, UserID: user}), tenant.WithScope(context.Background(), tenant.Scope{OrgID: 17, UserID: uuid.New()})} {
		if reader, _, err := files.OpenThumbnail(foreign, key); err == nil {
			reader.Close()
			t.Fatal("缓存绕过用户/空间授权")
		}
	}
	// 相同key在权限撤销后同样不可访问已有磁盘缓存。
	delete(files.ownership.(*fileOwnershipMemory).items, key)
	if _, _, err := files.OpenThumbnail(ctx, key); err == nil {
		t.Fatal("撤销权限后仍可读取缓存")
	}
}
