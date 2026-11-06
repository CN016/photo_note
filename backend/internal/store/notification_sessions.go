package store

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
)

// IssueNotificationSession 为已验证Web会话签发独立只读通知会话。原子替换该Web会话的旧通知会话，绝不消费Web刷新令牌。
// 绑定Web会话代次仅用于退出/切换撤销；两个会话的刷新令牌、访问令牌、索引和轮换均各自独立。
func (store *TokenStore) IssueNotificationSession(ctx context.Context, parent TokenClaims) (TokenPair, error) {
	if !validClaims(parent) || parent.Purpose != "" {
		return TokenPair{}, fault.Unauthorized()
	}
	claims := TokenClaims{UserBusinessID: parent.UserBusinessID, SessionID: uuid.NewString(), OrgID: parent.OrgID, SessionVersion: 1, Purpose: "notification", ParentSessionID: parent.SessionID, ParentVersion: parent.SessionVersion}
	access, err := secureToken()
	if err != nil {
		return TokenPair{}, fault.Internal(err)
	}
	refresh, err := secureToken()
	if err != nil {
		return TokenPair{}, fault.Internal(err)
	}
	serialized, _ := json.Marshal(claims)
	expected, _ := json.Marshal(parent)
	const script = `
      if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end
      local old=redis.call('GET',KEYS[2])
      if old then
        local sk='auth:session:'..old
        for _,k in ipairs(redis.call('SMEMBERS',sk)) do redis.call('DEL',k) end
        redis.call('DEL',sk,'auth:session-state:'..old); redis.call('SREM',KEYS[7],old)
      end
      redis.call('SET',KEYS[3],ARGV[2],'EX',ARGV[3]); redis.call('SET',KEYS[4],ARGV[2],'EX',ARGV[4])
      redis.call('SET',KEYS[5],ARGV[2],'EX',ARGV[4]); redis.call('SADD',KEYS[6],KEYS[3],KEYS[4]); redis.call('EXPIRE',KEYS[6],ARGV[4])
      redis.call('SADD',KEYS[7],ARGV[5]); redis.call('EXPIRE',KEYS[7],ARGV[4]); redis.call('SET',KEYS[2],ARGV[5],'EX',ARGV[4]); return 1`
	n, err := store.client.Eval(ctx, script, []string{sessionStateKey(parent.SessionID), "auth:notification-child:" + parent.SessionID, accessTokenKey(access), refreshTokenKey(refresh), sessionStateKey(claims.SessionID), sessionRedisKey(claims.SessionID), userSessionRedisKey(parent.UserBusinessID)}, string(expected), string(serialized), ttlSeconds(store.accessTokenTTL), ttlSeconds(store.refreshTokenTTL), claims.SessionID).Int()
	if err != nil {
		return TokenPair{}, fault.Unavailable(err)
	}
	if n != 1 {
		return TokenPair{}, fault.Unauthorized()
	}
	return store.scopedPair(access, refresh, claims), nil
}
