package imagecompress

import (
	"bytes"
	"image"
	"math"
)

// DeliveryPreview 为交付生成无元数据的 JPEG 预览；原始文件由调用方单独保存。
// target 是十进制字节上限，longEdge 是预览最大长边；先保留画质，再降低尺寸，禁止无限内存展开。
func DeliveryPreview(source []byte, target, longEdge int) ([]byte, error) {
	if target < 10000 || longEdge < 320 {
		return nil, ErrCompression
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil || !supportedFormat(format) || cfg.Width < 1 || cfg.Height < 1 {
		return nil, ErrUnsupportedImage
	}
	if uint64(cfg.Width) > maxDecodedPixels/uint64(cfg.Height) {
		return nil, ErrImageDimensions
	}
	decoded, err := decodeImage(source, format)
	if err != nil {
		return nil, err
	}
	if format == "jpeg" {
		decoded = applyJPEGOrientation(decoded, jpegExifOrientation(source))
	}
	bounds := decoded.Bounds()
	ratio := math.Min(1, float64(longEdge)/float64(max(bounds.Dx(), bounds.Dy())))
	for attempt := 0; attempt < 16; attempt++ {
		width, height := max(1, int(float64(bounds.Dx())*ratio)), max(1, int(float64(bounds.Dy())*ratio))
		scaled := resizeOpaque(decoded, width, height)
		var best []byte
		low, high := 76, 92
		for low <= high {
			quality := (low + high) / 2
			candidate, err := encodeJPEG(scaled, quality)
			if err != nil {
				return nil, err
			}
			if len(candidate) <= target {
				best = candidate
				low = quality + 1
			} else {
				high = quality - 1
			}
		}
		if best != nil {
			return best, nil
		}
		ratio *= 0.85
	}
	return nil, ErrCompression
}
