package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/config"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/redis/go-redis/v9"
)

// TokenClaims 是 Redis 中保存的最小会话信息，不含原始 access 或 refresh token。
type TokenClaims struct {
	UserBusinessID uuid.UUID `json:"user_business_id"`
	SessionID      string    `json:"session_id"`
	OrgID          int64     `json:"orgid"`
	SessionVersion int64     `json:"session_version"`
}

// TokenPair 是注册、登录和刷新接口返回的两种不透明随机令牌。
type TokenPair struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
	OrgID            int64  `json:"orgid"`
	SessionID        string `json:"session_id"`
	SessionVersion   int64  `json:"session_version"`
}

// TokenStore 负责签发、校验、原子刷新和撤销 Redis 会话内的所有令牌。
type TokenStore struct {
	client          *redis.Client
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
}

// NewTokenStore 创建带连接超时的 Redis 客户端，并保留由配置统一管理的令牌有效期。
func NewTokenStore(cfg config.Config) *TokenStore {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.RedisAddr,
		Password:     cfg.RedisPassword,
		DB:           cfg.RedisDB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     20,
	})
	return &TokenStore{client: client, accessTokenTTL: cfg.AccessTokenTTL, refreshTokenTTL: cfg.RefreshTokenTTL}
}

// Client 暴露 Redis 客户端给启动连通性检查和健康检查，不允许业务层直接读写令牌 key。
func (store *TokenStore) Client() *redis.Client {
	return store.client
}

// Close 释放 Redis 连接池。
func (store *TokenStore) Close() error {
	return store.client.Close()
}

// Issue 为新浏览器会话签发个人空间令牌；每个会话独立维护当前组织和版本栅栏。
func (store *TokenStore) Issue(ctx context.Context, userID uuid.UUID) (TokenPair, error) {
	claims := TokenClaims{UserBusinessID: userID, SessionID: uuid.NewString(), OrgID: -1, SessionVersion: 1}
	access, err := secureToken()
	if err != nil {
		return TokenPair{}, fault.Internal(err)
	}
	refresh, err := secureToken()
	if err != nil {
		return TokenPair{}, fault.Internal(err)
	}
	serialized, _ := json.Marshal(claims)
	pipe := store.client.TxPipeline()
	pipe.Set(ctx, accessTokenKey(access), serialized, store.accessTokenTTL)
	pipe.Set(ctx, refreshTokenKey(refresh), serialized, store.refreshTokenTTL)
	pipe.Set(ctx, sessionStateKey(claims.SessionID), serialized, store.refreshTokenTTL)
	pipe.SAdd(ctx, sessionRedisKey(claims.SessionID), accessTokenKey(access), refreshTokenKey(refresh))
	pipe.Expire(ctx, sessionRedisKey(claims.SessionID), store.refreshTokenTTL)
	pipe.SAdd(ctx, userSessionRedisKey(userID), claims.SessionID)
	pipe.Expire(ctx, userSessionRedisKey(userID), store.refreshTokenTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return TokenPair{}, fault.Unavailable(err)
	}
	return store.scopedPair(access, refresh, claims), nil
}

// validClaims 拒绝升级前缺少组织/版本的旧会话；orgid=0 从不代表个人版。
func validClaims(claims TokenClaims) bool {
	return claims.UserBusinessID != uuid.Nil && claims.SessionID != "" && claims.SessionVersion > 0 && (claims.OrgID == -1 || claims.OrgID > 0)
}

// readCurrentClaims 在同一 Redis Lua 中检查令牌声明与当前会话状态，避免读取后切换的时间窗口。
func (store *TokenStore) readCurrentClaims(ctx context.Context, key string) (TokenClaims, error) {
	const script = `
      local value=redis.call('GET',KEYS[1]); if not value then return '' end
      local ok,c=pcall(cjson.decode,value); if not ok or not c.session_id or not c.session_version or not c.orgid then return '' end
      if redis.call('GET','auth:session-state:'..c.session_id) ~= value then return '' end
      return value`
	value, err := store.client.Eval(ctx, script, []string{key}).Text()
	if err != nil {
		return TokenClaims{}, fault.Unavailable(err)
	}
	var claims TokenClaims
	if value == "" || json.Unmarshal([]byte(value), &claims) != nil || !validClaims(claims) {
		return TokenClaims{}, fault.Unauthorized()
	}
	return claims, nil
}

// ValidateAccess 校验会话版本；被空间切换撤销的旧 access token 即使 TTL 尚存也不能继续访问。
func (store *TokenStore) ValidateAccess(ctx context.Context, raw string) (TokenClaims, error) {
	if raw == "" {
		return TokenClaims{}, fault.Unauthorized()
	}
	return store.readCurrentClaims(ctx, accessTokenKey(raw))
}

// ReadRefreshClaims 只校验当前刷新令牌，不消费它，供业务层先验证组织和成员状态。
func (store *TokenStore) ReadRefreshClaims(ctx context.Context, raw string) (TokenClaims, error) {
	if raw == "" {
		return TokenClaims{}, fault.Unauthorized()
	}
	return store.readCurrentClaims(ctx, refreshTokenKey(raw))
}

// Rotate 单次消费 refresh token；普通刷新保留当前空间版本，Lua 同时核验版本栅栏。
func (store *TokenStore) Rotate(ctx context.Context, raw string) (TokenPair, TokenClaims, error) {
	claims, err := store.ReadRefreshClaims(ctx, raw)
	if err != nil {
		return TokenPair{}, TokenClaims{}, err
	}
	access, err := secureToken()
	if err != nil {
		return TokenPair{}, TokenClaims{}, fault.Internal(err)
	}
	refresh, err := secureToken()
	if err != nil {
		return TokenPair{}, TokenClaims{}, fault.Internal(err)
	}
	serialized, _ := json.Marshal(claims)
	const script = `
      if redis.call('GET',KEYS[1])~=ARGV[1] or redis.call('GET',KEYS[4])~=ARGV[1] then return 0 end
      if redis.call('SISMEMBER',KEYS[5],KEYS[1])==0 then return 0 end
      redis.call('SET',KEYS[2],ARGV[1],'EX',ARGV[2]); redis.call('SET',KEYS[3],ARGV[1],'EX',ARGV[3])
      redis.call('DEL',KEYS[1]); redis.call('SREM',KEYS[5],KEYS[1]); redis.call('SADD',KEYS[5],KEYS[2],KEYS[3])
      redis.call('EXPIRE',KEYS[5],ARGV[3]); redis.call('EXPIRE',KEYS[4],ARGV[3]);
      redis.call('SADD',KEYS[6],ARGV[4]); redis.call('EXPIRE',KEYS[6],ARGV[3]); return 1`
	n, err := store.client.Eval(ctx, script, []string{refreshTokenKey(raw), accessTokenKey(access), refreshTokenKey(refresh), sessionStateKey(claims.SessionID), sessionRedisKey(claims.SessionID), userSessionRedisKey(claims.UserBusinessID)}, string(serialized), ttlSeconds(store.accessTokenTTL), ttlSeconds(store.refreshTokenTTL), claims.SessionID).Int()
	if err != nil {
		return TokenPair{}, TokenClaims{}, fault.Unavailable(err)
	}
	if n != 1 {
		return TokenPair{}, TokenClaims{}, fault.Unauthorized()
	}
	return store.scopedPair(access, refresh, claims), claims, nil
}

// switchReceipt 不保存令牌原文；原刷新凭证与随机切换 UUID 共同支持响应丢失后的有限恢复。
type switchReceipt struct {
	Claims          TokenClaims `json:"claims"`
	SwitchID        string      `json:"switch_id"`
	PreviousVersion int64       `json:"previous_version"`
}

// ReadSwitchClaims 仅允许相同切换 UUID 使用旧刷新凭证恢复已提交结果，不恢复普通刷新权限。
func (store *TokenStore) ReadSwitchClaims(ctx context.Context, raw, switchID string) (TokenClaims, error) {
	claims, err := store.ReadRefreshClaims(ctx, raw)
	if err == nil {
		return claims, nil
	}
	if !fault.Is(err, 401, "unauthorized") {
		return TokenClaims{}, err
	}
	receipt, readErr := store.client.Get(ctx, switchReceiptKey(raw)).Bytes()
	if errors.Is(readErr, redis.Nil) {
		return TokenClaims{}, fault.Unauthorized()
	}
	if readErr != nil {
		return TokenClaims{}, fault.Unavailable(readErr)
	}
	var saved switchReceipt
	if json.Unmarshal(receipt, &saved) != nil || saved.SwitchID != switchID || !validClaims(saved.Claims) {
		return TokenClaims{}, fault.Unauthorized()
	}
	state, stateErr := store.client.Get(ctx, sessionStateKey(saved.Claims.SessionID)).Bytes()
	if errors.Is(stateErr, redis.Nil) {
		return TokenClaims{}, fault.Unauthorized()
	}
	if stateErr != nil {
		return TokenClaims{}, fault.Unavailable(stateErr)
	}
	var current TokenClaims
	if json.Unmarshal(state, &current) != nil || current != saved.Claims {
		return TokenClaims{}, fault.Unauthorized()
	}
	return saved.Claims, nil
}

// SwitchEnvironment 以预期版本原子切换并撤销该会话全部旧令牌；幂等恢复只重新签发同一版本。
func (store *TokenStore) SwitchEnvironment(ctx context.Context, raw string, orgID, expected int64, switchID string) (TokenPair, error) {
	claims, err := store.ReadSwitchClaims(ctx, raw, switchID)
	if err != nil {
		return TokenPair{}, err
	}
	oldSerialized, _ := json.Marshal(claims)
	next := claims
	if claims.SessionVersion == expected {
		next.OrgID = orgID
		next.SessionVersion++
	} else if claims.SessionVersion != expected+1 || claims.OrgID != orgID {
		return TokenPair{}, fault.Conflict("空间会话已变更，请重新读取当前状态")
	}
	access, err := secureToken()
	if err != nil {
		return TokenPair{}, fault.Internal(err)
	}
	refresh, err := secureToken()
	if err != nil {
		return TokenPair{}, fault.Internal(err)
	}
	nextSerialized, _ := json.Marshal(next)
	receipt, _ := json.Marshal(switchReceipt{Claims: next, SwitchID: switchID, PreviousVersion: expected})
	const script = `
      local old=redis.call('GET',KEYS[1]); local receipt=redis.call('GET',KEYS[7]);
      local recovery=false
      if receipt then local r=cjson.decode(receipt); recovery=(r.switch_id==ARGV[7] and r.previous_version==tonumber(ARGV[8]) and r.claims.orgid==tonumber(ARGV[9])) end
      local state=redis.call('GET',KEYS[4])
      if recovery then if state~=ARGV[2] then return 0 end
      else if old~=ARGV[1] or state~=ARGV[1] then return 0 end end
      local keys=redis.call('SMEMBERS',KEYS[5]); for _,k in ipairs(keys) do redis.call('DEL',k) end
      redis.call('DEL',KEYS[5]);
      redis.call('SET',KEYS[2],ARGV[2],'EX',ARGV[3]); redis.call('SET',KEYS[3],ARGV[2],'EX',ARGV[4]);
      redis.call('SET',KEYS[4],ARGV[2],'EX',ARGV[4]); redis.call('SADD',KEYS[5],KEYS[2],KEYS[3]); redis.call('EXPIRE',KEYS[5],ARGV[4]);
      redis.call('SADD',KEYS[6],ARGV[5]); redis.call('EXPIRE',KEYS[6],ARGV[4]);
      redis.call('SET',KEYS[7],ARGV[6],'EX',ARGV[3]); return 1`
	n, err := store.client.Eval(ctx, script, []string{refreshTokenKey(raw), accessTokenKey(access), refreshTokenKey(refresh), sessionStateKey(next.SessionID), sessionRedisKey(next.SessionID), userSessionRedisKey(next.UserBusinessID), switchReceiptKey(raw)}, string(oldSerialized), string(nextSerialized), ttlSeconds(store.accessTokenTTL), ttlSeconds(store.refreshTokenTTL), next.SessionID, string(receipt), switchID, expected, orgID).Int()
	if err != nil {
		return TokenPair{}, fault.Unavailable(err)
	}
	if n != 1 {
		return TokenPair{}, fault.Conflict("空间会话已变更，请重新读取当前状态")
	}
	return store.scopedPair(access, refresh, next), nil
}

// RevokeSession 同时撤销令牌集合与会话版本，迟到的切换恢复也不能重新激活已退出会话。
func (store *TokenStore) RevokeSession(ctx context.Context, sessionID string) error {
	const script = `local keys=redis.call('SMEMBERS',KEYS[1]); for _,k in ipairs(keys) do local v=redis.call('GET',k); if v then local c=cjson.decode(v); redis.call('SREM','auth:user-sessions:'..c.user_business_id,c.session_id) end; redis.call('DEL',k) end; redis.call('DEL',KEYS[1],KEYS[2]); return 1`
	if err := store.client.Eval(ctx, script, []string{sessionRedisKey(sessionID), sessionStateKey(sessionID)}).Err(); err != nil {
		return fault.Unavailable(err)
	}
	return nil
}

// RevokeOtherSessions 改密撤销整个账号的其他设备会话，不把撤销范围缩小为当前组织。
func (store *TokenStore) RevokeOtherSessions(ctx context.Context, userID uuid.UUID, keep string) error {
	const script = `local ids=redis.call('SMEMBERS',KEYS[1]); for _,sid in ipairs(ids) do if sid~=ARGV[1] then local sk='auth:session:'..sid; for _,k in ipairs(redis.call('SMEMBERS',sk)) do redis.call('DEL',k) end; redis.call('DEL',sk,'auth:session-state:'..sid); redis.call('SREM',KEYS[1],sid) end end; return 1`
	if err := store.client.Eval(ctx, script, []string{userSessionRedisKey(userID)}, keep).Err(); err != nil {
		return fault.Unavailable(err)
	}
	return nil
}

// AcquireUserLock 串行登录签发与改密；Redis 中仅保存随机锁值，不记录密码或令牌。
func (store *TokenStore) AcquireUserLock(ctx context.Context, userID uuid.UUID) (func(), error) {
	value, err := secureToken()
	if err != nil {
		return nil, fault.Internal(err)
	}
	key := "auth:user-lock:" + userID.String()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		ok, err := store.client.SetNX(ctx, key, value, time.Minute).Result()
		if err != nil {
			return nil, fault.Unavailable(err)
		}
		if ok {
			return func() {
				c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = store.client.Eval(c, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, []string{key}, value).Err()
			}, nil
		}
		select {
		case <-ctx.Done():
			return nil, fault.Unavailable(ctx.Err())
		case <-deadline.C:
			return nil, fault.Unavailable(fmt.Errorf("等待用户账户锁超时"))
		case <-ticker.C:
		}
	}
}

// IndexExistingSessions 只给新格式有效会话补建账号索引；升级前缺少组织版本的令牌始终被拒绝。
func (store *TokenStore) IndexExistingSessions(ctx context.Context) error {
	it := store.client.Scan(ctx, 0, "auth:session-state:*", 100).Iterator()
	for it.Next(ctx) {
		raw, err := store.client.Get(ctx, it.Val()).Bytes()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return fault.Unavailable(err)
		}
		var c TokenClaims
		if json.Unmarshal(raw, &c) != nil || !validClaims(c) {
			continue
		}
		if err := store.client.SAdd(ctx, userSessionRedisKey(c.UserBusinessID), c.SessionID).Err(); err != nil {
			return fault.Unavailable(err)
		}
		_ = store.client.Expire(ctx, userSessionRedisKey(c.UserBusinessID), store.refreshTokenTTL).Err()
	}
	if err := it.Err(); err != nil {
		return fault.Unavailable(err)
	}
	return nil
}

// scopedPair 把不透明令牌对应的可信空间信息一并返回，客户端无需解析 bearer secret。
func (store *TokenStore) scopedPair(access, refresh string, claims TokenClaims) TokenPair {
	pair := store.pair(access, refresh)
	pair.OrgID = claims.OrgID
	pair.SessionID = claims.SessionID
	pair.SessionVersion = claims.SessionVersion
	return pair
}

func sessionStateKey(id string) string   { return "auth:session-state:" + id }
func switchReceiptKey(raw string) string { return "auth:switch-receipt:" + tokenDigest(raw) }

// CheckFeedbackLimit 使用 IP 摘要执行滚动一小时计数，不在 Redis 键或日志中保存原始 IP。
func (store *TokenStore) CheckFeedbackLimit(ctx context.Context, clientIP string, limit int) (bool, error) {
	return store.checkIPRateLimit(ctx, "feedback", clientIP, limit)
}

// CheckScheduleBookingLimit 用独立 Redis 命名空间限制公开预约申请，不与反馈额度互相占用。
func (store *TokenStore) CheckScheduleBookingLimit(ctx context.Context, clientIP string, limit int) (bool, error) {
	return store.checkIPRateLimit(ctx, "schedule-booking", clientIP, limit)
}

// checkIPRateLimit 以 IP 摘要执行滚动一小时计数，Redis 不保存原始网络地址。
func (store *TokenStore) checkIPRateLimit(ctx context.Context, namespace, clientIP string, limit int) (bool, error) {
	digest := sha256.Sum256([]byte(clientIP))
	key := namespace + ":ip:" + hex.EncodeToString(digest[:])
	entryID := uuid.NewString()
	const rateLimitScript = `
		local current = redis.call('TIME')
		local now_ms = current[1] * 1000 + math.floor(current[2] / 1000)
		local cutoff = now_ms - 3600000
		redis.call('ZREMRANGEBYSCORE', KEYS[1], 0, cutoff)
		local count = redis.call('ZCARD', KEYS[1])
		if count >= tonumber(ARGV[1]) then return 0 end
		redis.call('ZADD', KEYS[1], now_ms, ARGV[2])
		redis.call('EXPIRE', KEYS[1], 3600)
		return 1
	`
	allowed, err := store.client.Eval(ctx, rateLimitScript, []string{key}, limit, entryID).Int64()
	if err != nil {
		return false, fault.Unavailable(err)
	}
	return allowed == 1, nil
}

// pair 统一生成 API token 响应时长，秒数与 Redis TTL 使用同一配置来源。
func (store *TokenStore) pair(accessToken, refreshToken string) TokenPair {
	return TokenPair{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		TokenType:        "Bearer",
		ExpiresIn:        int64(store.accessTokenTTL.Seconds()),
		RefreshExpiresIn: int64(store.refreshTokenTTL.Seconds()),
	}
}

// secureToken 从系统 CSPRNG 生成 32 字节不透明 token，不嵌入用户标识、时间戳或过期信息。
func secureToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("生成随机 token 失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// tokenDigest 只把原始 token 的 SHA-256 十六进制摘要用于 Redis key，Redis 不保存 bearer secret。
func tokenDigest(rawToken string) string {
	digest := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(digest[:])
}

// accessTokenKey 与 refreshTokenKey 使用独立命名空间，阻止两种令牌互相冒用。
func accessTokenKey(rawToken string) string  { return "auth:access:" + tokenDigest(rawToken) }
func refreshTokenKey(rawToken string) string { return "auth:refresh:" + tokenDigest(rawToken) }

// sessionRedisKey 以随机会话 UUID 编址，便于退出时撤销该设备会话的所有令牌。
func sessionRedisKey(sessionID string) string { return "auth:session:" + sessionID }

// userSessionRedisKey 索引用户业务 UUID 下的活跃会话 UUID，不存储 bearer token。
func userSessionRedisKey(userID uuid.UUID) string { return "auth:user-sessions:" + userID.String() }

// ttlSeconds 将正 duration 转为 Redis EX 秒数，避免小于一秒的配置被 Redis 当作无效 TTL。
func ttlSeconds(value time.Duration) int64 {
	seconds := int64(value / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}
