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
	"github.com/photo-nini/backend/internal/tenant"
)

var phone11Pattern = regexp.MustCompile(`^[0-9]{11}$`)

// UserRepository 定义认证流程需要的最小 PostgreSQL 查询接口，便于服务层与 GORM 隔离。
type UserRepository interface {
	FindUserByPhone(context.Context, string) (model.User, error)
	FindUserByBusinessID(context.Context, uuid.UUID) (model.User, error)
	CreateUser(context.Context, *model.User) error
	UpdateUserProfile(context.Context, uuid.UUID, map[string]any) (model.User, error)
	UpdateStatisticsChartMode(context.Context, uuid.UUID, string, string) (model.User, error)
	FindUserPasswordHash(context.Context, uuid.UUID) (model.User, error)
	UpdateUserPasswordHash(context.Context, uuid.UUID, string) error
}

// SessionTokens 定义认证服务使用的 Redis 会话操作，不允许 service 自行拼接 Redis key。
type SessionTokens interface {
	Issue(context.Context, uuid.UUID) (store.TokenPair, error)
	Rotate(context.Context, string) (store.TokenPair, store.TokenClaims, error)
	RevokeSession(context.Context, string) error
	RevokeOtherSessions(context.Context, uuid.UUID, string) error
	AcquireUserLock(context.Context, uuid.UUID) (func(), error)
}

// AuthService 编排注册、登录、刷新和退出流程，密码与 token 的原始内容不会进入日志。
type AuthService struct {
	users         UserRepository
	tokens        SessionTokens
	passwords     *security.PasswordManager
	organizations *OrganizationService
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

// SetOrganizations 注入每次刷新和切换所需的组织资格校验。
func (service *AuthService) SetOrganizations(organizations *OrganizationService) {
	service.organizations = organizations
}

// Register 校验规范化手机号和密码规则，生成业务 UUID 后保存 Argon2id 哈希并签发 token pair。
func (service *AuthService) Register(ctx context.Context, phone, password, nickname string) (AuthResult, error) {
	return service.register(ctx, phone, password, nickname, nil)
}

// RegisterWithAvatar 执行新版头像必传注册；头像文件需先由 HTTP 层校验并存入 V1 文件存储。
func (service *AuthService) RegisterWithAvatar(ctx context.Context, phone, password, nickname, avatarKey string) (AuthResult, error) {
	if strings.TrimSpace(avatarKey) == "" {
		return AuthResult{}, fault.InvalidField("avatar", "注册必须上传头像")
	}
	return service.register(ctx, phone, password, nickname, &avatarKey)
}

// ValidateRegistration 检查注册文本字段，使 HTTP 上传头像前即可拒绝明显无效或超长账户数据。
func (service *AuthService) ValidateRegistration(phone, password, nickname string) error {
	if _, err := normalizeRegistrationPhone(phone); err != nil {
		return err
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	nickname = strings.TrimSpace(nickname)
	if length := utf8.RuneCountInString(nickname); length < 1 || length > 64 {
		return fault.InvalidField("nickname", "昵称长度必须为 1 至 64 个字符")
	}
	return nil
}

// register 校验 11 位手机号和密码后保存资料，再为新账户签发当前设备会话。
func (service *AuthService) register(ctx context.Context, phone, password, nickname string, avatarKey *string) (AuthResult, error) {
	if err := service.ValidateRegistration(phone, password, nickname); err != nil {
		return AuthResult{}, err
	}
	phone, _ = normalizeRegistrationPhone(phone)
	nickname = strings.TrimSpace(nickname)
	passwordHash, err := service.passwords.Hash(password)
	if err != nil {
		return AuthResult{}, fault.Internal(err)
	}
	user := model.User{
		BaseModel:    model.BaseModel{BusinessID: uuid.New(), OrgID: tenant.GlobalOrgID},
		Nickname:     nickname,
		Phone:        phone,
		PasswordHash: passwordHash,
		AvatarKey:    avatarKey,
	}
	if err := service.users.CreateUser(tenant.System(ctx), &user); err != nil {
		return AuthResult{}, err
	}
	if avatarKey != nil {
		// 注册头像先由匿名上传入口保存；账号 UUID 生成后才登记个人空间授权。
		if registrar, ok := service.users.(interface {
			RegisterFileOwnership(context.Context, string, uuid.UUID) error
		}); ok {
			if err := registrar.RegisterFileOwnership(tenant.Personal(ctx, user.BusinessID), *avatarKey, user.BusinessID); err != nil {
				return AuthResult{User: user}, err
			}
		}
	}
	tokens, err := service.tokens.Issue(ctx, user.BusinessID)
	if err != nil {
		// PostgreSQL 与 Redis 无法组成分布式事务；账号保留，客户端可在依赖恢复后重新登录。
		return AuthResult{User: user}, err
	}
	return AuthResult{User: user, Tokens: tokens}, nil
}

// Login 对手机号格式错误返回字段校验，对不存在账号和密码错误统一执行同成本校验并返回 401。
func (service *AuthService) Login(ctx context.Context, phone, password string) (AuthResult, error) {
	phone, err := normalizeLoginPhone(phone)
	if err != nil {
		return AuthResult{}, err
	}
	if err := validatePassword(password); err != nil {
		return AuthResult{}, err
	}
	user, err := service.users.FindUserByPhone(tenant.System(ctx), phone)
	if fault.Is(err, 404, "not_found") {
		service.passwords.VerifyDummy(password)
		return AuthResult{}, fault.Unauthorized()
	}
	if err != nil {
		return AuthResult{}, err
	}
	release, err := service.tokens.AcquireUserLock(ctx, user.BusinessID)
	if err != nil {
		return AuthResult{}, err
	}
	defer release()
	user, err = service.users.FindUserPasswordHash(tenant.System(ctx), user.BusinessID)
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
	profile, err := service.users.FindUserByBusinessID(tenant.System(ctx), user.BusinessID)
	if err != nil {
		return AuthResult{}, err
	}
	tokens, err := service.tokens.Issue(ctx, user.BusinessID)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: profile, Tokens: tokens}, nil
}

// Refresh 消费旧 refresh token 并签发新的一对令牌，Redis 中不存在旧 token 时拒绝重放。
func (service *AuthService) Refresh(ctx context.Context, refreshToken string) (store.TokenPair, error) {
	// 先检查资格，再消费令牌；停用组织或移除成员不能靠刷新取得新 token。
	if reader, ok := service.tokens.(interface {
		ReadRefreshClaims(context.Context, string) (store.TokenClaims, error)
	}); ok && service.organizations != nil {
		claims, err := reader.ReadRefreshClaims(ctx, refreshToken)
		if err != nil {
			return store.TokenPair{}, err
		}
		if _, err = service.organizations.ResolveScope(ctx, claims.UserBusinessID, claims.OrgID, claims.SessionID, claims.SessionVersion); err != nil {
			return store.TokenPair{}, err
		}
	}
	pair, claims, err := service.tokens.Rotate(ctx, refreshToken)
	if err != nil {
		return store.TokenPair{}, err
	}
	if _, err := service.users.FindUserByBusinessID(tenant.System(ctx), claims.UserBusinessID); err != nil {
		if fault.Is(err, 404, "not_found") {
			return store.TokenPair{}, fault.Unauthorized()
		}
		return store.TokenPair{}, err
	}
	return pair, nil
}

// SwitchEnvironment 只使用当前浏览器刷新凭证切换空间；普通业务接口不接受orgid覆盖token。
func (service *AuthService) SwitchEnvironment(ctx context.Context, raw string, orgID, expected int64, switchID string) (store.TokenPair, error) {
	if service.organizations == nil {
		return store.TokenPair{}, fault.Unavailable(nil)
	}
	if !ValidOrgID(orgID) || expected < 1 {
		return store.TokenPair{}, fault.InvalidRequest("组织或预期会话版本无效")
	}
	if _, err := uuid.Parse(switchID); err != nil {
		return store.TokenPair{}, fault.InvalidField("switch_id", "切换标识必须为UUID")
	}
	tokens, ok := service.tokens.(interface {
		ReadSwitchClaims(context.Context, string, string) (store.TokenClaims, error)
		SwitchEnvironment(context.Context, string, int64, int64, string) (store.TokenPair, error)
	})
	if !ok {
		return store.TokenPair{}, fault.Unavailable(nil)
	}
	claims, err := tokens.ReadSwitchClaims(ctx, raw, switchID)
	if err != nil {
		return store.TokenPair{}, err
	}
	release, err := service.tokens.AcquireUserLock(ctx, claims.UserBusinessID)
	if err != nil {
		return store.TokenPair{}, err
	}
	defer release()
	if _, err = service.users.FindUserByBusinessID(tenant.System(ctx), claims.UserBusinessID); err != nil {
		return store.TokenPair{}, err
	}
	if err = service.organizations.ValidateEnvironment(ctx, claims.UserBusinessID, orgID); err != nil {
		return store.TokenPair{}, err
	}
	return tokens.SwitchEnvironment(ctx, raw, orgID, expected, switchID)
}

// Logout 撤销当前 access token 所属会话索引中的 access 和 refresh token。
func (service *AuthService) Logout(ctx context.Context, sessionID string) error {
	return service.tokens.RevokeSession(ctx, sessionID)
}

// ChangePassword 验证当前密码、保存新哈希并撤销除当前会话外的其他设备会话。
func (service *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, sessionID, currentPassword, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	release, err := service.tokens.AcquireUserLock(ctx, userID)
	if err != nil {
		return err
	}
	defer release()
	user, err := service.users.FindUserPasswordHash(tenant.System(ctx), userID)
	if err != nil {
		return err
	}
	valid, err := service.passwords.Verify(currentPassword, user.PasswordHash)
	if err != nil {
		return fault.Internal(err)
	}
	if !valid {
		return fault.Unauthorized()
	}
	hash, err := service.passwords.Hash(newPassword)
	if err != nil {
		return fault.Internal(err)
	}
	// Redis 撤销先于密码写入；若数据库更新失败，最多会让其他设备提前退出，不会留下旧密码仍有效的跨设备会话。
	if err := service.tokens.RevokeOtherSessions(ctx, userID, sessionID); err != nil {
		return err
	}
	return service.users.UpdateUserPasswordHash(tenant.System(ctx), userID, hash)
}

// normalizeRegistrationPhone 仅接受并保存 11 位数字，注册时不猜测国家区号。
func normalizeRegistrationPhone(phone string) (string, error) {
	phone = strings.TrimSpace(phone)
	if !phone11Pattern.MatchString(phone) {
		return "", fault.InvalidField("phone", "手机号必须为 11 位数字")
	}
	return phone, nil
}

// normalizeLoginPhone 兼容旧版 +86 加 11 位输入，并映射到数据库保存的 11 位号码。
func normalizeLoginPhone(phone string) (string, error) {
	phone = strings.TrimSpace(phone)
	if strings.HasPrefix(phone, "+86") {
		phone = strings.TrimPrefix(phone, "+86")
	}
	if !phone11Pattern.MatchString(phone) {
		return "", fault.InvalidField("phone", "手机号必须为 11 位数字；旧客户端可使用 +86 加 11 位号码登录")
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
