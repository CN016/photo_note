package service

import (
	"encoding/json"
	"github.com/photo-nini/backend/internal/model"
)

// PortfolioPublicMetadata 是公开作品照片允许展示的拍摄参数白名单，禁止包含定位或序列号。
type PortfolioPublicMetadata struct {
	FocalLengthMM       *float64 `json:"focal_length_mm,omitempty"`
	FocalLength35mmMM   *float64 `json:"focal_length_35mm_mm,omitempty"`
	ApertureFNumber     *float64 `json:"aperture_f_number,omitempty"`
	ExposureTimeSeconds *float64 `json:"exposure_time_seconds,omitempty"`
	ISOSpeed            *float64 `json:"iso_speed,omitempty"`
	CameraMake          *string  `json:"camera_make,omitempty"`
	CameraModel         *string  `json:"camera_model,omitempty"`
	LensMake            *string  `json:"lens_make,omitempty"`
	LensModel           *string  `json:"lens_model,omitempty"`
	ImageWidth          *int     `json:"image_width,omitempty"`
	ImageHeight         *int     `json:"image_height,omitempty"`
}

// PortfolioMetadataForPublic 仅从快照提取公开字段；解析失败必须返回错误，不回退原始数据。
func PortfolioMetadataForPublic(raw json.RawMessage) (PortfolioPublicMetadata, error) {
	var source model.InspirationImageMetadata
	if err := json.Unmarshal(raw, &source); err != nil {
		return PortfolioPublicMetadata{}, err
	}
	return PortfolioPublicMetadata{FocalLengthMM: source.FocalLengthMM, FocalLength35mmMM: source.FocalLength35mmMM,
		ApertureFNumber: source.ApertureFNumber, ExposureTimeSeconds: source.ExposureTimeSeconds, ISOSpeed: source.ISOSpeed,
		CameraMake: source.CameraMake, CameraModel: source.CameraModel, LensMake: source.LensMake, LensModel: source.LensModel,
		ImageWidth: source.ImageWidth, ImageHeight: source.ImageHeight}, nil
}
