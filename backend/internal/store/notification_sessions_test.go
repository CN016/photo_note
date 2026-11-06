package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/config"
	"github.com/photo-nini/backend/internal/domain/fault"
	"os"
	"sync"
	"testing"
	"time"
)

// TestIndependentNotificationSessionsIntegration 仅在独立测试Redis验证真实Lua，两条刷新链、权限标记和撤销隔离，不访问业务数据库。
func TestIndependentNotificationSessionsIntegration(t *testing.T) {
	address := os.Getenv("PHOTO_NINI_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("未配置独立测试Redis")
	}
	tokens := NewTokenStore(config.Config{RedisAddr: address, AccessTokenTTL: time.Minute, RefreshTokenTTL: 10 * time.Minute})
	defer tokens.Close()
	ctx := context.Background()
	user := uuid.New()
	web, err := tokens.Issue(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	defer tokens.RevokeSession(ctx, web.SessionID)
	parent, err := tokens.ValidateAccess(ctx, web.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	notice, err := tokens.IssueNotificationSession(ctx, parent)
	if err != nil {
		t.Fatal(err)
	}
	defer tokens.RevokeSession(ctx, notice.SessionID)
	if notice.SessionID == web.SessionID || notice.RefreshToken == web.RefreshToken || notice.AccessToken == web.AccessToken {
		t.Fatal("两条会话必须完全独立")
	}
	nclaims, err := tokens.ValidateAccess(ctx, notice.AccessToken)
	if err != nil || nclaims.Purpose != "notification" {
		t.Fatal("缺少通知用途边界", err)
	}
	if _, err = tokens.IssueNotificationSession(ctx, nclaims); !fault.Is(err, 401, "unauthorized") {
		t.Fatal("通知不能递归签发会话", err)
	}
	if _, err = tokens.SwitchEnvironment(ctx, notice.RefreshToken, 17, 1, uuid.NewString()); !fault.Is(err, 401, "unauthorized") {
		t.Fatal("通知不能切换空间", err)
	}
	// 两边并发轮换，任何一边都不能消费另一边的Refresh Token。
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for _, pair := range []TokenPair{web, notice} {
		wg.Add(1)
		go func(p TokenPair) {
			defer wg.Done()
			for i := 0; i < 8; i++ {
				var e error
				p, _, e = tokens.Rotate(ctx, p.RefreshToken)
				if e != nil {
					failures <- e
					return
				}
			}
			if _, e := tokens.ValidateAccess(ctx, p.AccessToken); e != nil {
				failures <- e
			}
		}(pair)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal("独立并发刷新失败", err)
	}
	// 重新申请只替换通知会话，不撤销Web访问令牌。
	replacement, err := tokens.IssueNotificationSession(ctx, parent)
	if err != nil {
		t.Fatal(err)
	}
	defer tokens.RevokeSession(ctx, replacement.SessionID)
	if _, err = tokens.ValidateAccess(ctx, web.AccessToken); err != nil {
		t.Fatal("通知重新签发破坏Web", err)
	}
	if _, err = tokens.ValidateAccess(ctx, notice.AccessToken); !fault.Is(err, 401, "unauthorized") {
		t.Fatal("旧通知会话未撤销", err)
	}
	if err = tokens.RevokeSession(ctx, replacement.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = tokens.ValidateAccess(ctx, web.AccessToken); err != nil {
		t.Fatal("退出通知破坏Web", err)
	}
	replacement, err = tokens.IssueNotificationSession(ctx, parent)
	if err != nil {
		t.Fatal(err)
	}
	defer tokens.RevokeSession(ctx, replacement.SessionID)
	// 同账号第二个正常登录不挤掉已有Web；Web退出只使自己绑定的通知失效。
	other, err := tokens.Issue(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	defer tokens.RevokeSession(ctx, other.SessionID)
	if err = tokens.RevokeSession(ctx, web.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = tokens.ValidateAccess(ctx, replacement.AccessToken); !fault.Is(err, 401, "unauthorized") {
		t.Fatal("Web退出后旧通知仍有效", err)
	}
	if _, err = tokens.ValidateAccess(ctx, other.AccessToken); err != nil {
		t.Fatal("退出影响其他独立登录", err)
	}
	// 组织切换与伪造父会话均不能跨范围签发或读取。
	orgWeb, err := tokens.Issue(ctx, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	defer tokens.RevokeSession(ctx, orgWeb.SessionID)
	scoped, err := tokens.SwitchEnvironment(ctx, orgWeb.RefreshToken, 17, 1, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	scopedClaims, err := tokens.ValidateAccess(ctx, scoped.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	orgNotice, err := tokens.IssueNotificationSession(ctx, scopedClaims)
	if err != nil {
		t.Fatal(err)
	}
	defer tokens.RevokeSession(ctx, orgNotice.SessionID)
	forged := scopedClaims
	forged.UserBusinessID = uuid.New()
	if _, err = tokens.IssueNotificationSession(ctx, forged); !fault.Is(err, 401, "unauthorized") {
		t.Fatal("伪造归属未拒绝", err)
	}
	if _, err = tokens.SwitchEnvironment(ctx, scoped.RefreshToken, 23, 2, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err = tokens.ValidateAccess(ctx, orgNotice.AccessToken); !fault.Is(err, 401, "unauthorized") {
		t.Fatal("空间切换后旧通知未失效", err)
	}
	if _, _, err = tokens.Rotate(ctx, orgNotice.RefreshToken); !fault.Is(err, 401, "unauthorized") {
		t.Fatal("旧空间通知仍可刷新", err)
	}
}
