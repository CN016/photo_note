package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

// noticeDispatchRepository 模拟领取后撤销资格，验证实际外发也会重新检查而不是只信任队列快照。
type noticeDispatchRepository struct {
	NoticeRepository
	allowed []bool
	checks  int
	err     error
	scope   tenant.Scope
}

func (repository *noticeDispatchRepository) CanDispatchNoticeDelivery(ctx context.Context, _ uuid.UUID) (bool, error) {
	repository.scope, _ = tenant.FromContext(ctx)
	index := repository.checks
	repository.checks++
	if repository.err != nil {
		return false, repository.err
	}
	if index >= len(repository.allowed) {
		return false, nil
	}
	return repository.allowed[index], nil
}

type noticeDispatchTransport func(*http.Request) (*http.Response, error)

func (transport noticeDispatchTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

// TestNoticeRechecksRevocationBeforeNetwork 覆盖领取即撤权、准备请求后撤权和资格查询故障，任何拒绝都不发送正文。
func TestNoticeRechecksRevocationBeforeNetwork(t *testing.T) {
	for _, test := range []struct {
		name         string
		allowed      []bool
		err          error
		wantCode     string
		wantRequests int
	}{
		{name: "领取后成员被移除", allowed: []bool{false}, wantCode: "delivery_unavailable"},
		{name: "准备请求后组织停用", allowed: []bool{true, false}, wantCode: "delivery_unavailable"},
		{name: "资格查询故障", err: errors.New("database unavailable"), wantCode: "dispatch_authorization_unavailable"},
		{name: "两次资格都有效", allowed: []bool{true, true}, wantRequests: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			user := uuid.New()
			ctx := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 31, UserID: user})
			repository := &noticeDispatchRepository{allowed: test.allowed, err: test.err}
			service := NewNoticeService(repository, nil)
			requests := 0
			service.client.Transport = noticeDispatchTransport(func(request *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errcode":0}`)), Request: request}, nil
			})
			delivery := model.NoticeDelivery{BaseModel: model.BaseModel{BusinessID: uuid.New(), OrgID: 31}, UserID: user, Payload: model.JSONB(`{"text":"private organization content"}`)}
			config := model.NoticeConfig{BaseModel: model.BaseModel{OrgID: 31}, UserID: user, Name: "测试渠道", Provider: "wecom", WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test-placeholder"}
			if got := service.deliver(ctx, delivery, config); got != test.wantCode || requests != test.wantRequests {
				t.Fatalf("撤权边界不符: code=%q requests=%d wantCode=%q wantRequests=%d", got, requests, test.wantCode, test.wantRequests)
			}
			if repository.scope.OrgID != 31 || repository.scope.UserID != user {
				t.Fatalf("投递资格丢失作用域: %+v", repository.scope)
			}
		})
	}
}
