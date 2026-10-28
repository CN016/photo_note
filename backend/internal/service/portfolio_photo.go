package service

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/imagecompress"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"io"
	"strings"
	"unicode/utf8"
)

// PortfolioPhotoRepository 提供作品照片授权读写。
type PortfolioPhotoRepository interface {
	FindPortfolio(context.Context, uuid.UUID, uuid.UUID) (model.Portfolio, error)
	ListPortfolioPhotos(context.Context, uuid.UUID, uuid.UUID) ([]model.PortfolioPhoto, error)
	CreatePortfolioPhoto(context.Context, *model.PortfolioPhoto) error
}

// PortfolioPhotoStore 只提供公开副本的保存和失败回收。
type PortfolioPhotoStore interface {
	StorePublic(context.Context, string, string, io.Reader, int64) (string, error)
	Delete(context.Context, string) error
}

// PortfolioPhotoService 使用服务端读取和识别后的字节创建独立公开图片。
type PortfolioPhotoService struct {
	repository PortfolioPhotoRepository
	files      PortfolioPhotoStore
	maxBytes   int64
	storeType  func(context.Context) (string, error)
}

// NewPortfolioPhotoService 注入持久化、文件存储、上传上限和动态存储配置。
func NewPortfolioPhotoService(repository PortfolioPhotoRepository, files PortfolioPhotoStore, maxBytes int64, storeType func(context.Context) (string, error)) *PortfolioPhotoService {
	return &PortfolioPhotoService{repository: repository, files: files, maxBytes: maxBytes, storeType: storeType}
}

// PortfolioPhotoView 不返回数据库内部 ID 或用户归属。
type PortfolioPhotoView struct {
	BusinessID    uuid.UUID       `json:"business_id"`
	ImageKey      string          `json:"image_key"`
	Title         string          `json:"title"`
	TagIDs        []string        `json:"tag_ids"`
	SortOrder     int64           `json:"sort_order"`
	ImageMetadata json.RawMessage `json:"image_metadata"`
}

func portfolioPhotoView(p model.PortfolioPhoto) PortfolioPhotoView {
	return PortfolioPhotoView{BusinessID: p.BusinessID, ImageKey: p.ImageKey, Title: p.Title, TagIDs: []string(p.TagIDs), SortOrder: p.SortOrder, ImageMetadata: json.RawMessage(p.ImageMetadata)}
}

// List 返回当前作者作品集中的照片。
func (s *PortfolioPhotoService) List(ctx context.Context, user, id uuid.UUID) ([]PortfolioPhotoView, error) {
	items, err := s.repository.ListPortfolioPhotos(ctx, user, id)
	if err != nil {
		return nil, err
	}
	views := make([]PortfolioPhotoView, 0, len(items))
	for _, item := range items {
		views = append(views, portfolioPhotoView(item))
	}
	return views, nil
}

// Upload 校验图片字节并保存公开副本，元数据提取失败不会阻止照片上传。
func (s *PortfolioPhotoService) Upload(ctx context.Context, user, id uuid.UUID, name string, content []byte) (PortfolioPhotoView, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return PortfolioPhotoView{}, err
	}
	if scope.UserID != user {
		return PortfolioPhotoView{}, fault.NotFound()
	}
	if _, err = s.repository.FindPortfolio(ctx, user, id); err != nil {
		return PortfolioPhotoView{}, err
	}
	if len(content) == 0 || int64(len(content)) > s.maxBytes {
		return PortfolioPhotoView{}, fault.InvalidField("file", "文件为空或超过大小限制")
	}
	if err = imagecompress.Validate(content); err != nil {
		return PortfolioPhotoView{}, fault.InvalidField("file", "图片格式不受支持或内容无效")
	}
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > 255 {
		return PortfolioPhotoView{}, fault.InvalidField("title", "标题过长")
	}
	metadata, parseErr := ReadExifToolMetadata(ctx, content)
	if parseErr != nil {
		metadata = model.InspirationImageMetadata{}
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return PortfolioPhotoView{}, err
	}
	kind, err := s.storeType(ctx)
	if err != nil {
		return PortfolioPhotoView{}, err
	}
	key, err := s.files.StorePublic(ctx, kind, name, bytes.NewReader(content), s.maxBytes)
	if err != nil {
		return PortfolioPhotoView{}, err
	}
	item := model.PortfolioPhoto{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, PortfolioID: id, ImageKey: key, Title: name, TagIDs: pq.StringArray{}, ImageMetadata: model.JSONB(raw)}
	if err = s.repository.CreatePortfolioPhoto(ctx, &item); err != nil {
		_ = s.files.Delete(context.WithoutCancel(ctx), key)
		return PortfolioPhotoView{}, err
	}
	return portfolioPhotoView(item), nil
}
