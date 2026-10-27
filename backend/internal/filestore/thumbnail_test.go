package filestore

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/photo-nini/backend/internal/imagecompress"
)

// TestThumbnailPersistentCopy 验证持久命中不重写、尺寸有界、原图保护、换图失效和删除清理。
func TestThumbnailPersistentCopy(t *testing.T) {
	root := t.TempDir()
	manager, e := NewManager(root)
	if e != nil {
		t.Fatal(e)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1200, 800))
	for y := 0; y < 800; y++ {
		for x := 0; x < 1200; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8((x*17 + y*31) % 256), uint8((x*7 + y*23) % 256), uint8(x * y % 256), 255})
		}
	}
	var original bytes.Buffer
	jpeg.Encode(&original, img, &jpeg.Options{Quality: 95})
	source := original.Bytes()
	key, e := manager.Store(context.Background(), "local", "source.jpg", bytes.NewReader(source), 10000000)
	if e != nil {
		t.Fatal(e)
	}
	options := imagecompress.ThumbnailOptions{Dimension: 384, Quality: 78, MaxPixels: 40000000, MaxBytes: 10000000}
	read := func() []byte {
		r, _, err := manager.OpenThumbnail(context.Background(), key, options)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first := read()
	config, _, e := image.DecodeConfig(bytes.NewReader(first))
	if e != nil || config.Width != 384 || config.Height != 256 || len(first) >= len(source) {
		t.Fatal("缩略图尺寸或体积不正确")
	}
	parsed, _ := ParseKey(key)
	path := manager.localPath(parsed)
	matches, _ := filepath.Glob(path + ".thumb-v1-*.jpg")
	if len(matches) != 1 {
		t.Fatal("缓存未持久保存")
	}
	info, _ := os.Stat(matches[0])
	if !bytes.Equal(first, read()) {
		t.Fatal("缓存内容不稳定")
	}
	later, _ := os.Stat(matches[0])
	if !later.ModTime().Equal(info.ModTime()) {
		t.Fatal("命中缓存仍发生重编码")
	}
	raw, _, _, _ := manager.Open(key)
	unchanged, _ := io.ReadAll(raw)
	raw.Close()
	if !bytes.Equal(unchanged, source) {
		t.Fatal("缩略图破坏原始内容")
	}
	var replacement bytes.Buffer
	jpeg.Encode(&replacement, image.NewRGBA(image.Rect(0, 0, 120, 80)), nil)
	if e = manager.Replace(context.Background(), key, replacement.Bytes()); e != nil {
		t.Fatal(e)
	}
	if bytes.Equal(first, read()) {
		t.Fatal("换图后读到旧缩略图")
	}
	if e = manager.Delete(context.Background(), key); e != nil {
		t.Fatal(e)
	}
	matches, _ = filepath.Glob(path + ".thumb-v1-*.jpg")
	if len(matches) != 0 {
		t.Fatal("删除原图未清理副本")
	}
	if _, _, e = manager.OpenThumbnail(context.Background(), key, options); e == nil {
		t.Fatal("源文件删除后仍可读取缩略图")
	}
}
