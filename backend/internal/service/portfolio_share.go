package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

// PortfolioShareRepository 只提供分享所需的作品集读写；实现须对普通操作校验空间及创建者。
type PortfolioShareRepository interface {
	SetPortfolioShareHash(context.Context, uuid.UUID, uuid.UUID, *string) error
	FindPortfolioByShareHash(context.Context, string) (model.Portfolio, error)
}

// PortfolioShareService 负责生成、撤销和验证公开分享能力，令牌只存储不可逆摘要。
type PortfolioShareService struct{ repository PortfolioShareRepository }

// NewPortfolioShareService 注入作品集持久化实现。
func NewPortfolioShareService(repository PortfolioShareRepository) *PortfolioShareService {
	return &PortfolioShareService{repository: repository}
}

// Reset 生成新令牌并替换旧摘要，返回的明文只能在本次操作响应中取得。
func (s *PortfolioShareService) Reset(ctx context.Context, userID, portfolioID uuid.UUID) (string, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return "", err
	}
	if scope.UserID != userID {
		return "", fault.NotFound()
	}
	if s == nil || s.repository == nil {
		return "", fault.Unavailable(errors.New("作品集分享服务未初始化"))
	}
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(data)
	hash := portfolioShareHash(token)
	if err := s.repository.SetPortfolioShareHash(ctx, userID, portfolioID, &hash); err != nil {
		return "", err
	}
	return token, nil
}

// Revoke 清除摘要，使现有分享链接失效；已公开图片的独立直链不受影响。
func (s *PortfolioShareService) Revoke(ctx context.Context, userID, portfolioID uuid.UUID) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if scope.UserID != userID {
		return fault.NotFound()
	}
	if s == nil || s.repository == nil {
		return fault.Unavailable(errors.New("作品集分享服务未初始化"))
	}
	return s.repository.SetPortfolioShareHash(ctx, userID, portfolioID, nil)
}

// Resolve 通过受控摘要查找分享记录，并返回从记录建立的可信租户上下文。
func (s *PortfolioShareService) Resolve(ctx context.Context, token string) (context.Context, model.Portfolio, error) {
	if s == nil || s.repository == nil || len(token) != 43 {
		return ctx, model.Portfolio{}, fault.NotFound()
	}
	bytes, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(bytes) != 32 {
		return ctx, model.Portfolio{}, fault.NotFound()
	}
	item, err := s.repository.FindPortfolioByShareHash(ctx, portfolioShareHash(token))
	if err != nil || item.ShareTokenHash == nil {
		return ctx, model.Portfolio{}, fault.NotFound()
	}
	scoped := tenant.WithScope(ctx, tenant.Scope{OrgID: item.OrgID, UserID: item.UserID})
	if _, err := tenant.Require(scoped); err != nil {
		return ctx, model.Portfolio{}, fault.NotFound()
	}
	return scoped, item, nil
}

// portfolioShareHash 固定 SHA-256 摘要格式，不持久化或记录分享明文。
func portfolioShareHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
