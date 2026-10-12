package service

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/security"
	"github.com/photo-nini/backend/internal/store"
)

var e164PhonePattern = regexp.MustCompile(`^\+[1-9][0-9]{1,14}$`)

// UserRepository 定义认证流程需要的最小 PostgreSQL 查询接口，便于服务层与 GORM 隔离。
type UserRepository interface {
	FindUserByPhone(context.Context, string) (model.User, error)
	FindUserByBusinessID(context.Context, uuid.UUID) (model.User, error)
	CreateUser(context.Context, *model.User) error
	UpdateUserProfile(context.Context, uuid.UUID, map[string]any) (model.User, error)
}

// SessionTokens 定义认证服务使用的 Redis 会话操作，不允许 service 自行拼接 Redis key。
type SessionTokens interface {
	Issue(context.Context, uuid.UUID) (store.TokenPair, error)
	Rotate(context.Context, string) (store.TokenPair, store.TokenClaims, error)
	RevokeSession(context.Context, string) error
}

// AuthService 编排注册、登录、刷新和退出流程，密码与 token 的原始内容不会进入日志。
type AuthService struct {
	users     UserRepository
	tokens    SessionTokens
	passwords *security.PasswordManager
}

// AuthResult 同时携带用户资料和令牌对，handler 将用户映射为不含数据库 ID/密码的 DTO。
type AuthResult struct {
	User   model.User
	Tokens store.TokenPair
}

// NewAuthService 注入用户存储、Redis 会话和密码哈希器，避免业务流程依赖具体基础设施。
func NewAuthService(users UserRepository, tokens SessionTokens, passwords *security.PasswordManager) *AuthService {
	return &AuthService{users: users, tokens: tokens, passwords: passwords}
}

// Register 校验规范化手机号和密码规则，生成业务 UUID 后保存 Argon2id 哈希并签发 token pair。
func (service *AuthService) Register(ctx context.Context, phone, password, nickname string) (AuthResult, error) {
	phone, err := normalizePhone(phone)
	if err != nil {
		return AuthResult{}, err
	}
	nickname = strings.TrimSpace(nickname)
	if length := utf8.RuneCountInString(nickname); length < 1 || length > 64 {
		return AuthResult{}, fault.InvalidField("nickname", "昵称长度必须为 1 至 64 个字符")
	}
	if err := validatePassword(password); err != nil {
		return AuthResult{}, err
	}
	passwordHash, err := service.passwords.Hash(password)
	if err != nil {
		return AuthResult{}, fault.Internal(err)
	}
	user := model.User{
		BaseModel:    model.BaseModel{BusinessID: uuid.New()},
		Nickname:     nickname,
		Phone:        phone,
		PasswordHash: passwordHash,
	}
	if err := service.users.CreateUser(ctx, &user); err != nil {
		return AuthResult{}, err
	}
	tokens, err := service.tokens.Issue(ctx, user.BusinessID)
	if err != nil {
		// PostgreSQL 与 Redis 无法组成分布式事务；账号保留，客户端可在依赖恢复后重新登录。
		return AuthResult{}, err
	}
	return AuthResult{User: user, Tokens: tokens}, nil
}

// Login 对手机号格式错误返回字段校验，对不存在账号和密码错误统一执行同成本校验并返回 401。
func (service *AuthService) Login(ctx context.Context, phone, password string) (AuthResult, error) {
	phone, err := normalizePhone(phone)
	if err != nil {
		return AuthResult{}, err
	}
	if err := validatePassword(password); err != nil {
		return AuthResult{}, err
	}
	user, err := service.users.FindUserByPhone(ctx, phone)
	if fault.Is(err, 404, "not_found") {
		service.passwords.VerifyDummy(password)
		return AuthResult{}, fault.Unauthorized()
	}
	if err != nil {
		return AuthResult{}, err
	}
	valid, err := service.passwords.Verify(password, user.PasswordHash)
	if err != nil {
		return AuthResult{}, fault.Internal(err)
	}
	if !valid {
		return AuthResult{}, fault.Unauthorized()
	}
	tokens, err := service.tokens.Issue(ctx, user.BusinessID)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: user, Tokens: tokens}, nil
}

// Refresh 消费旧 refresh token 并签发新的一对令牌，Redis 中不存在旧 token 时拒绝重放。
func (service *AuthService) Refresh(ctx context.Context, refreshToken string) (store.TokenPair, error) {
	pair, claims, err := service.tokens.Rotate(ctx, refreshToken)
	if err != nil {
		return store.TokenPair{}, err
	}
	if _, err := service.users.FindUserByBusinessID(ctx, claims.UserBusinessID); err != nil {
		if fault.Is(err, 404, "not_found") {
			return store.TokenPair{}, fault.Unauthorized()
		}
		return store.TokenPair{}, err
	}
	return pair, nil
}

// Logout 撤销当前 access token 所属会话索引中的 access 和 refresh token。
func (service *AuthService) Logout(ctx context.Context, sessionID string) error {
	return service.tokens.RevokeSession(ctx, sessionID)
}

// normalizePhone 只接受规范化后的 E.164 手机号，去除外围空白但不猜测国家区号。
func normalizePhone(phone string) (string, error) {
	phone = strings.TrimSpace(phone)
	if !e164PhonePattern.MatchString(phone) {
		return "", fault.InvalidField("phone", "手机号必须使用 E.164 格式，例如 +8613800138000")
	}
	return phone, nil
}

// validatePassword 在注册和登录时执行统一长度校验，长度按 Unicode 字符计算。
func validatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if length < 8 || length > 128 {
		return fault.InvalidField("password", "密码长度必须为 8 至 128 个字符")
	}
	return nil
}
