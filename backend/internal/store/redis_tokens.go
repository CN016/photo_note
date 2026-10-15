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
}

// TokenPair 是注册、登录和刷新接口返回的两种不透明随机令牌。
type TokenPair struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
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

// Issue 为一个用户会话分别生成两个 32 字节随机 token，并以摘要 key 存储其 TTL。
func (store *TokenStore) Issue(ctx context.Context, userID uuid.UUID) (TokenPair, error) {
	accessToken, err := secureToken()
	if err != nil {
		return TokenPair{}, fault.Internal(err)
	}
	refreshToken, err := secureToken()
	if err != nil {
		return TokenPair{}, fault.Internal(err)
	}
	claims := TokenClaims{UserBusinessID: userID, SessionID: uuid.NewString()}
	serialized, err := json.Marshal(claims)
	if err != nil {
		return TokenPair{}, fault.Internal(err)
	}
	accessKey := accessTokenKey(accessToken)
	refreshKey := refreshTokenKey(refreshToken)
	sessionKey := sessionRedisKey(claims.SessionID)

	// Redis MULTI/EXEC 将两种 token 和会话索引作为一组写入，避免只签发半对令牌。
	pipe := store.client.TxPipeline()
	pipe.Set(ctx, accessKey, serialized, store.accessTokenTTL)
	pipe.Set(ctx, refreshKey, serialized, store.refreshTokenTTL)
	pipe.SAdd(ctx, sessionKey, accessKey, refreshKey)
	pipe.Expire(ctx, sessionKey, store.refreshTokenTTL)
	pipe.SAdd(ctx, userSessionRedisKey(userID), claims.SessionID)
	pipe.Expire(ctx, userSessionRedisKey(userID), store.refreshTokenTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return TokenPair{}, fault.Unavailable(err)
	}
	return store.pair(accessToken, refreshToken), nil
}

// ValidateAccess 通过 SHA-256 摘要查 Redis，令牌格式正确但 key 不存在时仍视为未认证。
func (store *TokenStore) ValidateAccess(ctx context.Context, rawToken string) (TokenClaims, error) {
	if rawToken == "" {
		return TokenClaims{}, fault.Unauthorized()
	}
	serialized, err := store.client.Get(ctx, accessTokenKey(rawToken)).Bytes()
	if errors.Is(err, redis.Nil) {
		return TokenClaims{}, fault.Unauthorized()
	}
	if err != nil {
		return TokenClaims{}, fault.Unavailable(err)
	}
	var claims TokenClaims
	if err := json.Unmarshal(serialized, &claims); err != nil || claims.UserBusinessID == uuid.Nil || claims.SessionID == "" {
		return TokenClaims{}, fault.Unavailable(fmt.Errorf("Redis access token 数据损坏"))
	}
	return claims, nil
}

// Rotate 原子消费旧 refresh token 并签发新 token pair；并发重放只有一个请求可以成功。
func (store *TokenStore) Rotate(ctx context.Context, rawToken string) (TokenPair, TokenClaims, error) {
	if rawToken == "" {
		return TokenPair{}, TokenClaims{}, fault.Unauthorized()
	}
	oldKey := refreshTokenKey(rawToken)
	serialized, err := store.client.Get(ctx, oldKey).Bytes()
	if errors.Is(err, redis.Nil) {
		return TokenPair{}, TokenClaims{}, fault.Unauthorized()
	}
	if err != nil {
		return TokenPair{}, TokenClaims{}, fault.Unavailable(err)
	}
	var claims TokenClaims
	if err := json.Unmarshal(serialized, &claims); err != nil || claims.UserBusinessID == uuid.Nil || claims.SessionID == "" {
		return TokenPair{}, TokenClaims{}, fault.Unavailable(fmt.Errorf("Redis refresh token 数据损坏"))
	}
	accessToken, err := secureToken()
	if err != nil {
		return TokenPair{}, TokenClaims{}, fault.Internal(err)
	}
	refreshToken, err := secureToken()
	if err != nil {
		return TokenPair{}, TokenClaims{}, fault.Internal(err)
	}
	newAccessKey := accessTokenKey(accessToken)
	newRefreshKey := refreshTokenKey(refreshToken)
	accessValue, err := json.Marshal(claims)
	if err != nil {
		return TokenPair{}, TokenClaims{}, fault.Internal(err)
	}
	refreshValue, err := json.Marshal(claims)
	if err != nil {
		return TokenPair{}, TokenClaims{}, fault.Internal(err)
	}

	// Lua 脚本在 Redis 单线程执行中校验旧 key、替换 refresh token 并更新同一会话索引。
	const rotateScript = `
		local old_value = redis.call('GET', KEYS[1])
		if not old_value then return 0 end
		local old_claims = cjson.decode(old_value)
		if old_claims.session_id ~= ARGV[1] or old_claims.user_business_id ~= ARGV[2] then return 0 end
		if redis.call('SISMEMBER', KEYS[4], KEYS[1]) == 0 then return 0 end
		if redis.call('EXISTS', KEYS[2]) == 1 or redis.call('EXISTS', KEYS[3]) == 1 then return -1 end
		redis.call('SET', KEYS[2], ARGV[3], 'EX', ARGV[4])
		redis.call('SET', KEYS[3], ARGV[5], 'EX', ARGV[6])
		redis.call('DEL', KEYS[1])
		redis.call('SREM', KEYS[4], KEYS[1])
		redis.call('SADD', KEYS[4], KEYS[2], KEYS[3])
		redis.call('EXPIRE', KEYS[4], ARGV[6])
		redis.call('SADD', KEYS[5], ARGV[1])
		redis.call('EXPIRE', KEYS[5], ARGV[6])
		return 1
	`
	result, err := store.client.Eval(ctx, rotateScript,
		[]string{oldKey, newAccessKey, newRefreshKey, sessionRedisKey(claims.SessionID), userSessionRedisKey(claims.UserBusinessID)},
		claims.SessionID, claims.UserBusinessID.String(), string(accessValue), ttlSeconds(store.accessTokenTTL),
		string(refreshValue), ttlSeconds(store.refreshTokenTTL)).Int64()
	if err != nil {
		return TokenPair{}, TokenClaims{}, fault.Unavailable(err)
	}
	if result != 1 {
		if result < 0 {
			return TokenPair{}, TokenClaims{}, fault.Unavailable(fmt.Errorf("Redis token key collision"))
		}
		return TokenPair{}, TokenClaims{}, fault.Unauthorized()
	}
	return store.pair(accessToken, refreshToken), claims, nil
}

// RevokeSession 通过 Redis Lua 原子删除索引中登记的 token key，刷新与退出并发时仍保持完整撤销。
func (store *TokenStore) RevokeSession(ctx context.Context, sessionID string) error {
	const revokeScript = `
		local token_keys = redis.call('SMEMBERS', KEYS[1])
		for _, token_key in ipairs(token_keys) do
			local token_value = redis.call('GET', token_key)
			if token_value then
				local claims = cjson.decode(token_value)
				redis.call('SREM', 'auth:user-sessions:' .. claims.user_business_id, claims.session_id)
			end
			redis.call('DEL', token_key)
		end
		redis.call('DEL', KEYS[1])
		return #token_keys
	`
	if err := store.client.Eval(ctx, revokeScript, []string{sessionRedisKey(sessionID)}).Err(); err != nil {
		return fault.Unavailable(err)
	}
	return nil
}

// RevokeOtherSessions 撤销用户索引中的其他设备会话，并保留当前请求所属会话。
func (store *TokenStore) RevokeOtherSessions(ctx context.Context, userID uuid.UUID, keepSessionID string) error {
	const revokeScript = `
		local session_ids = redis.call('SMEMBERS', KEYS[1])
		for _, session_id in ipairs(session_ids) do
			if session_id ~= ARGV[1] then
				local session_key = 'auth:session:' .. session_id
				local token_keys = redis.call('SMEMBERS', session_key)
				for _, token_key in ipairs(token_keys) do redis.call('DEL', token_key) end
				redis.call('DEL', session_key)
				redis.call('SREM', KEYS[1], session_id)
			end
		end
		return 1
	`
	if err := store.client.Eval(ctx, revokeScript, []string{userSessionRedisKey(userID)}, keepSessionID).Err(); err != nil {
		return fault.Unavailable(err)
	}
	return nil
}

// AcquireUserLock 串行同一用户的登录令牌签发与密码变更，避免旧密码在撤销边界并发生成新会话。
func (store *TokenStore) AcquireUserLock(ctx context.Context, userID uuid.UUID) (func(), error) {
	lockValue, err := secureToken()
	if err != nil {
		return nil, fault.Internal(err)
	}
	key := "auth:user-lock:" + userID.String()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		acquired, setErr := store.client.SetNX(ctx, key, lockValue, time.Minute).Result()
		if setErr != nil {
			return nil, fault.Unavailable(setErr)
		}
		if acquired {
			release := func() {
				const unlockScript = `
					if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end
					return 0
				`
				unlockCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = store.client.Eval(unlockCtx, unlockScript, []string{key}, lockValue).Err()
			}
			return release, nil
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

// IndexExistingSessions 为启用跨设备撤销前签发的会话补建用户索引，启动期间只读取令牌声明。
func (store *TokenStore) IndexExistingSessions(ctx context.Context) error {
	iterator := store.client.Scan(ctx, 0, "auth:session:*", 100).Iterator()
	for iterator.Next(ctx) {
		sessionKey := iterator.Val()
		tokenKeys, err := store.client.SMembers(ctx, sessionKey).Result()
		if err != nil {
			return fault.Unavailable(err)
		}
		var claims TokenClaims
		for _, tokenKey := range tokenKeys {
			serialized, getErr := store.client.Get(ctx, tokenKey).Bytes()
			if errors.Is(getErr, redis.Nil) {
				continue
			}
			if getErr != nil {
				return fault.Unavailable(getErr)
			}
			if json.Unmarshal(serialized, &claims) == nil && claims.UserBusinessID != uuid.Nil && claims.SessionID != "" {
				break
			}
		}
		if claims.UserBusinessID == uuid.Nil || claims.SessionID == "" {
			continue
		}
		pipe := store.client.Pipeline()
		pipe.SAdd(ctx, userSessionRedisKey(claims.UserBusinessID), claims.SessionID)
		pipe.Expire(ctx, userSessionRedisKey(claims.UserBusinessID), store.refreshTokenTTL)
		if _, err := pipe.Exec(ctx); err != nil {
			return fault.Unavailable(err)
		}
	}
	if err := iterator.Err(); err != nil {
		return fault.Unavailable(err)
	}
	return nil
}

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
