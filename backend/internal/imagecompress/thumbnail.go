package imagecompress

import (
	"bytes"
	"errors"
	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
)

// ErrInvalidThumbnailImage 表示损坏或超过像素边界，禁止退回浏览器绕过服务端限制。
var ErrInvalidThumbnailImage = errors.New("图片无效或超过缩略图像素边界")

// ThumbnailOptions 固定服务端缩略图边界，所有运行值从私有配置服务传入。
type ThumbnailOptions struct {
	Dimension, Quality, MaxPixels int
	MaxBytes                      int64
}

// Thumbnail 只生成显示副本，保护原文件；预检像素、首帧解码、EXIF方向和白底合成均不调用AI。
func Thumbnail(source []byte, options ThumbnailOptions) ([]byte, error) {
	// HEIC/AVIF沿用既有客户端兼容解码；普通损坏图或超限图不返回兼容格式错误。
	if len(source) >= 12 && string(source[4:8]) == "ftyp" {
		switch string(source[8:12]) {
		case "heic", "heix", "hevc", "hevx", "mif1", "msf1", "avif", "avis":
			return nil, ErrUnsupportedImage
		}
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil || !(supportedFormat(format) || format == "bmp" || format == "tiff") || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > int64(options.MaxPixels) || options.Dimension < 1 || options.Quality < 1 || options.Quality > 95 {
		return nil, ErrInvalidThumbnailImage
	}
	// 缩略图允许动画首帧，但绝不覆盖或改写动画原文件。
	decoded, _, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return nil, ErrInvalidThumbnailImage
	}
	if format == "jpeg" {
		decoded = applyJPEGOrientation(decoded, jpegExifOrientation(source))
	} else if format == "tiff" {
		decoded = applyJPEGOrientation(decoded, parseTIFFOrientation(source))
	}
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width > options.Dimension || height > options.Dimension {
		if width >= height {
			height = max(1, height*options.Dimension/width)
			width = options.Dimension
		} else {
			width = max(1, width*options.Dimension/height)
			height = options.Dimension
		}
	}
	preview := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(preview, preview.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.ApproxBiLinear.Scale(preview, preview.Bounds(), decoded, bounds, draw.Over, nil)
	var output bytes.Buffer
	if err := jpeg.Encode(&output, preview, &jpeg.Options{Quality: options.Quality}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
