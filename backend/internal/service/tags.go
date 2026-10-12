package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
)

// TagRepository 定义标签 service 所需的用户隔离查询及引用清理操作。
type TagRepository interface {
	ListTags(context.Context, uuid.UUID, int, int) ([]model.Tag, int64, error)
	CreateTag(context.Context, *model.Tag) error
	FindTagByBusinessID(context.Context, uuid.UUID, uuid.UUID) (model.Tag, error)
	UpdateTag(context.Context, uuid.UUID, uuid.UUID, string) (model.Tag, error)
	DeleteTagAndCleanInspirations(context.Context, uuid.UUID, uuid.UUID) error
}

// TagService 承担标签内容校验和业务 UUID 归属规则。
type TagService struct {
	tags TagRepository
}

// NewTagService 创建标签 service。
func NewTagService(tags TagRepository) *TagService {
	return &TagService{tags: tags}
}

// List 返回当前用户自己的分页标签列表，分页参数由 HTTP 层预先限定范围。
func (service *TagService) List(ctx context.Context, userID uuid.UUID, page PageRequest) (Page[model.Tag], error) {
	items, total, err := service.tags.ListTags(ctx, userID, page.Offset, page.PageSize)
	if err != nil {
		return Page[model.Tag]{}, err
	}
	return Page[model.Tag]{Items: items, Page: page.Page, PageSize: page.PageSize, Total: total}, nil
}

// Create 校验标签文本后生成业务 UUID，并从已认证上下文确定所属用户。
func (service *TagService) Create(ctx context.Context, userID uuid.UUID, content string) (model.Tag, error) {
	content, err := validateTagContent(content)
	if err != nil {
		return model.Tag{}, err
	}
	tag := model.Tag{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: userID, Content: content}
	if err := service.tags.CreateTag(ctx, &tag); err != nil {
		return model.Tag{}, err
	}
	return tag, nil
}

// Get 按当前用户和标签业务 UUID 双重限定详情访问。
func (service *TagService) Get(ctx context.Context, userID, tagID uuid.UUID) (model.Tag, error) {
	return service.tags.FindTagByBusinessID(ctx, userID, tagID)
}

// Update 校验内容并只更新当前用户拥有的指定业务 UUID 标签。
func (service *TagService) Update(ctx context.Context, userID, tagID uuid.UUID, content string) (model.Tag, error) {
	content, err := validateTagContent(content)
	if err != nil {
		return model.Tag{}, err
	}
	return service.tags.UpdateTag(ctx, userID, tagID, content)
}

// Delete 在 repository 的 PostgreSQL 事务内清理所有灵感图数组引用后再删除标签。
func (service *TagService) Delete(ctx context.Context, userID, tagID uuid.UUID) error {
	return service.tags.DeleteTagAndCleanInspirations(ctx, userID, tagID)
}

// validateTagContent 统一执行标签去首尾空格和 1 至 64 个 Unicode 字符长度校验。
func validateTagContent(content string) (string, error) {
	content = strings.TrimSpace(content)
	if length := utf8.RuneCountInString(content); length < 1 || length > 64 {
		return "", fault.InvalidField("content", "标签内容长度必须为 1 至 64 个字符")
	}
	return content, nil
}
