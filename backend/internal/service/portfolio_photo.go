package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
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
	return s.upload(ctx, user, id, name, content, nil)
}

// upload 在独立公开副本中保留来源属性快照。
func (s *PortfolioPhotoService) upload(ctx context.Context, user, id uuid.UUID, name string, content []byte, snapshot *model.InspirationImageMetadata) (PortfolioPhotoView, error) {
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
	metadata := model.InspirationImageMetadata{}
	if snapshot != nil {
		metadata = *snapshot
	} else if parsed, parseErr := ReadExifToolMetadata(ctx, content); parseErr == nil {
		metadata = parsed
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

// PortfolioPhotoDeletionRepository 在持有照片行锁时完成文件清理和记录删除。
type PortfolioPhotoDeletionRepository interface {
	DeletePortfolioPhoto(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, func(string) error) error
}

// Delete 保留删除失败的记录，已不存在的文件视为清理成功以支持事务失败后的重试。
func (s *PortfolioPhotoService) Delete(ctx context.Context, user, portfolioID, photoID uuid.UUID) error {
	repo, ok := s.repository.(PortfolioPhotoDeletionRepository)
	if !ok {
		return fault.NotFound()
	}
	return repo.DeletePortfolioPhoto(ctx, user, portfolioID, photoID, func(key string) error {
		err := s.files.Delete(ctx, key)
		if errors.Is(err, filestore.ErrFileNotFound) {
			return nil
		}
		return err
	})
}

// PortfolioPhotoInput 为照片标题、标签和排序提供完整更新。
type PortfolioPhotoInput struct {
	Title     string      `json:"title"`
	TagIDs    []uuid.UUID `json:"tag_ids"`
	SortOrder int64       `json:"sort_order"`
}
type portfolioPhotoUpdater interface {
	UpdatePortfolioPhoto(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, []uuid.UUID, int64) (model.PortfolioPhoto, error)
}

// Update 校验可写字段并在存储层核验所有标签归属。
func (s *PortfolioPhotoService) Update(ctx context.Context, user, portfolioID, photoID uuid.UUID, input PortfolioPhotoInput) (PortfolioPhotoView, error) {
	input.Title = strings.TrimSpace(input.Title)
	if utf8.RuneCountInString(input.Title) > 255 {
		return PortfolioPhotoView{}, fault.InvalidField("title", "标题过长")
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range input.TagIDs {
		if id == uuid.Nil || seen[id] {
			return PortfolioPhotoView{}, fault.InvalidField("tag_ids", "标签无效或重复")
		}
		seen[id] = true
	}
	repo, ok := s.repository.(portfolioPhotoUpdater)
	if !ok {
		return PortfolioPhotoView{}, fault.NotFound()
	}
	photo, err := repo.UpdatePortfolioPhoto(ctx, user, portfolioID, photoID, input.Title, input.TagIDs, input.SortOrder)
	if err != nil {
		return PortfolioPhotoView{}, err
	}
	return portfolioPhotoView(photo), nil
}

type portfolioDeleter interface {
	DeletePortfolio(context.Context, uuid.UUID, uuid.UUID, func(string) error) error
}

// DeletePortfolio 删除整个作品集，文件清理失败时保留记录供重试。
func (s *PortfolioPhotoService) DeletePortfolio(ctx context.Context, user, id uuid.UUID) error {
	repo, ok := s.repository.(portfolioDeleter)
	if !ok {
		return fault.NotFound()
	}
	return repo.DeletePortfolio(ctx, user, id, func(key string) error {
		err := s.files.Delete(ctx, key)
		if errors.Is(err, filestore.ErrFileNotFound) {
			return nil
		}
		return err
	})
}

type portfolioInspirationReader interface {
	FindInspirationByBusinessID(context.Context, uuid.UUID, uuid.UUID) (model.Inspiration, error)
}
type portfolioFileReader interface {
	Open(string) (io.ReadCloser, int64, string, error)
}

// CopyInspiration 验证灵感图及其私有文件归属，复制已保存图片并保留其拍摄属性快照。
func (s *PortfolioPhotoService) CopyInspiration(ctx context.Context, user, portfolioID, inspirationID uuid.UUID) (PortfolioPhotoView, error) {
	repo, ok := s.repository.(portfolioInspirationReader)
	if !ok {
		return PortfolioPhotoView{}, fault.NotFound()
	}
	files, ok := s.files.(portfolioFileReader)
	if !ok {
		return PortfolioPhotoView{}, fault.NotFound()
	}
	scope, err := tenant.Require(ctx)
	if err != nil {
		return PortfolioPhotoView{}, err
	}
	if scope.UserID != user {
		return PortfolioPhotoView{}, fault.NotFound()
	}
	if _, err = s.repository.FindPortfolio(ctx, user, portfolioID); err != nil {
		return PortfolioPhotoView{}, err
	}
	original, err := repo.FindInspirationByBusinessID(ctx, user, inspirationID)
	if err != nil {
		return PortfolioPhotoView{}, err
	}
	reader, _, _, err := OpenFile(ctx, files, original.ImageKey)
	if err != nil {
		return PortfolioPhotoView{}, err
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, s.maxBytes+1))
	if err != nil {
		return PortfolioPhotoView{}, err
	}
	return s.upload(ctx, user, portfolioID, original.FileName, content, &original.ImageMetadata)
}
