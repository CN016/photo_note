package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/model"
)

const (
	// 额外元数据上限避免少见的大型 XMP 结构占用过多数据库空间。
	maxImageMetadataExtraBytes = 1 << 20
	maxExifToolOutputBytes     = 8 << 20
)

// ParseExifToolMetadata 将 ExifTool 分组 JSON 映射到固定属性列，并保留无法映射的命名空间标签。
func ParseExifToolMetadata(output []byte) (model.InspirationImageMetadata, error) {
	var records []map[string]json.RawMessage
	if err := json.Unmarshal(output, &records); err != nil {
		return model.InspirationImageMetadata{}, err
	}
	if len(records) == 0 {
		return model.InspirationImageMetadata{}, errors.New("ExifTool 未返回图像属性")
	}
	tags := records[0]
	metadata := model.InspirationImageMetadata{}
	metadata.ImageID = metadataString(tags, "ImageUniqueID", "ImageID")
	metadata.ImageWidth = metadataInteger(tags, "ImageWidth", "ExifImageWidth", "PixelXDimension", "ImageWidthPixels")
	metadata.ImageHeight = metadataInteger(tags, "ImageHeight", "ExifImageHeight", "PixelYDimension", "ImageHeightPixels")
	metadata.HorizontalResolutionDPI, metadata.VerticalResolutionDPI = metadataResolution(tags)
	metadata.BitDepth = metadataBitDepth(tags)
	metadata.ColorSpace = metadataColorSpace(tags)
	metadata.Compression = metadataEnumString(tags, []string{"Compression", "CompressionType"}, map[int]string{
		1: "Uncompressed", 5: "LZW", 6: "JPEG", 7: "JPEG", 8: "Deflate", 32773: "PackBits",
	})
	metadata.Title = metadataString(tags, "Title", "ObjectName", "DocumentName")
	metadata.Subject = metadataString(tags, "Subject", "Caption-Abstract", "Description")
	metadata.Rating = metadataInteger(tags, "Rating", "RatingPercent")
	metadata.Tags = pq.StringArray(metadataStrings(tags, "Keywords", "HierarchicalSubject", "SubjectCode"))
	metadata.Author = metadataString(tags, "Artist", "Creator", "By-line", "Author")
	metadata.Comments = metadataString(tags, "UserComment", "Comment")
	metadata.Copyright = metadataString(tags, "Copyright", "CopyrightNotice")
	metadata.Software = metadataString(tags, "Software", "ProgramName", "ProcessingSoftware")
	metadata.DateAcquired = metadataString(tags, "DateAcquired")
	metadata.DateTimeOriginal = metadataString(tags, "DateTimeOriginal", "DateCreated")
	metadata.DateTimeDigitized = metadataString(tags, "CreateDate", "DateTimeDigitized", "DateTimeCreated")
	metadata.CameraMake = metadataString(tags, "Make", "CameraMake")
	metadata.CameraModel = metadataString(tags, "Model", "CameraModel")
	metadata.CameraSerialNumber = metadataString(tags, "SerialNumber", "BodySerialNumber", "CameraSerialNumber")
	metadata.LensMake = metadataString(tags, "LensMake", "LensManufacturer")
	metadata.LensModel = metadataString(tags, "LensModel", "LensID")
	metadata.LensSerialNumber = metadataString(tags, "LensSerialNumber")
	metadata.FlashMake = metadataString(tags, "FlashMake", "FlashManufacturer")
	metadata.FlashModel = metadataString(tags, "FlashModel")
	metadata.FlashSerialNumber = metadataString(tags, "FlashSerialNumber")
	metadata.ApertureFNumber = metadataAperture(tags, "FNumber", "ApertureValue")
	metadata.MaximumApertureFNumber = metadataAperture(tags, "MaximumApertureFNumber", "MaxApertureValue")
	metadata.ExposureTimeSeconds = metadataExposureSeconds(tags)
	metadata.ISOSpeed = metadataNumber(tags, "ISO", "ISOSpeedRatings", "PhotographicSensitivity")
	metadata.ExposureCompensationEV = metadataNumber(tags, "ExposureCompensation", "ExposureBiasValue")
	metadata.ExposureProgram = metadataEnumString(tags, []string{"ExposureProgram"}, map[int]string{
		0: "Not defined", 1: "Manual", 2: "Program AE", 3: "Aperture priority", 4: "Shutter priority",
		5: "Creative program", 6: "Action program", 7: "Portrait mode", 8: "Landscape mode",
	})
	metadata.FlashFired, metadata.FlashMode = metadataFlash(tags)
	metadata.FlashEnergyBCPS = metadataNumber(tags, "FlashEnergy")
	metadata.FocalLengthMM = metadataNumber(tags, "FocalLength")
	metadata.FocalLength35mmMM = metadataNumber(tags, "FocalLengthIn35mmFormat", "FocalLength35efl")
	metadata.SubjectDistanceM = metadataNumber(tags, "SubjectDistance")
	metadata.MeteringMode = metadataEnumString(tags, []string{"MeteringMode"}, map[int]string{
		0: "Unknown", 1: "Average", 2: "Center-weighted average", 3: "Spot", 4: "Multi-spot", 5: "Pattern", 6: "Partial", 255: "Other",
	})
	metadata.LightSource = metadataEnumString(tags, []string{"LightSource"}, map[int]string{
		0: "Unknown", 1: "Daylight", 2: "Fluorescent", 3: "Tungsten", 4: "Flash", 9: "Fine weather", 10: "Cloudy", 11: "Shade", 255: "Other",
	})
	metadata.WhiteBalance = metadataEnumString(tags, []string{"WhiteBalance"}, map[int]string{0: "Auto", 1: "Manual"})
	metadata.Contrast = metadataEnumString(tags, []string{"Contrast"}, map[int]string{0: "Normal", 1: "Low", 2: "High"})
	metadata.BrightnessEV = metadataNumber(tags, "BrightnessValue")
	metadata.Saturation = metadataEnumString(tags, []string{"Saturation"}, map[int]string{0: "Normal", 1: "Low", 2: "High"})
	metadata.Sharpness = metadataEnumString(tags, []string{"Sharpness"}, map[int]string{0: "Normal", 1: "Soft", 2: "Hard"})
	metadata.DigitalZoomRatio = metadataNumber(tags, "DigitalZoomRatio")
	metadata.CompressionBitsPerPixel = metadataNumber(tags, "CompressedBitsPerPixel")
	metadata.EXIFVersion = metadataString(tags, "ExifVersion", "EXIFVersion")
	metadata.GPSLatitude = metadataNumber(tags, "GPSLatitude")
	metadata.GPSLongitude = metadataNumber(tags, "GPSLongitude")
	metadata.GPSAltitudeM = metadataNumber(tags, "GPSAltitude")
	if date := metadataString(tags, "GPSDateStamp"); date != nil {
		if clock := metadataString(tags, "GPSTimeStamp"); clock != nil {
			joined := strings.TrimSpace(*date + " " + *clock)
			metadata.GPSTimestamp = &joined
		} else {
			metadata.GPSTimestamp = date
		}
	} else {
		metadata.GPSTimestamp = metadataString(tags, "GPSDateTime")
	}
	metadata.Extra = extraMetadata(tags, knownExifTags)
	return metadata, nil
}

// ReadExifToolMetadata 通过固定参数和标准输入调用 ExifTool，不拼接 shell 命令或临时文件路径。
func ReadExifToolMetadata(ctx context.Context, content []byte) (model.InspirationImageMetadata, error) {
	command := exec.CommandContext(ctx, "exiftool", "-json", "-G4", "-s", "-n", "-")
	command.Stdin = bytes.NewReader(content)
	var output limitedMetadataBuffer
	output.limit = maxExifToolOutputBytes
	command.Stdout = &output
	if err := command.Run(); err != nil {
		return model.InspirationImageMetadata{}, err
	}
	if output.overflow {
		return model.InspirationImageMetadata{}, errors.New("ExifTool 输出超过读取上限")
	}
	return ParseExifToolMetadata(output.Bytes())
}

// MergeInspirationImageMetadata 用服务端原图值覆盖同名客户端值；服务端缺失项保留浏览器补充值。
func MergeInspirationImageMetadata(client, server model.InspirationImageMetadata) model.InspirationImageMetadata {
	merged := model.InspirationImageMetadata{
		ImageID:                 chooseMetadata(client.ImageID, server.ImageID),
		SourceFileSize:          chooseMetadata(client.SourceFileSize, server.SourceFileSize),
		SourceLastModifiedAt:    chooseMetadata(client.SourceLastModifiedAt, server.SourceLastModifiedAt),
		ImageWidth:              chooseMetadata(client.ImageWidth, server.ImageWidth),
		ImageHeight:             chooseMetadata(client.ImageHeight, server.ImageHeight),
		HorizontalResolutionDPI: chooseMetadata(client.HorizontalResolutionDPI, server.HorizontalResolutionDPI),
		VerticalResolutionDPI:   chooseMetadata(client.VerticalResolutionDPI, server.VerticalResolutionDPI),
		BitDepth:                chooseMetadata(client.BitDepth, server.BitDepth),
		ColorSpace:              chooseMetadata(client.ColorSpace, server.ColorSpace),
		Compression:             chooseMetadata(client.Compression, server.Compression),
		Title:                   chooseMetadata(client.Title, server.Title),
		Subject:                 chooseMetadata(client.Subject, server.Subject),
		Rating:                  chooseMetadata(client.Rating, server.Rating),
		Author:                  chooseMetadata(client.Author, server.Author),
		Comments:                chooseMetadata(client.Comments, server.Comments),
		Copyright:               chooseMetadata(client.Copyright, server.Copyright),
		Software:                chooseMetadata(client.Software, server.Software),
		DateAcquired:            chooseMetadata(client.DateAcquired, server.DateAcquired),
		DateTimeOriginal:        chooseMetadata(client.DateTimeOriginal, server.DateTimeOriginal),
		DateTimeDigitized:       chooseMetadata(client.DateTimeDigitized, server.DateTimeDigitized),
		CameraMake:              chooseMetadata(client.CameraMake, server.CameraMake),
		CameraModel:             chooseMetadata(client.CameraModel, server.CameraModel),
		CameraSerialNumber:      chooseMetadata(client.CameraSerialNumber, server.CameraSerialNumber),
		LensMake:                chooseMetadata(client.LensMake, server.LensMake),
		LensModel:               chooseMetadata(client.LensModel, server.LensModel),
		LensSerialNumber:        chooseMetadata(client.LensSerialNumber, server.LensSerialNumber),
		FlashMake:               chooseMetadata(client.FlashMake, server.FlashMake),
		FlashModel:              chooseMetadata(client.FlashModel, server.FlashModel),
		FlashSerialNumber:       chooseMetadata(client.FlashSerialNumber, server.FlashSerialNumber),
		ApertureFNumber:         chooseMetadata(client.ApertureFNumber, server.ApertureFNumber),
		MaximumApertureFNumber:  chooseMetadata(client.MaximumApertureFNumber, server.MaximumApertureFNumber),
		ExposureTimeSeconds:     chooseMetadata(client.ExposureTimeSeconds, server.ExposureTimeSeconds),
		ISOSpeed:                chooseMetadata(client.ISOSpeed, server.ISOSpeed),
		ExposureCompensationEV:  chooseMetadata(client.ExposureCompensationEV, server.ExposureCompensationEV),
		ExposureProgram:         chooseMetadata(client.ExposureProgram, server.ExposureProgram),
		FlashFired:              chooseMetadata(client.FlashFired, server.FlashFired),
		FlashMode:               chooseMetadata(client.FlashMode, server.FlashMode),
		FlashEnergyBCPS:         chooseMetadata(client.FlashEnergyBCPS, server.FlashEnergyBCPS),
		FocalLengthMM:           chooseMetadata(client.FocalLengthMM, server.FocalLengthMM),
		FocalLength35mmMM:       chooseMetadata(client.FocalLength35mmMM, server.FocalLength35mmMM),
		SubjectDistanceM:        chooseMetadata(client.SubjectDistanceM, server.SubjectDistanceM),
		MeteringMode:            chooseMetadata(client.MeteringMode, server.MeteringMode),
		LightSource:             chooseMetadata(client.LightSource, server.LightSource),
		WhiteBalance:            chooseMetadata(client.WhiteBalance, server.WhiteBalance),
		Contrast:                chooseMetadata(client.Contrast, server.Contrast),
		BrightnessEV:            chooseMetadata(client.BrightnessEV, server.BrightnessEV),
		Saturation:              chooseMetadata(client.Saturation, server.Saturation),
		Sharpness:               chooseMetadata(client.Sharpness, server.Sharpness),
		DigitalZoomRatio:        chooseMetadata(client.DigitalZoomRatio, server.DigitalZoomRatio),
		CompressionBitsPerPixel: chooseMetadata(client.CompressionBitsPerPixel, server.CompressionBitsPerPixel),
		EXIFVersion:             chooseMetadata(client.EXIFVersion, server.EXIFVersion),
		GPSLatitude:             chooseMetadata(client.GPSLatitude, server.GPSLatitude),
		GPSLongitude:            chooseMetadata(client.GPSLongitude, server.GPSLongitude),
		GPSAltitudeM:            chooseMetadata(client.GPSAltitudeM, server.GPSAltitudeM),
		GPSTimestamp:            chooseMetadata(client.GPSTimestamp, server.GPSTimestamp),
	}
	merged.Tags = client.Tags
	if server.Tags != nil {
		merged.Tags = server.Tags
	}
	merged.Extra = mergeMetadataExtra(client.Extra, server.Extra)
	return merged
}

// NormalizeImageMetadata 限制客户端扩展属性体积并将空扩展对象规范为 JSON 对象。
func NormalizeImageMetadata(metadata model.InspirationImageMetadata) model.InspirationImageMetadata {
	metadata.Extra = normalizeMetadataExtra(metadata.Extra)
	return metadata
}

var knownExifTags = map[string]struct{}{}

// init 登记可映射的 ExifTool 标签名，避免重复保存为扩展属性。
func init() {
	for _, name := range []string{
		"SourceFile", "FileName", "Directory", "FileSize", "ExifToolVersion", "ImageUniqueID", "ImageID",
		"ImageWidth", "ExifImageWidth", "PixelXDimension", "ImageWidthPixels", "ImageHeight", "ExifImageHeight", "PixelYDimension", "ImageHeightPixels",
		"XResolution", "YResolution", "ResolutionUnit", "BitsPerSample", "BitDepth", "BitDepthPerSample", "SamplesPerPixel", "ColorSpace", "ColorModel", "ColorType", "ProfileDescription", "Compression", "CompressionType",
		"Title", "ObjectName", "DocumentName", "Subject", "Caption-Abstract", "Description", "Rating", "RatingPercent", "Keywords", "HierarchicalSubject", "SubjectCode",
		"Artist", "Creator", "By-line", "Author", "UserComment", "Comment", "Copyright", "CopyrightNotice", "Software", "ProgramName", "ProcessingSoftware",
		"DateAcquired", "DateTimeOriginal", "DateCreated", "CreateDate", "DateTimeDigitized", "DateTimeCreated", "Make", "CameraMake", "Model", "CameraModel", "SerialNumber", "BodySerialNumber", "CameraSerialNumber", "LensMake", "LensManufacturer", "LensModel", "LensID", "LensSerialNumber", "FlashMake", "FlashManufacturer", "FlashModel", "FlashSerialNumber",
		"FNumber", "ApertureValue", "MaxApertureValue", "MaximumApertureValue", "ExposureTime", "ShutterSpeedValue", "ISO", "ISOSpeedRatings", "PhotographicSensitivity", "ExposureCompensation", "ExposureBiasValue", "ExposureProgram", "Flash", "FlashEnergy", "FocalLength", "FocalLengthIn35mmFormat", "FocalLength35efl", "SubjectDistance", "MeteringMode", "LightSource", "WhiteBalance", "Contrast", "BrightnessValue", "Saturation", "Sharpness", "DigitalZoomRatio", "CompressedBitsPerPixel", "ExifVersion", "EXIFVersion",
		"GPSLatitude", "GPSLongitude", "GPSAltitude", "GPSDateStamp", "GPSTimeStamp", "GPSDateTime",
	} {
		knownExifTags[name] = struct{}{}
	}
}

// chooseMetadata 按优先级选取一个属性：服务端解析值优先，缺失时保留客户端值。
func chooseMetadata[T any](client, server *T) *T {
	if server != nil {
		return server
	}
	return client
}

// metadataKeys 按候选标签名查找分组 JSON 键，并从重复分组中稳定选择排序靠前的一项。
func metadataKeys(tags map[string]json.RawMessage, aliases ...string) []string {
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, alias := range aliases {
		for _, key := range keys {
			if isLocalFileMetadataKey(key) {
				continue
			}
			name := key
			if separator := strings.LastIndexByte(name, ':'); separator >= 0 {
				name = name[separator+1:]
			}
			if strings.EqualFold(name, alias) {
				return []string{key}
			}
		}
	}
	return nil
}

// isLocalFileMetadataKey 过滤 ExifTool 文件系统分组和创建、访问、修改时间，避免保存设备本机属性。
func isLocalFileMetadataKey(key string) bool {
	separator := strings.IndexByte(key, ':')
	if separator >= 0 && strings.EqualFold(key[:separator], "File") {
		return true
	}
	name := key
	if separator = strings.LastIndexByte(name, ':'); separator >= 0 {
		name = name[separator+1:]
	}
	switch strings.ToLower(name) {
	case "filecreatedate", "fileaccessdate", "filemodifydate", "filecreationdate", "filelastaccessdate", "filelastmodifieddate", "fileinodechangedate", "filepermissions", "fileattributes", "filedevicenumber", "fileinodenumber", "filehardlinks":
		return true
	default:
		return false
	}
}

// metadataString 将字符串、数值或数组属性规范成可保存的文本；缺失和空值返回 nil。
func metadataString(tags map[string]json.RawMessage, aliases ...string) *string {
	keys := metadataKeys(tags, aliases...)
	if len(keys) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(tags[keys[0]], &value); err != nil || value == nil {
		return nil
	}
	var result string
	switch typed := value.(type) {
	case string:
		result = strings.TrimSpace(typed)
	case float64:
		result = strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		result = strconv.FormatBool(typed)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return nil
		}
		result = string(encoded)
	}
	if result == "" || strings.EqualFold(result, "null") {
		return nil
	}
	return &result
}

// metadataEnumString 将 ExifTool 的整数枚举转换成稳定名称；未知码保留原始数值文本。
func metadataEnumString(tags map[string]json.RawMessage, aliases []string, values map[int]string) *string {
	value := metadataString(tags, aliases...)
	if value == nil {
		return nil
	}
	code, err := strconv.Atoi(*value)
	if err != nil {
		return value
	}
	if mapped, exists := values[code]; exists {
		return &mapped
	}
	return value
}

// metadataColorSpace 把 EXIF 常见色彩空间编号转换为显示名称，其他标签原样保存。
func metadataColorSpace(tags map[string]json.RawMessage) *string {
	value := metadataString(tags, "ColorSpace", "ProfileDescription", "ColorModel")
	if value == nil {
		colorType := metadataInteger(tags, "ColorType")
		if colorType == nil {
			return nil
		}
		mapped, exists := map[int]string{0: "grayscale", 2: "RGB", 3: "indexed color", 4: "grayscale with alpha", 6: "RGB with alpha"}[*colorType]
		if !exists {
			return nil
		}
		return &mapped
	}
	if strings.Contains(strings.ToLower(*value), "srgb") {
		mapped := "sRGB"
		return &mapped
	}
	switch *value {
	case "1":
		mapped := "sRGB"
		return &mapped
	case "65535":
		mapped := "Uncalibrated"
		return &mapped
	default:
		return value
	}
}

// metadataNumber 读取有限的数值属性；不能安全转换的值以 nil 表示，不生成 NaN 或无穷值。
func metadataNumber(tags map[string]json.RawMessage, aliases ...string) *float64 {
	keys := metadataKeys(tags, aliases...)
	if len(keys) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(tags[keys[0]], &value); err != nil || value == nil {
		return nil
	}
	var result float64
	switch typed := value.(type) {
	case float64:
		result = typed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return nil
		}
		result = parsed
	default:
		return nil
	}
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return nil
	}
	return &result
}

// metadataInteger 仅接受可由本机 int 表示的数值，并对分数执行四舍五入。
func metadataInteger(tags map[string]json.RawMessage, aliases ...string) *int {
	value := metadataNumber(tags, aliases...)
	if value == nil || *value < math.MinInt || *value > math.MaxInt {
		return nil
	}
	result := int(math.Round(*value))
	return &result
}

// metadataStrings 将 EXIF/IPTC 关键词列表统一成非空字符串切片。
func metadataStrings(tags map[string]json.RawMessage, aliases ...string) []string {
	keys := metadataKeys(tags, aliases...)
	if len(keys) == 0 {
		return nil
	}
	var values []any
	if err := json.Unmarshal(tags[keys[0]], &values); err == nil {
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, strings.TrimSpace(text))
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	if value := metadataString(tags, aliases...); value != nil {
		return []string{*value}
	}
	return nil
}

// metadataResolution 仅在单位可确认时返回 DPI；厘米值乘以 2.54 转为英寸基准。
func metadataResolution(tags map[string]json.RawMessage) (*float64, *float64) {
	x := metadataNumber(tags, "XResolution")
	y := metadataNumber(tags, "YResolution")
	unitKeys := metadataKeys(tags, "ResolutionUnit")
	if len(unitKeys) == 0 {
		return nil, nil
	}
	unit := metadataInteger(tags, "ResolutionUnit")
	if unit == nil {
		return nil, nil
	}
	group := strings.SplitN(unitKeys[0], ":", 2)[0]
	centimeters := (*unit == 3 && !strings.EqualFold(group, "JFIF")) || (*unit == 2 && strings.EqualFold(group, "JFIF"))
	inches := (*unit == 2 && !strings.EqualFold(group, "JFIF")) || (*unit == 1 && strings.EqualFold(group, "JFIF"))
	if !centimeters && !inches {
		return nil, nil
	}
	if centimeters {
		if x != nil {
			value := *x * 2.54
			x = &value
		}
		if y != nil {
			value := *y * 2.54
			y = &value
		}
	}
	return x, y
}

// metadataAperture 优先读取 F 数；仅有 APEX 光圈值时换算为 F 数后返回。
func metadataAperture(tags map[string]json.RawMessage, fNumberTag, apexTag string) *float64 {
	if value := metadataNumber(tags, fNumberTag); value != nil {
		return value
	}
	// EXIF 的 ApertureValue 以 APEX 表示；换算成 F 数后才与 Windows 显示单位一致。
	if apex := metadataNumber(tags, apexTag); apex != nil {
		value := math.Pow(2, *apex/2)
		if !math.IsInf(value, 0) && !math.IsNaN(value) {
			return &value
		}
	}
	return nil
}

// metadataExposureSeconds 优先读取秒值；仅有 APEX 快门值时换算成秒。
func metadataExposureSeconds(tags map[string]json.RawMessage) *float64 {
	if value := metadataNumber(tags, "ExposureTime"); value != nil {
		return value
	}
	// EXIF 的 ShutterSpeedValue 以 APEX 表示，曝光时间（秒）为 2 的负指数次方。
	if apex := metadataNumber(tags, "ShutterSpeedValue"); apex != nil {
		value := math.Pow(2, -*apex)
		if !math.IsInf(value, 0) && !math.IsNaN(value) {
			return &value
		}
	}
	return nil
}

// metadataBitDepth 多通道位深求和，并在来源仅提供单通道位深时结合像素样本数计算总位深。
func metadataBitDepth(tags map[string]json.RawMessage) *int {
	keys := metadataKeys(tags, "BitsPerSample", "BitDepth", "BitDepthPerSample")
	if len(keys) == 0 {
		return nil
	}
	var values []float64
	if json.Unmarshal(tags[keys[0]], &values) == nil && len(values) > 0 {
		total := 0
		for _, value := range values {
			total += int(math.Round(value))
		}
		if len(values) > 1 {
			return &total
		}
		return metadataBitDepthWithSamples(tags, total)
	}
	value := metadataInteger(tags, "BitsPerSample", "BitDepth", "BitDepthPerSample")
	if value != nil {
		return metadataBitDepthWithSamples(tags, *value)
	}
	return value
}

// metadataBitDepthWithSamples 将单通道深度转换为每像素总位数；PNG 颜色类型可在缺少样本数时推导通道数。
func metadataBitDepthWithSamples(tags map[string]json.RawMessage, channelDepth int) *int {
	samples := metadataInteger(tags, "SamplesPerPixel")
	if samples == nil {
		if colorType := metadataInteger(tags, "ColorType"); colorType != nil {
			count, exists := map[int]int{0: 1, 2: 3, 3: 1, 4: 2, 6: 4}[*colorType]
			if exists {
				samples = &count
			}
		}
	}
	if samples != nil && *samples > 1 {
		total := channelDepth * *samples
		return &total
	}
	return &channelDepth
}

// metadataFlash 从 EXIF 闪光灯位标志解析是否触发、工作模式和红眼修正状态。
func metadataFlash(tags map[string]json.RawMessage) (*bool, *string) {
	keys := metadataKeys(tags, "Flash")
	if len(keys) == 0 {
		return nil, nil
	}
	raw := tags[keys[0]]
	var number float64
	if json.Unmarshal(raw, &number) == nil {
		fired := int(number)&1 == 1
		flags := int(math.Round(number))
		mode := "unspecified"
		switch flags & 0x18 {
		case 0x08:
			mode = "compulsory firing"
		case 0x10:
			mode = "compulsory suppression"
		case 0x18:
			mode = "auto mode"
		}
		if fired {
			mode += ", fired"
		} else {
			mode += ", did not fire"
		}
		if flags&0x40 != 0 {
			mode += ", red-eye reduction"
		}
		return &fired, &mode
	}
	value := metadataString(tags, "Flash")
	if value == nil {
		return nil, nil
	}
	lower := strings.ToLower(*value)
	fired := strings.Contains(lower, "fired")
	return &fired, value
}

// extraMetadata 仅保留未映射且带命名空间的标签，并在超过大小上限时整体省略扩展值。
func extraMetadata(tags map[string]json.RawMessage, known map[string]struct{}) model.JSONB {
	extra := make(map[string]json.RawMessage)
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := tags[key]
		name := key
		if separator := strings.LastIndexByte(name, ':'); separator >= 0 {
			name = name[separator+1:]
		}
		if _, exists := known[name]; exists || isLocalFileMetadataKey(key) || name == "SourceFile" || len(value) == 0 || !json.Valid(value) {
			continue
		}
		if separator := strings.IndexByte(key, ':'); separator == 0 {
			key = "ExifTool:" + name
		} else if separator < 0 {
			key = "ExifTool:" + key
		}
		extra[key] = append(json.RawMessage(nil), value...)
		encoded, err := json.Marshal(extra)
		if err != nil || len(encoded) > maxImageMetadataExtraBytes {
			delete(extra, key)
		}
	}
	encoded, err := json.Marshal(extra)
	if err != nil || len(encoded) > maxImageMetadataExtraBytes {
		return model.JSONB(`{}`)
	}
	if len(extra) == 0 {
		return model.JSONB(`{}`)
	}
	return model.JSONB(encoded)
}

// mergeMetadataExtra 合并两端扩展标签，服务端同名标签覆盖客户端，并统一执行大小上限。
func mergeMetadataExtra(client, server model.JSONB) model.JSONB {
	values := make(map[string]json.RawMessage)
	for _, source := range []model.JSONB{client, server} {
		var parsed map[string]json.RawMessage
		if json.Unmarshal(source, &parsed) != nil {
			continue
		}
		for key, value := range parsed {
			values[key] = value
		}
	}
	encoded, err := json.Marshal(values)
	if err != nil || len(encoded) > maxImageMetadataExtraBytes {
		return model.JSONB(`{}`)
	}
	if len(values) == 0 {
		return model.JSONB(`{}`)
	}
	return model.JSONB(encoded)
}

// normalizeMetadataExtra 过滤无命名空间或无效 JSON 标签，并按稳定顺序保留 1 MiB 内的属性。
func normalizeMetadataExtra(extra model.JSONB) model.JSONB {
	var values map[string]json.RawMessage
	if json.Unmarshal(extra, &values) != nil || values == nil {
		return model.JSONB(`{}`)
	}
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if strings.Contains(key, ":") && len(value) > 0 && json.Valid(value) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	result := make(map[string]json.RawMessage)
	for _, key := range keys {
		result[key] = values[key]
		encoded, err := json.Marshal(result)
		if err != nil || len(encoded) > maxImageMetadataExtraBytes {
			delete(result, key)
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) == 0 {
		return model.JSONB(`{}`)
	}
	return model.JSONB(encoded)
}

// limitedMetadataBuffer 将 ExifTool 的标准输出限制在固定内存上限内。
type limitedMetadataBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

// Write 在 ExifTool 子进程输出超过限制时截断缓存但继续消费管道，避免无限占用进程内存。
func (buffer *limitedMetadataBuffer) Write(value []byte) (int, error) {
	remaining := buffer.limit - buffer.Len()
	if remaining <= 0 {
		buffer.overflow = true
		return len(value), nil
	}
	if len(value) > remaining {
		buffer.overflow = true
		_, _ = buffer.Buffer.Write(value[:remaining])
		return len(value), nil
	}
	return buffer.Buffer.Write(value)
}
