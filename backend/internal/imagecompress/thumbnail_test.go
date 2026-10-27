package imagecompress

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"testing"
)

// TestThumbnailLimitsCannotUseCompatibilityFallback 验证损坏/超限普通图不会绕过服务端边界退回客户端。
func TestThumbnailLimitsCannotUseCompatibilityFallback(t *testing.T) {
	var source bytes.Buffer
	png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 100, 100)))
	options := ThumbnailOptions{Dimension: 384, Quality: 78, MaxPixels: 10, MaxBytes: 1000000}
	for _, content := range [][]byte{source.Bytes(), []byte("invalid-file")} {
		if _, err := Thumbnail(content, options); !errors.Is(err, ErrInvalidThumbnailImage) {
			t.Fatalf("超限或损坏图错误分类不正确: %v", err)
		}
	}
	if _, err := Thumbnail([]byte("0000ftypheic0000"), options); !errors.Is(err, ErrUnsupportedImage) {
		t.Fatal("历史HEIC兼容入口不可用")
	}
}
