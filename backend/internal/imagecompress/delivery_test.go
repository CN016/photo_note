package imagecompress

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// TestDeliveryPreviewBudget 使用高纹理大图验证尺寸、体积和基本结构保留，不以字节数宣称主观画质。
func TestDeliveryPreviewBudget(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3600, 2400))
	state := uint32(12345)
	for y := 0; y < 2400; y++ {
		for x := 0; x < 3600; x++ {
			state = 1664525*state + 1013904223
			img.SetRGBA(x, y, color.RGBA{uint8((x / 12) % 256), uint8((y / 12) % 256), uint8(state >> 24), 255})
		}
	}
	var source bytes.Buffer
	if err := jpeg.Encode(&source, img, &jpeg.Options{Quality: 98}); err != nil {
		t.Fatal(err)
	}
	original := append([]byte{}, source.Bytes()...)
	preview, err := DeliveryPreview(source.Bytes(), 1000000, 2560)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) > 1000000 || len(preview) == 0 {
		t.Fatalf("预览体积错误: %d", len(preview))
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(preview))
	if err != nil || format != "jpeg" || max(cfg.Width, cfg.Height) > 2560 || cfg.Width < 1600 {
		t.Fatalf("尺寸或编码异常: %+v %s %v", cfg, format, err)
	}
	if !bytes.Equal(source.Bytes(), original) {
		t.Fatal("原始字节被修改")
	}
}

// TestDeliveryPreviewSmallAndInvalid 确认小图不放大且格式校验不会绕过解码。
func TestDeliveryPreviewSmallAndInvalid(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 80, 40))
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	p, err := DeliveryPreview(b.Bytes(), 1000000, 2560)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(p))
	if cfg.Width != 80 || cfg.Height != 40 {
		t.Fatal(cfg)
	}
	if _, err = DeliveryPreview([]byte("not a photo"), 1000000, 2560); err == nil {
		t.Fatal("非法图片未拒绝")
	}
	if _, err = DeliveryPreview(b.Bytes(), 1, 2560); err == nil {
		t.Fatal("非法压缩限制未拒绝")
	}
}
