// Package imagecompress 将需要保存到灵感图中的静态图片压缩到统一大小上限。
package imagecompress

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// TargetBytes 是灵感图文件压缩后的最大目标字节数，按十进制 1 MB 计算。
	TargetBytes = 1_000_000
	// maxDecodedPixels 限制解码后的像素总数，避免小体积畸形图片展开为过大的内存对象。
	maxDecodedPixels = 40_000_000
	// maxJPEGQuality 和 minJPEGQuality 控制有损 JPEG 编码的质量搜索范围。
	maxJPEGQuality = 85
	minJPEGQuality = 35
)

var (
	// ErrUnsupportedImage 表示 storekey 内容不是可解码的 JPEG、PNG、GIF 或 WebP 图片。
	ErrUnsupportedImage = errors.New("图片格式不受支持或内容无效")
	// ErrImageDimensions 表示压缩所需解码的图片像素总量超过安全上限。
	ErrImageDimensions = errors.New("图片像素总量超过处理上限")
	// ErrAnimatedImage 表示超出目标大小的动画图片无法按静态灵感图方式无损压缩。
	ErrAnimatedImage = errors.New("暂不支持压缩动画图片")
	// ErrCompression 表示经过格式编码和尺寸缩放仍无法达到目标文件大小。
	ErrCompression = errors.New("图片压缩未能达到目标大小")
)

// Compress 在保留原 storekey 的前提下压缩图片；返回的 changed 用于决定是否覆盖存储文件。
// 不超过目标大小的有效图片保持原始字节，避免重复保存时对已有 JPEG 反复有损编码。
func Compress(source []byte) ([]byte, bool, error) {
	if len(source) == 0 {
		return nil, false, ErrUnsupportedImage
	}
	configuration, format, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil || !supportedFormat(format) || configuration.Width < 1 || configuration.Height < 1 {
		return nil, false, ErrUnsupportedImage
	}
	if len(source) <= TargetBytes {
		return source, false, nil
	}
	if uint64(configuration.Width) > maxDecodedPixels/uint64(configuration.Height) {
		return nil, false, ErrImageDimensions
	}

	decoded, err := decodeImage(source, format)
	if err != nil {
		if errors.Is(err, ErrAnimatedImage) {
			return nil, false, err
		}
		return nil, false, ErrUnsupportedImage
	}
	if format == "jpeg" {
		decoded = applyJPEGOrientation(decoded, jpegExifOrientation(source))
	}
	if hasTransparency(decoded) {
		compressed, err := compressTransparentPNG(decoded)
		return compressed, err == nil, err
	}
	compressed, err := compressOpaqueJPEG(decoded)
	return compressed, err == nil, err
}

// supportedFormat 明确限制当前转换器接受的静态栅格图像格式。
func supportedFormat(format string) bool {
	switch format {
	case "jpeg", "png", "gif", "webp":
		return true
	default:
		return false
	}
}

// decodeImage 解码首帧静态图片；需要压缩的多帧 GIF 会被拒绝以免静默丢失动画。
func decodeImage(source []byte, format string) (image.Image, error) {
	if format == "webp" && isAnimatedWebP(source) {
		return nil, ErrAnimatedImage
	}
	if format == "gif" {
		animation, err := gif.DecodeAll(bytes.NewReader(source))
		if err != nil || len(animation.Image) == 0 {
			return nil, ErrUnsupportedImage
		}
		if len(animation.Image) > 1 {
			return nil, ErrAnimatedImage
		}
		return animation.Image[0], nil
	}
	decoded, actualFormat, err := image.Decode(bytes.NewReader(source))
	if err != nil || actualFormat != format {
		return nil, ErrUnsupportedImage
	}
	return decoded, nil
}

// isAnimatedWebP 检查 WebP VP8X 扩展头的动画标志，避免有损压缩时只保留动画首帧。
func isAnimatedWebP(source []byte) bool {
	if len(source) < 12 || string(source[:4]) != "RIFF" || string(source[8:12]) != "WEBP" {
		return false
	}
	for offset := 12; offset+8 <= len(source); {
		chunkSize := uint64(binary.LittleEndian.Uint32(source[offset+4 : offset+8]))
		chunkStart := uint64(offset) + 8
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > uint64(len(source)) {
			return false
		}
		if string(source[offset:offset+4]) == "VP8X" && chunkSize >= 10 {
			const animationFlag = 1 << 1
			return source[chunkStart]&animationFlag != 0
		}
		offset = int(chunkEnd + chunkSize%2)
	}
	return false
}

// compressOpaqueJPEG 逐级降低编码质量；只有质量下限仍超标时才从原图缩小尺寸。
func compressOpaqueJPEG(source image.Image) ([]byte, error) {
	originalBounds := source.Bounds()
	width, height := originalBounds.Dx(), originalBounds.Dy()
	for width > 0 && height > 0 {
		scaled := resizeOpaque(source, width, height)
		highQuality, err := encodeJPEG(scaled, maxJPEGQuality)
		if err != nil {
			return nil, err
		}
		if len(highQuality) <= TargetBytes {
			return highQuality, nil
		}

		lowQuality, err := encodeJPEG(scaled, minJPEGQuality)
		if err != nil {
			return nil, err
		}
		if len(lowQuality) <= TargetBytes {
			// 在最低可接受质量与当前尺寸最高质量之间搜索，取满足大小限制的最高质量结果。
			best := lowQuality
			low, high := minJPEGQuality+1, maxJPEGQuality-1
			for low <= high {
				quality := (low + high) / 2
				candidate, encodeErr := encodeJPEG(scaled, quality)
				if encodeErr != nil {
					return nil, encodeErr
				}
				if len(candidate) <= TargetBytes {
					best = candidate
					low = quality + 1
				} else {
					high = quality - 1
				}
			}
			return best, nil
		}
		if width == 1 && height == 1 {
			break
		}
		width, height = reducedDimension(width), reducedDimension(height)
	}
	return nil, ErrCompression
}

// compressTransparentPNG 逐步缩小带透明通道的图片，避免转换 JPEG 时把透明区域铺成实色。
func compressTransparentPNG(source image.Image) ([]byte, error) {
	width, height := source.Bounds().Dx(), source.Bounds().Dy()
	for width > 0 && height > 0 {
		scaled := resizeTransparent(source, width, height)
		var output bytes.Buffer
		if err := png.Encode(&output, scaled); err != nil {
			return nil, err
		}
		if output.Len() <= TargetBytes {
			return output.Bytes(), nil
		}
		if width == 1 && height == 1 {
			break
		}
		width, height = reducedDimension(width), reducedDimension(height)
	}
	return nil, ErrCompression
}

// encodeJPEG 编码一次指定质量的 JPEG 候选结果。
func encodeJPEG(source image.Image, quality int) ([]byte, error) {
	var output bytes.Buffer
	if err := jpeg.Encode(&output, source, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// resizeOpaque 将任意图片缩放到不透明白色画布，透明来源像素已在调用前排除。
func resizeOpaque(source image.Image, width, height int) *image.RGBA {
	result := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(result, result.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(result, result.Bounds(), source, source.Bounds(), xdraw.Over, nil)
	return result
}

// resizeTransparent 以高质量采样缩放透明图片并保留其 Alpha 通道。
func resizeTransparent(source image.Image, width, height int) *image.NRGBA {
	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(result, result.Bounds(), source, source.Bounds(), xdraw.Src, nil)
	return result
}

// reducedDimension 每轮按 85% 缩小最长边，且确保正整数尺寸至少递减一个像素。
func reducedDimension(dimension int) int {
	reduced := dimension * 85 / 100
	if reduced >= dimension {
		return dimension - 1
	}
	if reduced < 1 {
		return 1
	}
	return reduced
}

// hasTransparency 先使用常见图像类型的 Opaque 快捷能力，未知类型再逐像素检测 Alpha。
func hasTransparency(source image.Image) bool {
	if opaque, ok := source.(interface{ Opaque() bool }); ok {
		return !opaque.Opaque()
	}
	bounds := source.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := source.At(x, y).RGBA()
			if alpha != 0xffff {
				return true
			}
		}
	}
	return false
}

// jpegExifOrientation 仅提取 JPEG EXIF Orientation 标签，其他元数据不会被复制进压缩结果。
func jpegExifOrientation(source []byte) int {
	if len(source) < 4 || source[0] != 0xff || source[1] != 0xd8 {
		return 1
	}
	for offset := 2; offset+4 <= len(source); {
		if source[offset] != 0xff {
			return 1
		}
		for offset < len(source) && source[offset] == 0xff {
			offset++
		}
		if offset >= len(source) {
			return 1
		}
		marker := source[offset]
		offset++
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if offset+2 > len(source) {
			return 1
		}
		segmentLength := int(binary.BigEndian.Uint16(source[offset : offset+2]))
		if segmentLength < 2 || offset+segmentLength > len(source) {
			return 1
		}
		segment := source[offset+2 : offset+segmentLength]
		if marker == 0xe1 && len(segment) >= 6 && bytes.Equal(segment[:6], []byte{'E', 'x', 'i', 'f', 0, 0}) {
			if orientation := parseTIFFOrientation(segment[6:]); orientation >= 1 && orientation <= 8 {
				return orientation
			}
		}
		offset += segmentLength
	}
	return 1
}

// parseTIFFOrientation 安全解析 TIFF IFD0 中的 Orientation 标签及其内联 SHORT 值。
func parseTIFFOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 1
	}
	ifdOffset := uint64(order.Uint32(tiff[4:8]))
	if ifdOffset+2 > uint64(len(tiff)) {
		return 1
	}
	entryCount := int(order.Uint16(tiff[ifdOffset : ifdOffset+2]))
	entriesStart := ifdOffset + 2
	for index := 0; index < entryCount; index++ {
		entryOffset := entriesStart + uint64(index*12)
		if entryOffset+12 > uint64(len(tiff)) {
			return 1
		}
		entry := tiff[entryOffset : entryOffset+12]
		if order.Uint16(entry[0:2]) != 0x0112 || order.Uint16(entry[2:4]) != 3 || order.Uint32(entry[4:8]) < 1 {
			continue
		}
		if orientation := int(order.Uint16(entry[8:10])); orientation >= 1 && orientation <= 8 {
			return orientation
		}
	}
	return 1
}

// applyJPEGOrientation 将 EXIF 方向变换落实到像素，压缩后移除旧元数据时仍保持视觉朝向。
func applyJPEGOrientation(source image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return source
	}
	sourceBounds := source.Bounds()
	width, height := sourceBounds.Dx(), sourceBounds.Dy()
	destinationWidth, destinationHeight := width, height
	if orientation >= 5 {
		destinationWidth, destinationHeight = height, width
	}
	destination := image.NewNRGBA(image.Rect(0, 0, destinationWidth, destinationHeight))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var destinationX, destinationY int
			switch orientation {
			case 2:
				destinationX, destinationY = width-1-x, y
			case 3:
				destinationX, destinationY = width-1-x, height-1-y
			case 4:
				destinationX, destinationY = x, height-1-y
			case 5:
				destinationX, destinationY = y, x
			case 6:
				destinationX, destinationY = height-1-y, x
			case 7:
				destinationX, destinationY = height-1-y, width-1-x
			case 8:
				destinationX, destinationY = y, width-1-x
			}
			destination.Set(destinationX, destinationY, source.At(sourceBounds.Min.X+x, sourceBounds.Min.Y+y))
		}
	}
	return destination
}
