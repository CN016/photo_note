package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
)

// InspirationStatisticsRepository 只暴露按用户范围聚合图片元数据的只读能力。
type InspirationStatisticsRepository interface {
	GetInspirationStatistics(context.Context, uuid.UUID) (model.InspirationStatistics, error)
}

// InspirationStatisticsService 将已鉴权用户的统计请求委托给数据库聚合层。
type InspirationStatisticsService struct {
	repository InspirationStatisticsRepository
}

// NewInspirationStatisticsService 创建灵感图统计服务；统计范围由调用方传入的用户业务 UUID 决定。
func NewInspirationStatisticsService(repository InspirationStatisticsRepository) *InspirationStatisticsService {
	return &InspirationStatisticsService{repository: repository}
}

// Get 返回当前用户的图片数量及拍摄属性分布，不读取或返回原始图片内容和私密位置数据。
func (service *InspirationStatisticsService) Get(ctx context.Context, userID uuid.UUID) (model.InspirationStatistics, error) {
	return service.repository.GetInspirationStatistics(ctx, userID)
}
