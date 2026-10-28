package service

import (
	"context"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"strings"
	"unicode/utf8"
)

// PortfolioRepository 限定作品集数据操作所需能力。
type PortfolioRepository interface {
	ListPortfolios(context.Context, uuid.UUID) ([]model.Portfolio, error)
	FindPortfolio(context.Context, uuid.UUID, uuid.UUID) (model.Portfolio, error)
	CreatePortfolio(context.Context, *model.Portfolio) error
	ValidateTagOwnership(context.Context, uuid.UUID, []uuid.UUID) error
}

// PortfolioService 保证作品集输入及创建者范围由服务端验证。
type PortfolioService struct{ repository PortfolioRepository }

// NewPortfolioService 注入作品集存储。
func NewPortfolioService(repository PortfolioRepository) *PortfolioService {
	return &PortfolioService{repository: repository}
}

// PortfolioView 不包含内部数据库 ID、用户 ID 或分享令牌摘要。
type PortfolioView struct {
	BusinessID   uuid.UUID `json:"business_id"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	TagIDs       []string  `json:"tag_ids"`
	ShareEnabled bool      `json:"share_enabled"`
}

// PortfolioInput 是创建作品集的用户可写字段。
type PortfolioInput struct {
	Title       string      `json:"title"`
	Description string      `json:"description"`
	TagIDs      []uuid.UUID `json:"tag_ids"`
}

func portfolioView(item model.Portfolio) PortfolioView {
	return PortfolioView{BusinessID: item.BusinessID, Title: item.Title, Description: item.Description, TagIDs: []string(item.TagIDs), ShareEnabled: item.ShareTokenHash != nil}
}

// List 按当前空间与创建者读取作品集。
func (s *PortfolioService) List(ctx context.Context, user uuid.UUID) ([]PortfolioView, error) {
	items, err := s.repository.ListPortfolios(ctx, user)
	if err != nil {
		return nil, err
	}
	views := make([]PortfolioView, 0, len(items))
	for _, item := range items {
		views = append(views, portfolioView(item))
	}
	return views, nil
}

// Get 只返回当前用户拥有的作品集。
func (s *PortfolioService) Get(ctx context.Context, user, id uuid.UUID) (PortfolioView, error) {
	item, err := s.repository.FindPortfolio(ctx, user, id)
	if err != nil {
		return PortfolioView{}, err
	}
	return portfolioView(item), nil
}

// Create 校验标题和标签归属，再创建默认不分享的作品集。
func (s *PortfolioService) Create(ctx context.Context, user uuid.UUID, input PortfolioInput) (PortfolioView, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return PortfolioView{}, err
	}
	if scope.UserID != user {
		return PortfolioView{}, fault.NotFound()
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	if n := utf8.RuneCountInString(input.Title); n == 0 || n > 120 {
		return PortfolioView{}, fault.InvalidField("title", "标题应为 1 至 120 个字符")
	}
	if utf8.RuneCountInString(input.Description) > 5000 {
		return PortfolioView{}, fault.InvalidField("description", "说明过长")
	}
	seen := map[uuid.UUID]bool{}
	tags := pq.StringArray{}
	for _, id := range input.TagIDs {
		if id == uuid.Nil || seen[id] {
			return PortfolioView{}, fault.InvalidField("tag_ids", "标签无效或重复")
		}
		seen[id] = true
		tags = append(tags, id.String())
	}
	if err := s.repository.ValidateTagOwnership(ctx, user, input.TagIDs); err != nil {
		return PortfolioView{}, err
	}
	item := model.Portfolio{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, Title: input.Title, Description: input.Description, TagIDs: tags}
	if err := s.repository.CreatePortfolio(ctx, &item); err != nil {
		return PortfolioView{}, err
	}
	return portfolioView(item), nil
}
