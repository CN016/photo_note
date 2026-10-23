package store

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/config"
	"github.com/photo-nini/backend/internal/domain/fault"
)

// TestRedisTenantSessionsIntegration 用独立测试Redis验证Lua原子性；不读取或清空项目Redis数据库。
func TestRedisTenantSessionsIntegration(t *testing.T) {
	address := os.Getenv("PHOTO_NINI_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("未配置独立测试Redis")
	}
	tokens := NewTokenStore(config.Config{RedisAddr: address, AccessTokenTTL: time.Minute, RefreshTokenTTL: 10 * time.Minute})
	defer tokens.Close()
	ctx := context.Background()
	userID := uuid.New()
	pair, err := tokens.Issue(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer tokens.RevokeSession(ctx, pair.SessionID)
	if pair.OrgID != -1 || pair.SessionVersion != 1 || pair.SessionID == "" {
		t.Fatal("初始个人会话元数据无效")
	}
	initial, err := tokens.ValidateAccess(ctx, pair.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	// 缺失orgid/version的旧格式即使Redis存在key，也不得因默认零值而获得个人空间权限。
	oldRaw, _ := secureToken()
	oldJSON, _ := json.Marshal(map[string]any{"user_business_id": userID, "session_id": pair.SessionID})
	if err = tokens.client.Set(ctx, accessTokenKey(oldRaw), oldJSON, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	defer tokens.client.Del(ctx, accessTokenKey(oldRaw))
	if _, err = tokens.ValidateAccess(ctx, oldRaw); !fault.Is(err, 401, "unauthorized") {
		t.Fatalf("旧格式未拒绝: %v", err)
	}
	switched, err := tokens.SwitchEnvironment(ctx, pair.RefreshToken, 17, 1, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if switched.OrgID != 17 || switched.SessionVersion != 2 || switched.SessionID != pair.SessionID {
		t.Fatal("切换会话元数据错误")
	}
	if _, err = tokens.ValidateAccess(ctx, pair.AccessToken); !fault.Is(err, 401, "unauthorized") {
		t.Fatalf("切换后旧access仍有效: %v", err)
	}
	if _, _, err = tokens.Rotate(ctx, pair.RefreshToken); !fault.Is(err, 401, "unauthorized") {
		t.Fatalf("切换后旧refresh仍能普通刷新: %v", err)
	}
	if current, err := tokens.ValidateAccess(ctx, switched.AccessToken); err != nil || current.OrgID != 17 {
		t.Fatalf("新组织access无效: %+v %v", current, err)
	}
	// 同一refresh并发轮换只允许一个成功，并保留组织与版本。
	var wait sync.WaitGroup
	var mu sync.Mutex
	success := 0
	var rotated TokenPair
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			p, _, e := tokens.Rotate(ctx, switched.RefreshToken)
			if e == nil {
				mu.Lock()
				success++
				rotated = p
				mu.Unlock()
			}
		}()
	}
	wait.Wait()
	if success != 1 {
		t.Fatalf("并发刷新成功数=%d,要求1", success)
	}
	if rotated.OrgID != 17 || rotated.SessionVersion != 2 {
		t.Fatal("普通刷新改变了空间")
	}
	switchID := uuid.NewString()
	next, err := tokens.SwitchEnvironment(ctx, rotated.RefreshToken, 18, 2, switchID)
	if err != nil {
		t.Fatal(err)
	}
	// 丢失切换响应时只能凭相同switch_id恢复相同版本，恢复不泄露或保存原始token。
	recovered, err := tokens.SwitchEnvironment(ctx, rotated.RefreshToken, 18, 2, switchID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.SessionVersion != 3 || recovered.OrgID != 18 {
		t.Fatal("恢复会话元数据错误")
	}
	if _, err = tokens.ValidateAccess(ctx, next.AccessToken); !fault.Is(err, 401, "unauthorized") {
		t.Fatal("恢复没有撤销上次未确认token")
	}
	if _, err = tokens.SwitchEnvironment(ctx, rotated.RefreshToken, 18, 2, uuid.NewString()); !fault.Is(err, 401, "unauthorized") {
		t.Fatalf("不同switch_id可恢复旧凭证: %v", err)
	}
	if initial.UserBusinessID != userID {
		t.Fatal("用户身份改变")
	}
	if err = tokens.RevokeSession(ctx, recovered.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = tokens.ValidateAccess(ctx, recovered.AccessToken); !fault.Is(err, 401, "unauthorized") {
		t.Fatal("退出后access仍有效")
	}
	if _, err = tokens.SwitchEnvironment(ctx, rotated.RefreshToken, 18, 2, switchID); !fault.Is(err, 401, "unauthorized") {
		t.Fatalf("退出后恢复复活会话: %v", err)
	}
}
