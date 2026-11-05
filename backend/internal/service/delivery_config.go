package service

import (
	"context"
	"fmt"
)

// DeliveryConfig 是每次操作读取的私有系统配置，不允许组织覆盖。
type DeliveryConfig struct{ PreviewBytes, LongEdge, MaxPhotos, MaxTaskBytes int }

// DeliverySettings 通过统一配置服务读取限制；缺失或非法时拒绝上传，不退回隐藏常量。
func (s *RuntimeConfigService) DeliverySettings(ctx context.Context) (DeliveryConfig, error) {
	keys := []string{"delivery.preview_bytes", "delivery.preview_long_edge", "delivery.max_photos", "delivery.max_task_bytes"}
	values, err := s.reader.ReadConfigValues(ctx, keys)
	if err != nil {
		return DeliveryConfig{}, err
	}
	nums := make([]int, 4)
	for i, k := range keys {
		nums[i], err = parsePositiveConfigInt(k, values[k], 1<<40)
		if err != nil {
			return DeliveryConfig{}, err
		}
	}
	if nums[0] < 10000 || nums[0] > 1000000 || nums[1] < 320 || nums[1] > 4096 || nums[2] > 10000 {
		return DeliveryConfig{}, fmt.Errorf("交付压缩或容量配置超出安全范围")
	}
	return DeliveryConfig{nums[0], nums[1], nums[2], nums[3]}, nil
}

// DeliveryExportRetention 从统一配置读取临时打包文件保留小时数，清理不涉及原始照片。
func (s *RuntimeConfigService) DeliveryExportRetention(ctx context.Context) (int, error) {
	values, err := s.reader.ReadConfigValues(ctx, []string{"delivery.export_retention_hours"})
	if err != nil {
		return 0, err
	}
	return parsePositiveConfigInt("delivery.export_retention_hours", values["delivery.export_retention_hours"], 720)
}

// DeliveryPublicRate 从统一配置读取单进程每 IP 每分钟公开请求上限。
func (s *RuntimeConfigService) DeliveryPublicRate(ctx context.Context) (int, error) {
	values, err := s.reader.ReadConfigValues(ctx, []string{"delivery.public_requests_per_minute"})
	if err != nil {
		return 0, err
	}
	return parsePositiveConfigInt("delivery.public_requests_per_minute", values["delivery.public_requests_per_minute"], 3600)
}
