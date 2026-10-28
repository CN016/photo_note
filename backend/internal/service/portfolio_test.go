package service

import (
	"context"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"testing"
)

type portfolioMemory struct{ items []model.Portfolio }

func (m *portfolioMemory) ListPortfolios(_ context.Context, _ uuid.UUID) ([]model.Portfolio, error) {
	return m.items, nil
}
func (m *portfolioMemory) FindPortfolio(_ context.Context, _, id uuid.UUID) (model.Portfolio, error) {
	for _, x := range m.items {
		if x.BusinessID == id {
			return x, nil
		}
	}
	return model.Portfolio{}, context.Canceled
}
func (m *portfolioMemory) CreatePortfolio(_ context.Context, p *model.Portfolio) error {
	m.items = append(m.items, *p)
	return nil
}
func (m *portfolioMemory) ValidateTagOwnership(_ context.Context, _ uuid.UUID, ids []uuid.UUID) error {
	if len(ids) > 0 {
		return context.Canceled
	}
	return nil
}

// TestPortfolioCreate 确认创建拒绝缺失范围、重复或不归属标签，并默认关闭分享。
func TestPortfolioCreate(t *testing.T) {
	user := uuid.New()
	m := &portfolioMemory{}
	s := NewPortfolioService(m)
	ctx := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 3, UserID: user})
	if _, err := s.Create(context.Background(), user, PortfolioInput{Title: "title"}); err == nil {
		t.Fatal("缺少范围应失败")
	}
	if _, err := s.Create(ctx, uuid.New(), PortfolioInput{Title: "title"}); err == nil {
		t.Fatal("其他用户应失败")
	}
	if _, err := s.Create(ctx, user, PortfolioInput{Title: "  "}); err == nil {
		t.Fatal("空标题应失败")
	}
	if _, err := s.Create(ctx, user, PortfolioInput{Title: "title", TagIDs: []uuid.UUID{uuid.New()}}); err == nil {
		t.Fatal("未授权标签应失败")
	}
	got, err := s.Create(ctx, user, PortfolioInput{Title: " sample "})
	if err != nil || got.Title != "sample" || got.ShareEnabled {
		t.Fatalf("创建结果错误: %+v %v", got, err)
	}
}
