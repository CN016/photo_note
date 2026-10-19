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
	ListTags(context.Context, uuid.UUID, string, int, int) ([]model.Tag, int64, error)
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

// List 按当前用户和可选名称关键字分页读取标签，分页参数由 HTTP 层预先限定范围。
func (service *TagService) List(ctx context.Context, userID uuid.UUID, query string, page PageRequest) (Page[model.Tag], error) {
	query = strings.TrimSpace(query)
	if len(query) > 200 {
		return Page[model.Tag]{}, fault.InvalidRequest("q 长度不能超过 200 个字节")
	}
	items, total, err := service.tags.ListTags(ctx, userID, query, page.Offset, page.PageSize)
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

// normalizeTagNames 校验并去重整组标签名；名称去首尾空格后按大小写精确匹配。
func normalizeTagNames(names []string) ([]string, error) {
	normalized := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		content := strings.TrimSpace(name)
		if length := utf8.RuneCountInString(content); length < 1 || length > 64 {
			return nil, fault.InvalidField("tag_names", "每个标签名必须为 1 至 64 个字符")
		}
		if _, exists := seen[content]; exists {
			continue
		}
		seen[content] = struct{}{}
		normalized = append(normalized, content)
	}
	return normalized, nil
}
