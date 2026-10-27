package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/imagecompress"
)

// ThumbnailConfig 为私有缩略图配置；并发只在启动装配时读取，其余参数按请求刷新。
type ThumbnailConfig struct {
	Dimension int   `json:"dimension"`
	Quality   int   `json:"quality"`
	MaxPixels int   `json:"max_pixels"`
	Workers   int   `json:"workers"`
	MaxBytes  int64 `json:"-"`
}

// Thumbnails 经统一配置读取并验证缩略图参数，不直接查询配置表。
func (s *RuntimeConfigService) Thumbnails(ctx context.Context) (ThumbnailConfig, error) {
	var c ThumbnailConfig
	values, err := s.reader.ReadConfigValues(ctx, []string{"files.thumbnails", "files.max_upload_bytes"})
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal([]byte(values["files.thumbnails"]), &c); err != nil {
		return c, errors.New("缩略图配置格式无效")
	}
	c.MaxBytes, err = strconv.ParseInt(values["files.max_upload_bytes"], 10, 64)
	if err != nil || c.MaxBytes < 1 || c.Dimension < 64 || c.Dimension > 1024 || c.Quality < 30 || c.Quality > 95 || c.MaxPixels < 1 || c.MaxPixels > 40000000 || c.Workers < 1 || c.Workers > 2 {
		return c, errors.New("缩略图配置范围无效")
	}
	return c, nil
}

// ConfigureThumbnails 在启动时注入配置和并发槽，不启动AI或扫描图库。
func (s *FileService) ConfigureThumbnails(ctx context.Context, config *RuntimeConfigService) error {
	c, err := config.Thumbnails(ctx)
	if err != nil {
		return err
	}
	s.thumbnailConfig = config.Thumbnails
	s.thumbnailSlots = make(chan struct{}, c.Workers)
	return nil
}

// OpenThumbnail 每次先校验当前空间与创建者；命中磁盘缓存也不会绕过鉴权和原文件存在性检查。
func (s *FileService) OpenThumbnail(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	parsed, err := filestore.ParseKey(key)
	if err != nil || parsed.Public {
		return nil, 0, filestore.ErrInvalidStoreKey
	}
	if err = s.AuthorizePrivateFile(ctx, key); err != nil {
		return nil, 0, err
	}
	if s.thumbnailConfig == nil {
		return nil, 0, fault.Unavailable(errors.New("缩略图未配置"))
	}
	c, err := s.thumbnailConfig(ctx)
	if err != nil {
		return nil, 0, err
	}
	backend, ok := s.backend.(interface {
		OpenThumbnail(context.Context, string, imagecompress.ThumbnailOptions) (io.ReadCloser, int64, error)
	})
	if !ok {
		return nil, 0, filestore.ErrBackendUnavailable
	}
	select {
	case s.thumbnailSlots <- struct{}{}:
		defer func() { <-s.thumbnailSlots }()
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
	unlock := s.thumbnailLocks.acquire(key)
	defer unlock()
	return backend.OpenThumbnail(ctx, key, imagecompress.ThumbnailOptions{Dimension: c.Dimension, Quality: c.Quality, MaxPixels: c.MaxPixels, MaxBytes: c.MaxBytes})
}

// WarmThumbnail 随新增/换图的压缩任务预生成副本；失败不破坏已保存图片，也不触发识别。
func (s *FileService) WarmThumbnail(ctx context.Context, key string) error {
	if s.thumbnailConfig == nil {
		return nil
	}
	reader, _, err := s.OpenThumbnail(ctx, key)
	if reader != nil {
		reader.Close()
	}
	return err
}
