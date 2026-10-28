package service

import (
	"context"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"testing"
)

type portfolioShareMemory struct{ item model.Portfolio }

func (m *portfolioShareMemory) SetPortfolioShareHash(_ context.Context, user, id uuid.UUID, hash *string) error {
	if m.item.UserID != user || m.item.BusinessID != id {
		return context.Canceled
	}
	m.item.ShareTokenHash = hash
	return nil
}
func (m *portfolioShareMemory) FindPortfolioByShareHash(_ context.Context, hash string) (model.Portfolio, error) {
	if m.item.ShareTokenHash == nil || *m.item.ShareTokenHash != hash {
		return model.Portfolio{}, context.Canceled
	}
	return m.item, nil
}

// TestPortfolioShareLifecycle 验证令牌重置、撤销及可信空间恢复。
func TestPortfolioShareLifecycle(t *testing.T) {
	user, id := uuid.New(), uuid.New()
	memory := &portfolioShareMemory{item: model.Portfolio{BaseModel: model.BaseModel{OrgID: 12, BusinessID: id}, UserID: user}}
	svc := NewPortfolioShareService(memory)
	ctx := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 12, UserID: user})
	if _, err := svc.Reset(context.Background(), user, id); err == nil {
		t.Fatal("缺失租户必须拒绝")
	}
	first, err := svc.Reset(ctx, user, id)
	if err != nil {
		t.Fatal(err)
	}
	if memory.item.ShareTokenHash == nil || *memory.item.ShareTokenHash == first {
		t.Fatal("不得保存令牌明文")
	}
	publicCtx, _, err := svc.Resolve(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := tenant.Require(publicCtx)
	if err != nil || scope.OrgID != 12 || scope.UserID != user {
		t.Fatal("分享范围错误")
	}
	second, err := svc.Reset(ctx, user, id)
	if err != nil || first == second {
		t.Fatal("重置应生成新令牌")
	}
	if _, _, err := svc.Resolve(context.Background(), first); err == nil {
		t.Fatal("旧令牌仍有效")
	}
	if err := svc.Revoke(ctx, user, id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Resolve(context.Background(), second); err == nil {
		t.Fatal("撤销后令牌仍有效")
	}
}
